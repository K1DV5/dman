// -{go install}
// -{go fmt %f}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	KB             = 1024
	MB             = KB * KB
	GB             = MB * KB
	LEN_CHECK      = 32 * KB // data interval to check if connection should stop
	MIN_CUT_ETA    = 10      // minimum ramaining time to split connection, in seconds
	STAT_INTERVAL  = 500 * time.Millisecond
	LONG_TIME      = 3 * 24 * int(time.Hour) // 3 days, arbitrarily large duration
	PART_DIR_NAME  = ".dman"
	PROG_FILE_EXT  = ".dman"
	MOVING_AVG_LEN = 3
	// message order types
	O_STAT         = 0
	O_PARAMS       = 1
	O_LENGTH       = 2
	O_COMP_PHOLDER = -1
	O_STOP         = -2
)

var (
	pausedError       = fmt.Errorf("paused")
	noSplitError      = fmt.Errorf("No job to split found")
	notResumableError = fmt.Errorf("Connection not resumable")
)

type status struct {
	Id         int     `json:"id,omitempty"`
	Rebuilding bool    `json:"rebuilding,omitempty"`
	Speed      string  `json:"speed,omitempty"`
	Written    string  `json:"written,omitempty"`
	Percent    float64 `json:"percent,omitempty"`
	Conns      int     `json:"conns,omitempty"`
	Eta        string  `json:"eta,omitempty"`
}

func getFilename(resp *http.Response) string {
	var filename string
	disposition := resp.Header.Get("Content-Disposition")
	if prefix := "filename="; strings.Contains(disposition, prefix) {
		start := strings.Index(disposition, prefix) + len(prefix)
		filename = disposition[start:]
	} else {
		url_parts := strings.Split(resp.Request.URL.Path, "/")
		filename = url_parts[len(url_parts)-1]
	}
	return filename
}

func readableSize(length int) (float64, string) {
	var value = float64(length)
	var unit string
	switch {
	case value > GB:
		value /= GB
		unit = "GB"
	case value > MB:
		value /= MB
		unit = "MB"
	case value > KB:
		value /= KB
		unit = "KB"
	default:
		unit = "B"
	}
	return value, unit
}

func movingAvg(hist *[MOVING_AVG_LEN]int, newVal int) int {
	var avg int
	for i, h := range hist[1:] {
		hist[i] = h
		avg += h
	}
	hist[len(*hist)-1] = newVal
	return (avg + newVal) / len(*hist)
}

type jobMsg struct {
	offset   int
	received int
	eta      int
	length   int
	order    int
	duration int
	speed    int
}

type downJob struct {
	offset, length, received int
	body                     io.ReadCloser
	file                     *os.File
	msg                      chan jobMsg
	err                      error
}

type readResult struct {
	n int
	err error
}

type Download struct {
	// Required:
	id       int
	url      string
	dir      string
	maxConns int
	err      chan error
	// status
	emitStatus chan status
	// Dynamically set:
	filename  string
	length    int
	jobDone   chan *downJob
	jobStat   chan jobMsg
	jobParams chan jobMsg
	jobs      map[int]*downJob
	jobsDone  []*downJob
	insertJob chan [2]*downJob
	stop      chan os.Signal
}

func (down *Download) getResponse(job *downJob) (*http.Response, error) {
	req, _ := http.NewRequest("GET", down.url, nil)
	if job.length > 0 { // unknown length, probably additional connection
		// request partial content
		req.Header.Add("Range", "bytes="+strconv.Itoa(job.offset+job.received)+"-"+strconv.Itoa(job.offset+job.length-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	} else if job.length > 0 && resp.StatusCode != 206 { // partial content
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return nil, notResumableError
		} else {
			return nil, fmt.Errorf("Connection error: %s", resp.Status)
		}
	} else if job.length == 0 { // full content, probably for first connection
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("Bad response: %s", resp.Status)
		}
		if job.received == 0 { // not resumed
			job.length = int(resp.ContentLength)
		}
	}
	job.body = resp.Body
	job.msg = make(chan jobMsg, 2) // for stat and params
	return resp, nil
}

