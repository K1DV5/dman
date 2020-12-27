// -{go install}
// -{go fmt %f}

package main

import (
	// "encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	KB             = 1024
	MB             = KB * KB
	GB             = MB * KB
	LEN_CHECK      = 32 * KB              // data interval to check if connection should stop
	MIN_CUT_ETA    = 5 * int(time.Second) // min ramaining time to split connection
	STAT_INTERVAL  = 500 * time.Millisecond
	LONG_TIME      = 3 * 24 * int(time.Hour) // 3 days, arbitrarily large duration
	PART_DIR_NAME  = ".dman"
	PROG_FILE_EXT  = ".dman"
	SPEED_HIST_LEN = 10
)

type status struct {
	Id         int     `json:"id,omitempty"`
	Rebuilding bool    `json:"rebuilding,omitempty"`
	Speed      string  `json:"speed,omitempty"`
	Written    string     `json:"written,omitempty"`
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

type jobMsg struct {
	received int
	eta int
	length int
	order int  // 0: stop, 1: get status
	duration int
	speed int
}

type downJob struct {
	offset, length, received int
	body io.ReadCloser
	file *os.File
	msg chan jobMsg
	stat chan jobMsg
	params chan jobMsg
	done chan *downJob
	err error
}

type completedJob struct {
	file *os.File
	length int
	offset int
}

type Download struct {
	// Required:
	id       int
	url      string
	dir 	string
	maxConns int
	addChan chan *downJob
	// status
	emitStatus chan status
	stopStatus chan bool
	// Dynamically set:
	headers     [][]string
	filename    string
	length      int
	jobDone    chan *downJob
	jobs map[int]*downJob
	completed []*downJob
	stopAdd     chan bool
	stop        chan os.Signal
}


func (down *Download) getResponse(job *downJob) (*http.Response, error) {
	req, err := http.NewRequest("GET", down.url, nil)
	if err != nil {
		return nil, err
	}
	for _, pair := range down.headers {
		req.Header.Add(pair[0], pair[1])
	}
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
			return nil, fmt.Errorf("Connection not resumable")
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
	job.msg = make(chan jobMsg)
	job.body = resp.Body
	if job.file == nil { // new, not resuming
		file, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, getFilename(resp) + "." + strconv.Itoa(job.offset)))
		if err != nil {
			return nil, err
		}
		job.file = file
	}
	return resp, nil
}

func (down *Download) updateStatus() func(int) {
	// var written, lastWritten, conns int64
	var speedHist [SPEED_HIST_LEN]int64
	return func(duration int) {
		var written, speed, eta int64
		var wg sync.WaitGroup
		for _, job := range down.jobs {
			wg.Add(1)
			go func() {
				job.msg <- jobMsg{order: 0, duration: duration}
				param := <- job.stat
				atomic.AddInt64(&written, int64(param.received))
				atomic.AddInt64(&speed, int64(param.speed))
				atomic.AddInt64(&eta, int64(param.eta))
				wg.Done()
			}()
		}
		wg.Wait()
		for _, job := range down.completed {  // take the completed into account
			written += int64(job.length)
		}
		fmt.Print("\r", written, speed, eta, strings.Repeat(" ", 30))
		if down.emitStatus != nil && len(down.emitStatus) == 0 {
			// moving average speed
			var speedAvg int64
			for i, sp := range speedHist[1:] {
				speedHist[i] = int64(sp)
				speedAvg += int64(sp)
			}
			speedHist[len(speedHist)-1] = speed
			speedAvg = (speedAvg + speed) / int64(len(speedHist))
			speedVal, sUnit := readableSize(int(speedAvg))
			writtenVal, wUnit := readableSize(int(written))
			fmt.Printf("\r%.2f%s/s %.2f%% %.2f%s %s x%d" + strings.Repeat(" ", 40), speedVal, sUnit, float64(written) / float64(down.length) * 100, writtenVal, wUnit, time.Duration(eta * int64(time.Second)).String(), len(down.jobs))
			// down.emitStatus <- status{
			// 	Id:      down.id,
			// 	Speed:   fmt.Sprintf("%.2f%s/s", speedVal, sUnit),
			// 	Percent: float64(written) / float64(down.length) * 100,
			// 	Written: fmt.Sprintf("%.2f%s", writtenVal, wUnit),
			// 	Conns:   conns,
			// 	Eta:     eta,
			// }
		}
	}
}

