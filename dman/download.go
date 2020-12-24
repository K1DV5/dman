// -{go install}
// -{go fmt %f}

package main

import (
	"encoding/json"
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
	"time"
)

const (
	LEN_CHECK      = 1 << 16              // 64KB, data interval to check if connection should stop
	MIN_CUT_ETA    = 5 * int(time.Second) // min ramaining time to split connection
	STAT_INTERVAL  = 500 * time.Millisecond
	LONG_TIME      = 3 * 24 * int(time.Hour) // 3 days, arbitrarily large duration
	PART_DIR_NAME  = ".dman"
	PROG_FILE_EXT  = ".dman"
	SPEED_HIST_LEN = 10
	KB             = 1024
	MB             = KB * KB
	GB             = MB * KB
)

type status struct {
	Id         int     `json:"id,omitempty"`
	Rebuilding bool    `json:"rebuilding,omitempty"`
	Speed      string  `json:"speed,omitempty"`
	Written    int     `json:"written,omitempty"`
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

type connection struct {
	offset, length, received, eta int
	stop                          chan bool
	file                          *os.File
	dir string
	done                          chan *connection
	lock                          sync.Mutex
}

func (conn *connection) start(url string, headers [][]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for _, pair := range headers {
		req.Header.Add(pair[0], pair[1])
	}
	if conn.length > 0 { // unknown length, probably additional connection
		// request partial content
		req.Header.Add("Range", "bytes="+strconv.Itoa(conn.offset+conn.received)+"-"+strconv.Itoa(conn.offset+conn.length-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	} else if conn.length > 0 && resp.StatusCode != 206 { // partial content
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return nil, fmt.Errorf("Connection not resumable")
		} else {
			return nil, fmt.Errorf("Connection error: %s", resp.Status)
		}
	} else if conn.length == 0 { // full content, probably for first connection
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("Bad response: %s", resp.Status)
		}
		if conn.received == 0 { // not resumed
			conn.length = int(resp.ContentLength)
		}
	}
	conn.stop = make(chan bool)
	conn.eta = LONG_TIME
	if conn.file == nil { // new, not resuming
		file, err := os.Create(filepath.Join(conn.dir, PART_DIR_NAME, getFilename(resp) + "." + strconv.Itoa(conn.offset)))
		if err != nil {
			return nil, err
		}
		conn.file = file
	}
	go conn.download(resp.Body)
	return resp, nil
}

func (conn *connection) download(body io.ReadCloser) {
	// also cache chunks for faster writes
	defer body.Close()
	for {
		conn.lock.Lock()
		select {
		case <-conn.stop:
			return
		default:
		}
		if conn.length > 0 && conn.length-conn.received < LEN_CHECK { // final
			remaining := conn.length - conn.received
			if remaining > 0 {
				io.CopyN(conn.file, body, int64(remaining))
				conn.received += remaining
			} else if remaining < 0 { // received too much
				conn.file.Truncate(int64(conn.length))
				conn.received = conn.length
			}
			conn.lock.Unlock()
			conn.done <- nil
			break
		}
		wrote, err := io.CopyN(conn.file, body, int64(LEN_CHECK))
		if err != nil {
			conn.received += int(wrote)
			conn.length = conn.received
			conn.lock.Unlock()
			if err == io.EOF {
				conn.done <- nil
			} else {
				conn.done <- conn
			}
			break
		}
		conn.received += LEN_CHECK
		conn.lock.Unlock()
	}
}

type Download struct {
	// Required:
	id       int
	url      string
	dir 	string
	maxConns int
	// status
	emitStatus chan status
	stopStatus chan bool
	// Dynamically set:
	headers     [][]string
	filename    string
	length      int
	waitlist    *sync.WaitGroup
	connDone    chan *connection
	connections []*connection
	stopAdd     chan bool
	stop        chan os.Signal
}

func (down *Download) addConn() error {
	var longest *connection // connection having the longest undownloaded part
	longestFree := 0
	for _, conn := range down.connections {
		conn.lock.Lock()
		free := conn.length - conn.received // not yet downloaded
		if free > longestFree {
			if longest != nil { // bigger one found
				longest.lock.Unlock()
			}
			longest = conn
			longestFree = free
		} else {
			conn.lock.Unlock()
		}
	}
	if longest != nil {
		defer longest.lock.Unlock()
	}
	if longest == nil || longest.eta < MIN_CUT_ETA/int(time.Second) {
		return fmt.Errorf("No connection to split found")
	}
	newLen := int(math.Ceil(float64(longestFree / 2)))
	newConn := &connection{
		offset: longest.offset + longest.length - newLen,
		length: newLen,
		done:   down.connDone,
		dir: down.dir,
	}
	_, err := newConn.start(down.url, down.headers)
	if err != nil {
		return err
	}
	down.waitlist.Add(1)
	// shorten previous connection
	longest.length -= newLen
	// add this conn to the collection
	down.connections = append(down.connections, newConn)
	return nil
}

func (down *Download) updateStatus() {
	var lastTime time.Time
	connLastReceived := make([]int, down.maxConns)
	var written, lastWritten, conns int
	var rebuilding bool
	var speedHist [SPEED_HIST_LEN]int
	for {
		select {
		case <-down.stopStatus:
			return
		case now := <-time.After(STAT_INTERVAL):
			duration := int(now.Sub(lastTime))
			lastTime = now
			if rebuilding { // finished downloading, rebuilding
				stat, _ := down.connections[0].file.Stat()
				down.emitStatus <- status{
					Id: down.id,
					Rebuilding: true,
					Percent:    float64(stat.Size()) / float64(down.length) * 100,
				}
				continue
			}
			written, lastWritten, conns = 0, 0, 0
			for i, conn := range down.connections {
				if len(connLastReceived) == i {
					connLastReceived = append(connLastReceived, 0)
				}
				speed := (conn.received - connLastReceived[i]) * int(time.Second) / duration
				if speed == 0 {
					conn.eta = LONG_TIME
				} else {
					conn.eta = (conn.length - conn.received) / speed // in seconds
				}
				lastWritten += connLastReceived[i]
				connLastReceived[i] = conn.received
				written += conn.received
				if conn.received < conn.length {
					conns++
				}
			}
			if down.emitStatus != nil {
				// moving average speed
				speedNow := (written - lastWritten) * int(time.Second) / duration
				var speed int
				for i, sp := range speedHist[1:] {
					speedHist[i] = sp
					speed += sp
				}
				speedHist[len(speedHist)-1] = speedNow
				speed = (speed + speedNow) / len(speedHist)
				var eta string
				if speed == 0 {
					eta = "LongTime"
				} else {
					eta = time.Duration((down.length - written) * int(time.Second) / speed).Round(time.Second).String()
				}
				speedVal, unit := readableSize(speed)
				down.emitStatus <- status{
					Id:      down.id,
					Speed:   fmt.Sprintf("%.2f%s/s", speedVal, unit),
					Percent: float64(written) / float64(down.length) * 100,
					Written: written,
					Conns:   conns,
					Eta:     eta,
				}
			}
			if written >= down.length {
				rebuilding = true
			}
		}
	}
}

func (down *Download) start() error {
	os.Mkdir(filepath.Join(down.dir, PART_DIR_NAME), 666)
	firstConn := connection{done: down.connDone, dir: down.dir}
	resp, err := firstConn.start(down.url, down.headers)
	if err != nil {
		return err
	}
	// wait for firstConn
	down.waitlist.Add(1)
	// get filename
	down.filename = getFilename(resp)
	down.length = firstConn.length
	down.connections = append(down.connections, &firstConn)
	go down.updateStatus()
	go down.startOthers()
	return nil
}

func (down *Download) startOthers() {
	// add connections
	toAdd := down.maxConns
	if down.length < 0 { // unknown size, single connection
		toAdd = 0
	}
	for _, conn := range down.connections {
		conn.lock.Lock()
		if conn.received < conn.length {
			toAdd--
		}
		conn.lock.Unlock()
	}
	for i := 0; i < toAdd; i++ {
		select {
		case <-down.stopAdd:
			return
		default:
			if down.addConn() != nil { // fail
				break
			}
		}
	}
	// retasking
	var failedConns []*connection
	for {
		select {
		case <-down.stopAdd:
			return
		case conn := <-down.connDone:
			if conn == nil { // finished
				if down.length > 0 {
					down.addConn()
				}
				down.waitlist.Done()
				for _, conn := range failedConns {
					conn.start(down.url, down.headers)
				}
			} else { // failed
				_, err := conn.start(down.url, down.headers)
				if err != nil {
					failedConns = append(failedConns, conn)
				}
			}
		}
	}
}

func (down *Download) wait() bool {
	over := make(chan bool)
	go func() {
		down.waitlist.Wait()
		over <- true // finished normally
	}()
	var finished bool
	select {
	case <-down.stop:
		// abort/pause
		down.stopAdd <- true
		wg := sync.WaitGroup{}
		for _, conn := range down.connections {
			conn.lock.Lock()
			if conn.received == conn.length { // finished
				conn.lock.Unlock()
				continue
			}
			wg.Add(1)
			go func(conn *connection) {
				conn.stop <- true
				wg.Done()
			}(conn)
			conn.lock.Unlock()
		}
		wg.Wait()
	case <-over:
		down.stopAdd <- true
		finished = true
	}
	close(down.stopAdd)
	close(down.connDone)
	return finished
}

func (down *Download) rebuild() error {
	// sort by offset
	sort.Slice(down.connections, func(i, j int) bool {
		return down.connections[i].offset < down.connections[j].offset
	})
	file := down.connections[0].file
	if down.length < 0 { // unknown file size, single connection, length set at end
		down.length = down.connections[0].length
	}
	for _, conn := range down.connections[1:] {
		if _, err := conn.file.Seek(0, 0); err != nil {
			return err
		}
		if _, err := io.Copy(file, conn.file); err != nil {
			return err
		}
		if err := conn.file.Close(); err != nil {
			return err
		}
		if err := os.Remove(conn.file.Name()); err != nil {
			return err
		}
	}
	down.stopStatus <- true
	close(down.stopStatus)
	close(down.emitStatus)
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(down.dir, down.filename)); err != nil {
		return err
	}
	os.Remove(filepath.Dir(file.Name())) // only if empty
	return nil
}