func (down *Download) updateStatus() func(int) {
	var speedHist [MOVING_AVG_LEN]int
	return func(duration int) {
		var written, speed int
		for _, job := range down.jobs {
			job.msg <- jobMsg{order: O_STAT, duration: duration}
		}
		for i := 0; i < len(down.jobs); i++ {
			param := <-down.jobStat
			written += param.received
			speed += param.speed
		}
		for _, job := range down.jobsDone { // take the completed into account
			written += job.length
		}
		if down.emitStatus == nil || len(down.emitStatus) > 0 {
			return
		}
		// moving average speed and eta
		var eta string
		if speed == 0 {
			eta = "LongTime"
		} else {
			etaVal := (down.length - written) * int(time.Second) / speed
			eta = time.Duration(etaVal).Round(time.Second).String()
		}
		speedVal, sUnit := readableSize(int(movingAvg(&speedHist, speed)))
		writtenVal, wUnit := readableSize(int(written))
		var percent float64
		if down.length > 0 {
			percent = float64(written) / float64(down.length) * 100
		}
		down.emitStatus <- status{
			Id:      down.id,
			Speed:   fmt.Sprintf("%.2f%s/s", speedVal, sUnit),
			Percent: percent,
			Written: fmt.Sprintf("%.2f%s", writtenVal, wUnit),
			Conns:   len(down.jobs),
			Eta:     eta,
		}
	}
}

func (down *Download) addJob() error {
	var longest *downJob // connection having the longest undownloaded part
	var longestParams jobMsg
	longestFree := 0
	for _, job := range down.jobs {
		job.msg <- jobMsg{order: O_PARAMS} // get length
	}
	for i := 0; i < len(down.jobs); i++ {
		// select not necessary here as down.jobParams will be closed and longest will be nil
		params := <-down.jobParams
		free := params.length - params.received // not yet downloaded
		if free > longestFree {
			longest = down.jobs[params.offset]
			longestFree = free
			longestParams = params
		}
	}
	if longest == nil || longestParams.eta < MIN_CUT_ETA {
		return noSplitError
	}
	newLen := longestFree / 2
	newJob := &downJob{
		offset: longestParams.offset + longestParams.length - newLen,
		length: newLen,
	}
	go func() {
		_, err := down.getResponse(newJob)
		if err != nil {
			newJob.err = err
		}
		down.insertJob <- [2]*downJob{newJob, longest}
	}()
	return nil
}

func (down *Download) coordinate() {
	defer close(down.emitStatus)
	defer close(down.insertJob)
	defer close(down.jobDone)
	defer close(down.err)
	defer close(down.stop)
	defer close(down.jobStat)
	defer close(down.jobParams)
	lastTime := time.Now()
	timer := time.NewTimer(STAT_INTERVAL)
	updateStat := down.updateStatus()
	var mainError error
	var rebuilding, addingJobLock bool
	if down.maxConns > 1 {
		// add other conns
		addingJobLock = down.addJob() == nil
	}
	for {
		select {
		case job := <-down.jobDone:
			if job.offset < 0 { // finished rebuilding
				down.err <- job.err
				return
			}
			delete(down.jobs, job.offset)
			down.jobsDone = append(down.jobsDone, job)
			close(job.msg)
			failed := job.err != nil && job.err != pausedError
			if failed {
				mainError = job.err
			}
			if len(down.jobs) == 0 {
				if addingJobLock {
					// flush the new one
					<-down.insertJob
				}
				if job.err != nil && mainError == nil { // no previous errors
					mainError = job.err
				}
				if mainError != nil { // finished pausing or failing
					// close the completed files
					for _, job := range down.jobsDone {
						if err := job.file.Close(); err != nil {
							mainError = fmt.Errorf("%v, & %v", mainError, err)
						}
					}
					down.saveProgress()
					down.err <- mainError
					return
				}
				// finished downloading, start rebuilding
				down.rebuild()
				rebuilding = true
			} else if failed { // failed
				for _, job := range down.jobs { // start pausing others
					job.msg <- jobMsg{order: O_STOP}
				}
			} else if !addingJobLock && len(down.jobs) < down.maxConns {
				addingJobLock = down.addJob() == nil
			}
		case now := <-timer.C: // status update time
			duration := int(now.Sub(lastTime))
			lastTime = now
			if rebuilding { // finished downloading, rebuilding
				stat, err := down.jobsDone[0].file.Stat()
				if err != nil {
					down.err <- nil // maybe rebuilding finished already
					return
				}
				if down.emitStatus != nil && len(down.emitStatus) == 0 {
					down.emitStatus <- status{
						Id:         down.id,
						Rebuilding: true,
						Percent:    float64(stat.Size()) / float64(down.length) * 100,
					}
				}
				continue
			}
			updateStat(duration)
			timer.Reset(STAT_INTERVAL)
		case jobs := <-down.insertJob:
			job, longest := jobs[0], jobs[1]
			if down.jobs[longest.offset] != nil && job.err == nil { // still in progress
				file, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename+"."+strconv.Itoa(job.offset)))
				if err == nil {
					job.file = file
					// add this job to the collection
					down.jobs[job.offset] = job
					// subtract length
					longest.msg <- jobMsg{
						order:  O_LENGTH,
						length: job.length,
					}
					go down.download(job)
				}
			}
			if len(down.jobs) < down.maxConns {
				addingJobLock = down.addJob() == nil
			} else {
				addingJobLock = false
			}
		case <-down.stop:
			if rebuilding {
				continue
			}
			for _, job := range down.jobs { // start pausing
				job.msg <- jobMsg{order: O_STOP}
			}
		}
	}
}