func (down *Download) addJob() error {
	var longest *downJob // connection having the longest undownloaded part
	var longestParams jobMsg
	longestFree := 0
	for _, job := range down.jobs {
		job.msg <- jobMsg{order: 1}  // get length
		params := <-job.params
		free := params.length - params.received // not yet downloaded
		if free > longestFree {
			longest = job
			longestFree = free
			longestParams = params
		}
	}
	if longest == nil || longestParams.eta < MIN_CUT_ETA/int(time.Second) {
		return fmt.Errorf("No connection to split found")
	}
	newLen := int(math.Ceil(float64(longestFree / 2)))
	newJob := &downJob{
		offset: longest.offset + longest.length - newLen,
		length: newLen,
		done:   down.jobDone,
		msg: make(chan jobMsg),
		stat: make(chan jobMsg),
		params: make(chan jobMsg),
	}
	_, err := down.getResponse(newJob)
	if err != nil {
		return err
	}
	// shorten previous connection
	longest.msg <- jobMsg{order: 2, length: newLen}  // sey length
	// add this conn to the collection
	down.jobs[newJob.offset] = newJob
	down.addChan <- newJob
	return nil
}


func (down *Download) wait() error {  // while coordinating downloads
	defer close(down.addChan)
	var lastTime time.Time
	var rebuilding bool
	timer := time.NewTimer(STAT_INTERVAL)
	updateStat := down.updateStatus()
	for {
		select {
		case job, ok := <-down.jobDone:
			if !ok {
				return nil
			} else if job.err != nil {
				return job.err
			}
			delete(down.jobs, job.offset)
			down.completed = append(down.completed, job)
			if len(down.jobs) == 0 {
				go down.rebuild()
				rebuilding = true
			}
		case now := <-timer.C:  // status update time
			duration := int(now.Sub(lastTime))
			lastTime = now
			if rebuilding { // finished downloading, rebuilding
				stat, err := down.completed[0].file.Stat()
				if err != nil {
					return nil // maybe rebuilding finished already
				}
				fmt.Print("\r", float64(stat.Size()) / float64(down.length) * 100, strings.Repeat(" ", 30))
				// down.emitStatus <- status{
				// 	Id: down.id,
				// 	Rebuilding: true,
				// 	Percent:    float64(stat.Size()) / float64(down.length) * 100,
				// }
				continue
			}
			updateStat(duration)
			timer.Reset(STAT_INTERVAL)
		default:
			if len(down.jobs) < down.maxConns {
				down.addJob()
			}
		}
	}
}


func (down *Download) download() {
	for job := range down.addChan {
		var lastReceived, speed, eta int
		for {
			select {
			case msg := <-job.msg:
				if msg.order == 0 || msg.order == 1 {
					toSend := jobMsg{
							received: job.received,
							eta: eta,
							speed: speed,
							length: job.length,
						}
					if msg.order == 0 {  // get status
						speed = (job.received - lastReceived) * int(time.Second) / msg.duration
						if speed == 0 {
							eta = LONG_TIME
						} else {
							eta = (job.length - job.received) / speed // in seconds
						}
						toSend.speed = speed
						toSend.eta = eta
						job.stat <- toSend
					} else {  // get params for adding and such
						job.params <- toSend
					}
					lastReceived = job.received
				} else {  // update length
					job.length = msg.length
				}
			default:  // continue downloading
			}
			if job.length > 0 && job.length-job.received < LEN_CHECK { // final
				remaining := job.length - job.received
				if remaining > 0 {
					io.CopyN(job.file, job.body, int64(remaining))
					job.received += remaining
				} else if remaining < 0 { // received too much
					job.file.Truncate(int64(job.length))
					job.received = job.length
				}
				job.done <- job
				break
			}
			wrote, err := io.CopyN(job.file, job.body, int64(LEN_CHECK))
			if err != nil {
				job.received += int(wrote)
				job.length = job.received
				if err != io.EOF {
					job.err = err
				}
				job.done <- job
				break
			}
			job.received += LEN_CHECK
		}
		job.body.Close()
	}
}