func (down *Download) saveProgress() error {
	prog := progress{Url: down.url, Filename: down.filename, Dir: down.dir}
	for _, conn := range down.connections {
		connProg := map[string]int{
			"offset":   conn.offset,
			"length":   conn.length,
			"received": conn.received,
		}
		prog.Parts = append(prog.Parts, connProg)
	}
	f, err := os.Create(filepath.Join(down.dir, PART_DIR_NAME, down.filename + PROG_FILE_EXT))
	if err != nil {
		return err
	}
	json.NewEncoder(f).Encode(prog)
	f.Close()
	return nil
}

func (down *Download) resume(progressFile string) error {
	var prog progress
	f, err := os.Open(progressFile)
	if err != nil {
		return err
	}
	if err := json.NewDecoder(f).Decode(&prog); err != nil {
		return err
	}
	down.url = prog.Url
	down.dir = prog.Dir
	down.filename = prog.Filename
	go down.updateStatus()
	for _, conn := range prog.Parts {
		fname := filepath.Join(down.dir, PART_DIR_NAME, down.filename+"."+strconv.Itoa(conn["offset"]))
		file, err := os.OpenFile(fname, os.O_APPEND, 755)
		if err != nil {
			return err
		}
		newConn := connection{
			offset:   conn["offset"],
			length:   conn["length"],
			received: conn["received"],
			done:     down.connDone,
			file:     file,
			dir: down.dir,
		}
		if newConn.received < newConn.length { // unfinished
			_, err := newConn.start(down.url, down.headers)
			if err != nil {
				return err
			}
			down.waitlist.Add(1)
		}
		down.connections = append(down.connections, &newConn)
		down.length += newConn.length
	}
	// add other conns
	go down.startOthers()
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
		id:         id,
		url:        url,
		dir: dir,
		maxConns:   maxConns,
		stop:       make(chan os.Signal),
		emitStatus: make(chan status),
		stopStatus: make(chan bool),
		stopAdd:    make(chan bool),
		connDone:   make(chan *connection),
		waitlist: &sync.WaitGroup{},
	}
	return &down
}