func (down *Download) handleMsg(job *downJob) func(jobMsg) {
	var lastReceived, speed, eta int
	eta = LONG_TIME
	overDestChans := [2]chan jobMsg{down.jobStat, down.jobParams}
	return func(msg jobMsg) {
		toSend := jobMsg{
			received: job.received,
			length:   job.length,
		}
		if msg.order == O_STAT { // get status
			speed = (job.received - lastReceived) * int(time.Second) / msg.duration // per second
			if speed == 0 {
				eta = LONG_TIME
			} else {
				eta = (job.length - job.received) / speed // in seconds
			}
			toSend.speed = speed
			toSend.eta = eta
			down.jobStat <- toSend
			lastReceived = job.received
		} else if msg.order == O_PARAMS { // get params for adding and such
			toSend.offset = job.offset
			toSend.eta = eta
			down.jobParams <- toSend
		} else if msg.order == O_LENGTH { // subtract length
			eta = eta * (job.length - msg.length - job.received) / (job.length - job.received)
			job.length -= msg.length
			if job.received >= job.length {
				job.file.Truncate(int64(job.length))
				job.length = job.received
			}
		} else if msg.order == O_COMP_PHOLDER && msg.offset != O_STOP {
			// just until over message is accepted, stat or params
			if msg.offset > 1 {
				return
			}
			overDestChans[msg.offset] <- toSend
		}
	}
}

func (down *Download) readBody(body io.ReadCloser, buffer []byte, bufLenCh chan int, resCh chan readResult) {
	defer close(resCh)
	for bufLen := range bufLenCh {
		n, err := body.Read(buffer[:bufLen])  // can be interrupted by closing job.body
		resCh <- readResult{
			n: n,
			err: err,
		}
	}
}

func (down *Download) download(job *downJob) {
	handleMsg := down.handleMsg(job)
	bufLenCh := make(chan int, 1)
	defer close(bufLenCh)
	readCh := make(chan readResult)
	var readRes readResult
	var buffer [LEN_CHECK]byte
	go down.readBody(job.body, buffer[:], bufLenCh, readCh)
	bufLen := LEN_CHECK
	if remaining := job.length - job.received; remaining < bufLen {
		bufLen = remaining
	}
	bufLenCh <- bufLen
	for {
		select {
		case msg := <-job.msg:
			handleMsg(msg)
			if msg.order != O_STOP {
				continue
			}
			// stop ordered
			readRes = readResult{err: pausedError}
		case readRes = <-readCh:  // continue reading
		}
		// code adapted from io source
		if readRes.n > 0 {
			nWrote, errW := job.file.Write(buffer[:readRes.n])
			if nWrote > 0 {
				job.received += nWrote
				if job.received == job.length {  // finished
					break
				} else if remaining := job.length - job.received; remaining < bufLen {
					bufLen = remaining
				}
			}
			if errW != nil {
				job.err = errW
				break
			}
			if readRes.n != nWrote {
				job.err = io.ErrShortWrite
				break
			}
		}
		if readRes.err != nil {
			if readRes.err == io.EOF { // unknown length, now known
				job.length = job.received
			} else {
				job.err = readRes.err
			}
			break
		}
		bufLenCh <- bufLen
	}
	job.body.Close()
	// continue responding to messages until done message is received
	for {
		select {
		case down.jobDone <- job:
			return
		case msg := <-job.msg:
			// to indicate that the job is done and prevent stat calculation
			msg.order, msg.offset = O_COMP_PHOLDER, msg.order
			handleMsg(msg)
		}
	}
}