func (down *Download) start() error {
	os.Mkdir(filepath.Join(down.dir, PART_DIR_NAME), 666)
	// start workers
	for i := 0; i < down.maxConns; i++ {
		go down.download()
	}
	firstJob := &downJob{
		done: down.jobDone,
		msg: make(chan jobMsg),
		stat: make(chan jobMsg),
		params: make(chan jobMsg),
	}
	resp, err := down.getResponse(firstJob)
	if err != nil {
		return err
	}
	// get filename
	down.filename = getFilename(resp)
	down.length = firstJob.length
	down.jobs[0] = firstJob
	down.addChan <- firstJob
	return nil
}

func (down *Download) rebuild() error {
	// sort by offset
	sort.Slice(down.completed, func(i, j int) bool {
		return down.completed[i].offset < down.completed[j].offset
	})
	file := down.completed[0].file
	if down.length < 0 { // unknown file size, single connection, length set at end
		down.length = down.completed[0].length
	}
	for _, job := range down.completed[1:] {
		if _, err := job.file.Seek(0, 0); err != nil {
			return err
		}
		if _, err := io.Copy(file, job.file); err != nil {
			return err
		}
		if err := job.file.Close(); err != nil {
			return err
		}
		if err := os.Remove(job.file.Name()); err != nil {
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(down.dir, down.filename)); err != nil {
		return err
	}
	os.Remove(filepath.Dir(file.Name())) // only if empty
	close(down.jobDone)
	return nil
}

func (down *Download) saveProgress() error {
	// prog := progress{Url: down.url, Filename: down.filename, Dir: down.dir}
	// for _, conn := range down.connections {
	// 	connProg := map[string]int{
	// 		"offset":   conn.offset,
	// 		"length":   conn.length,
	// 		"received": conn.received,
	// 	}
	// 	prog.Parts = append(prog.Parts, connProg)
	// }
	// f, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename + PROG_FILE_EXT))
	// if err != nil {
	// 	return err
	// }
	// json.NewEncoder(f).Encode(prog)
	// f.Close()
	return nil
}

func (down *Download) resume(progressFile string) error {
	// var prog progress
	// f, err := os.Open(progressFile)
	// if err != nil {
	// 	return err
	// }
	// if err := json.NewDecoder(f).Decode(&prog); err != nil {
	// 	return err
	// }
	// down.url = prog.Url
	// down.dir = prog.Dir
	// down.filename = prog.Filename
	// go down.updateStatus()
	// for _, conn := range prog.Parts {
	// 	fname := filepath.Join(down.dir, PART_DIR_NAME, down.filename+"."+strconv.Itoa(conn["offset"]))
	// 	file, err := os.OpenFile(fname, os.O_APPEND, 755)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	newConn := connection{
	// 		offset:   conn["offset"],
	// 		length:   conn["length"],
	// 		received: conn["received"],
	// 		done:     down.connDone,
	// 		file:     file,
	// 		dir: down.dir,
	// 	}
	// 	if newConn.received < newConn.length { // unfinished
	// 		_, err := newConn.start(down.url, down.headers)
	// 		if err != nil {
	// 			return err
	// 		}
	// 		down.waitlist.Add(1)
	// 	}
	// 	down.appendConn(&newConn)
	// 	down.length += newConn.length
	// }
	// // add other conns
	// go down.startOthers()
	// os.Remove(progressFile)
	return nil
}

type progress struct {
	Url      string           `json:"url"`
	Dir string `json:"dir"`
	Filename string           `json:"filename"`
	Parts    []map[string]int `json:"parts"`
}

func newDownload(url string, maxConns int, id int, dir string) *Download {
	down := Download{
		url: url,
		dir: dir,
		maxConns:   maxConns,
		jobs: map[int]*downJob{},
		addChan: make(chan *downJob, 10),
		stop:       make(chan os.Signal),
		emitStatus: make(chan status, 1),  // buffered to bypass emitting if no consumer and continue updating, wait()
		stopStatus: make(chan bool),
		stopAdd:    make(chan bool),
		jobDone:   make(chan *downJob),
	}
	return &down
}
