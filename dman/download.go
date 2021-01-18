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
	MOVING_AVG_LEN = 5
	// download states
	S_DOWNLOADING = 0
	S_STOPPING    = 1
	S_FAILING     = 2
	S_REBUILDING  = 3
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

func readableSize(length int64) (float64, string) {
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

type downJob struct {
	offset, length, received, lastReceived, eta int64
	body                                        io.ReadCloser
	file                                        *os.File
	bufLenCh                                    chan int64
	err                                         error
}

type checkJob struct {
	received int64
	job      *downJob
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
	length    int64
	checkJob  chan checkJob
	jobDone   chan *downJob
	jobs      map[int64]*downJob
	jobsDone  []*downJob
	insertJob chan [2]*downJob
	stop      chan os.Signal
}

func (down *Download) getResponse(job *downJob) *http.Response {
	req, _ := http.NewRequest("GET", down.url, nil)
	if job.length > 0 { // unknown length, probably additional connection
		// request partial content
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", job.offset+job.received, job.offset+job.length-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		job.err = err
		return nil
	} else if job.length > 0 {  // resuming
		if resp.StatusCode != 206 { // partial content requested
			resp.Body.Close()
			if resp.StatusCode == 200 {
				job.err = notResumableError
			} else {
				job.err = fmt.Errorf("Connection error: %s", resp.Status)
			}
			return nil
		} else if newLen := resp.ContentLength; newLen != job.length {
			// probably file on server changed
			job.err = fmt.Errorf("Server sent another file.")
			return nil
		}
	} else { // full content, probably for first connection
		if resp.StatusCode != 200 {
			resp.Body.Close()
			job.err = fmt.Errorf("Bad response: %s", resp.Status)
			return nil
		}
		if job.received == 0 { // not resumed
			job.length = resp.ContentLength
		}
	}
	job.body = resp.Body
	return resp
}

func (down *Download) updateStatus() func(int64) {
	var speedHist [MOVING_AVG_LEN]int64
	return func(duration int64) {
		var written int64
		var speed int64
		for _, job := range down.jobs {
			jobSpeed := (job.received - job.lastReceived) * int64(time.Second) / duration // per second
			if jobSpeed == 0 {
				job.eta = int64(LONG_TIME)
			} else {
				job.eta = (job.length - job.received) / jobSpeed // in seconds
			}
			job.lastReceived = job.received
			written += job.received
			speed += jobSpeed
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
			etaVal := (down.length - written) * int64(time.Second) / speed
			eta = time.Duration(etaVal).Round(time.Second).String()
		}
		// moving average speed
		var avgSpeed int64
		for i, sp := range speedHist[1:] {
			speedHist[i] = sp
			avgSpeed += sp
		}
		speedHist[len(speedHist)-1] = speed
		avgSpeed = (avgSpeed + speed) / int64(len(speedHist))
		speedVal, sUnit := readableSize(avgSpeed)
		writtenVal, wUnit := readableSize(written)
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
	var longestFree int64
	for _, job := range down.jobs {
		free := job.length - job.received // not yet downloaded
		if free > longestFree {
			longest = down.jobs[job.offset]
			longestFree = free
		}
	}
	if longest == nil || longest.eta < MIN_CUT_ETA {
		return noSplitError
	}
	newLen := longestFree / 2
	newJob := &downJob{
		offset: longest.offset + longest.length - newLen,
		length: newLen,
	}
	go func() {
		down.getResponse(newJob)
		down.insertJob <- [2]*downJob{newJob, longest}
	}()
	return nil
}

func (down *Download) initJob(job *downJob) {
	// to receive buffer length from down.coordinate()
	job.bufLenCh = make(chan int64, 1)
	// so that down.addJob() doesn't skip this if the eta is 0
	job.eta = int64(LONG_TIME)
}

// this will modify only the response body and the file
func (down *Download) download(job *downJob) {
	bufLen := LEN_CHECK
	if job.length < int64(bufLen) {
		bufLen = int(job.length)
	}
	// kicksart the communication
	down.checkJob <- checkJob{0, job}
	for bufLen := range job.bufLenCh {
		written, err := io.CopyN(job.file, job.body, bufLen)
		if err != nil {
			job.err = err
			break
		}
		down.checkJob <- checkJob{written, job}
	}
	down.jobDone <- job
}

func (down *Download) coordinate() {
	defer close(down.emitStatus)
	defer close(down.insertJob)
	defer close(down.jobDone)
	defer close(down.err)
	defer close(down.stop)
	defer close(down.checkJob)
	lastTime := time.Now()
	timer := time.NewTimer(STAT_INTERVAL)
	updateStat := down.updateStatus()
	var mainError error
	var addingJobLock bool
	state := S_DOWNLOADING
	if down.maxConns > 1 {
		// add other conns
		addingJobLock = down.addJob() == nil
	}
	for {
		select {
		case check := <-down.checkJob:
			check.job.received += check.received
			if state != S_DOWNLOADING {
				// clean up already completed
				continue
			}
			if check.job.received < check.job.length {
				bufLen := int64(LEN_CHECK)
				if remaining := check.job.length - check.job.received; remaining < bufLen {
					bufLen = remaining
				}
				check.job.bufLenCh <- bufLen
			} else {  // clean up
				check.job.body.Close()
				close(check.job.bufLenCh)
			}
		case job := <-down.jobDone:
			if job.offset < 0 { // finished rebuilding
				down.err <- job.err
				return
			}
			delete(down.jobs, job.offset)
			down.jobsDone = append(down.jobsDone, job)
			failed := job.err != nil && state == S_DOWNLOADING
			if failed {
				mainError = job.err
			}
			if len(down.jobs) == 0 {
				if addingJobLock {
					// flush the new one
					jobs := <-down.insertJob
					jobs[0].body.Close()
				}
				if state == S_STOPPING {
					mainError = pausedError
				} else if state != S_FAILING && job.err != nil && mainError == nil {
					// no previous errors, record this one
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
				if down.length < 0 { // length was unknown, now known
					down.length = job.length
				}
				state = S_REBUILDING
				down.rebuild()
			} else if failed { // failed
				state = S_FAILING
				for _, job := range down.jobs { // start pausing others
					job.body.Close()
					close(job.bufLenCh)
				}
			} else if state == S_DOWNLOADING && !addingJobLock && len(down.jobs) < down.maxConns {
				addingJobLock = down.addJob() == nil
			}
		case now := <-timer.C: // status update time
			duration := int64(now.Sub(lastTime))
			lastTime = now
			if state == S_REBUILDING {
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
			if state != S_DOWNLOADING {
				job.body.Close()
				continue
			}
			if job.err == nil {
				if longest.received < longest.length - job.length { // still in progress
					file, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, fmt.Sprintf("%s.%d", down.filename, job.offset)))
					if err == nil {
						job.file = file
						down.initJob(job)
						go down.download(job)
						// add this job to the collection
						down.jobs[job.offset] = job
						// subtract length from the helped job
						longest.length -= job.length
					} else {
						job.body.Close()
					}
				} else {
					job.body.Close()
				}
			}
			if len(down.jobs) < down.maxConns {
				addingJobLock = down.addJob() == nil
			} else {
				addingJobLock = false
			}
		case <-down.stop:
			if state == S_DOWNLOADING {
				state = S_STOPPING
				for _, job := range down.jobs { // start pausing
					job.body.Close()
					close(job.bufLenCh)
				}
			}
		}
	}
}

func (down *Download) start() error {
	firstJob := &downJob{}
	resp := down.getResponse(firstJob)
	if firstJob.err != nil {
		return firstJob.err
	}
	// get filename
	down.filename = getFilename(resp)
	os.Mkdir(filepath.Join(down.dir, PART_DIR_NAME), 666)
	file, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename+".0"))
	if err != nil {
		return err
	}
	firstJob.file = file
	down.initJob(firstJob)
	down.length = firstJob.length
	down.jobs[0] = firstJob
	go down.download(firstJob)
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
		connProg := map[string]int64{
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
		fname := filepath.Join(down.dir, PART_DIR_NAME, fmt.Sprintf("%s.%d", down.filename, newJob.offset))
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
		down.getResponse(job)
		requestErr <- job.err
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
		down.initJob(job)
		go down.download(job)
	}
	go down.coordinate()
	os.Remove(progressFile)
	return nil
}

type progress struct {
	Id       int                `json:"id"`
	Url      string             `json:"url"`
	Filename string             `json:"filename"`
	Parts    []map[string]int64 `json:"parts"`
}

func newDownload(url string, maxConns int, id int, dir string) *Download {
	down := Download{
		id:         id,
		url:        url,
		dir:        dir,
		maxConns:   maxConns,
		jobs:       map[int64]*downJob{},
		err:        make(chan error),
		stop:       make(chan os.Signal),
		emitStatus: make(chan status, 1), // buffered to bypass emitting if no consumer and continue updating, coordinate()
		insertJob:  make(chan [2]*downJob),
		checkJob:   make(chan checkJob, 10),
		jobDone:    make(chan *downJob),
	}
	return &down
}