func (down *Download) start() error {
	os.Mkdir(filepath.Join(down.dir, PART_DIR_NAME), 666)
	firstJob := &downJob{}
	resp, err := down.getResponse(firstJob)
	if err != nil {
		return err
	}
	// get filename
	down.filename = getFilename(resp)
	file, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename+".0"))
	if err != nil {
		return err
	}
	firstJob.file = file
	down.length = firstJob.length
	down.jobs[0] = firstJob
	if down.length > 0 { // if the length is known, begin adding more connections
		go down.download(firstJob)
	}
	go down.coordinate()
	return nil
}

func (down *Download) rebuild() {
	// sort by offset
	sort.Slice(down.jobsDone, func(i, j int) bool {
		return down.jobsDone[i].offset < down.jobsDone[j].offset
	})
	go func() {
		var err error
		defer func() {
			down.jobDone <- &downJob{offset: -1, err: err}
		}()
		file := down.jobsDone[0].file
		if down.length < 0 { // unknown file size, single connection, length set at end
			down.length = down.jobsDone[0].length
		}
		for _, job := range down.jobsDone[1:] {
			if job.file == nil {
				continue
			}
			if _, err = job.file.Seek(0, 0); err != nil {
				return
			}
			if _, err = io.Copy(file, job.file); err != nil {
				return
			}
			if err = job.file.Close(); err != nil {
				return
			}
			if err = os.Remove(job.file.Name()); err != nil {
				return
			}
		}
		if err = file.Close(); err != nil {
			return
		}
		if err = os.Rename(file.Name(), filepath.Join(down.dir, down.filename)); err != nil {
			return
		}
		os.Remove(filepath.Dir(file.Name())) // only if empty
	}()
}

func (down *Download) saveProgress() error {
	prog := progress{
		Id:       down.id,
		Url:      down.url,
		Filename: down.filename,
	}
	for _, job := range down.jobsDone {
		connProg := map[string]int{
			"offset":   job.offset,
			"length":   job.length,
			"received": job.received,
		}
		prog.Parts = append(prog.Parts, connProg)
	}
	f, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename+PROG_FILE_EXT))
	if err != nil {
		return err
	}
	json.NewEncoder(f).Encode(prog)
	f.Close()
	return nil
}

func (down *Download) resume(progressFile string) (err error) {
	defer func() {
		if err != nil {
			for _, job := range down.jobs {
				job.file.Close()
			}
		}
	}()
	var prog progress
	f, err := os.Open(progressFile)
	if err != nil {
		return err
	}
	if err := json.NewDecoder(f).Decode(&prog); err != nil {
		return err
	}
	f.Close()
	down.id = prog.Id
	down.url = prog.Url
	down.dir = filepath.Dir(filepath.Dir(progressFile))
	down.filename = prog.Filename
	for _, job := range prog.Parts {
		newJob := &downJob{
			offset:   job["offset"],
			length:   job["length"],
			received: job["received"],
		}
		fname := filepath.Join(down.dir, PART_DIR_NAME, down.filename+"."+strconv.Itoa(newJob.offset))
		file, err := os.OpenFile(fname, os.O_APPEND, 755)
		if err != nil {
			return err
		}
		newJob.file = file
		if newJob.received < newJob.length { // unfinished
			down.jobs[newJob.offset] = newJob
		} else {
			down.jobsDone = append(down.jobsDone, newJob)
		}
		down.length += newJob.length
	}
	// make requests
	requestErr := make(chan error)
	request := func(job *downJob) {
		_, err := down.getResponse(job)
		requestErr <- err
	}
	for _, job := range down.jobs {
		go request(job)
	}
	// check requests errors
	for i := 0; i < len(down.jobs); i++ {
		if err := <-requestErr; err != nil {
			return err
		}
	}
	for _, job := range down.jobs {
		go down.download(job)
	}
	go down.coordinate()
	os.Remove(progressFile)
	return nil
}

type progress struct {
	Id       int              `json:"id"`
	Url      string           `json:"url"`
	Filename string           `json:"filename"`
	Parts    []map[string]int `json:"parts"`
}

func newDownload(url string, maxConns int, id int, dir string) *Download {
	down := Download{
		id:         id,
		url:        url,
		dir:        dir,
		maxConns:   maxConns,
		jobs:       map[int]*downJob{},
		err:        make(chan error),
		stop:       make(chan os.Signal),
		emitStatus: make(chan status, 1), // buffered to bypass emitting if no consumer and continue updating, coordinate()
		insertJob:  make(chan [2]*downJob),
		jobDone:    make(chan *downJob),
		jobStat:    make(chan jobMsg, maxConns),
		jobParams:  make(chan jobMsg, maxConns),
	}
	return &down
}
