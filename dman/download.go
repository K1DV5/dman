// -{go fmt %f}

package main

import (
	"encoding/json"
	// "fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LEN_CHECK     = 1 << 16              // 64KB, data interval to check if connection should stop
	LEN_CACHE     = LEN_CHECK * 32       // in memory cache length for less io
	MIN_CUT_ETA   = 5 * int(time.Second) // min ramaining time to split connection
	STAT_INTERVAL = 500 * time.Millisecond
	LONG_TIME     = 3 * 24 * int(time.Hour) // 3 days, arbitrarily large duration
	PART_DIR_NAME = ".dman"
)

// error checker
func check(err error) {
	if err != nil {
		panic(err)
	}
}

type status struct {
	speed   float64
	written int
	percent float64
	conns   int
}

func getFilename(resp *http.Response) string {
	var filename string
	if disposition := resp.Header["Content-Disposition"]; len(disposition) > 0 {
		filename = disposition[0]
	} else {
		url_parts := strings.Split(resp.Request.URL.Path, "/")
		filename = url_parts[len(url_parts)-1]
	}
	return filename
}

type connection struct {
	offset, length, received, receivedTemp, eta int
	stop                          chan bool
	filename                      string // the name of the temp part file
	file                          *os.File
	done chan bool
}

func (conn *connection) start(url string, headers [][]string) *http.Response {
	req, err := http.NewRequest("GET", url, nil)
	check(err)
	for _, pair := range headers {
		req.Header.Add(pair[0], pair[1])
	}
	if conn.length > 0 {
		// request partial content
		req.Header.Add("Range", "bytes="+strconv.Itoa(conn.offset+conn.received)+"-"+strconv.Itoa(conn.offset+conn.length-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	} else if conn.length > 0 && resp.StatusCode != 206 { // partial content
		resp.Body.Close()
		if resp.StatusCode == 200 {
			panic("Connection not resumable")
		} else {
			panic(resp.Status)
		}
	} else if conn.length == 0 { // full content, probably for first connection
		if resp.StatusCode != 200 {
			resp.Body.Close()
			panic(resp.Status)
		}
	}
	conn.length = int(resp.ContentLength)
	conn.stop = make(chan bool)
	conn.eta = LONG_TIME
	if conn.file == nil { // new
		conn.filename = PART_DIR_NAME + "/" + getFilename(resp) + "." + strconv.Itoa(conn.offset)
		file, err := os.Create(conn.filename)
		check(err)
		conn.file = file
	} else { // resume
		conn.filename = conn.file.Name()
	}
	go conn.download(resp.Body)
	return resp
}

func (conn *connection) download(body io.ReadCloser) {
	// also cache chunks for faster writes
	defer body.Close()
	cache := make([]byte, LEN_CACHE)
	var cacheI int
	flushI := LEN_CACHE - LEN_CHECK
	conn.receivedTemp = conn.received
	for conn.length-conn.receivedTemp > LEN_CHECK {
		select {
		case <-conn.stop:
			return
		default:
			body.Read(cache[cacheI : cacheI+LEN_CHECK])
			conn.receivedTemp += LEN_CHECK
			if cacheI == flushI { // flush cache
				conn.file.Write(cache[:])
				conn.received = conn.receivedTemp
				cache = make([]byte, LEN_CACHE)
				cacheI = 0
			} else {
				cacheI += LEN_CHECK
			}
		}
	}
	select {
	case <-conn.stop:
		return
	default:
		if conn.receivedTemp < conn.length {
			remaining := conn.length - conn.receivedTemp
			body.Read(cache[cacheI : cacheI+remaining])
			conn.receivedTemp += remaining
			cacheI += remaining
		}
		conn.file.Write(cache[:cacheI])
		conn.received = conn.receivedTemp
		conn.done <- true
	}
}

type Download struct {
	// Required:
	url      string
	maxConns int
	// status
	emitStatus chan status
	stopStatus chan bool
	// Dynamically set:
	headers     [][]string
	filename    string
	length      int
	waitlist    sync.WaitGroup
	connDone chan bool
	connections []*connection
	stopAdd     chan bool
}

func (down *Download) addConn() bool {
	var longest *connection // connection having the longest undownloaded part
	longestFree := 0
	for _, conn := range down.connections {
		free := conn.length - conn.receivedTemp // not yet downloaded
		if free > longestFree {
			longest = conn
			longestFree = free
		}
	}
	if longest == nil || longest.eta < MIN_CUT_ETA / int(time.Second) {
		return false
	}
	newLen := int(math.Ceil(float64(longestFree / 2)))
	newConn := &connection{
		offset: longest.offset + longest.length - newLen,
		length: newLen,
		done: down.connDone,
	}
	resp := newConn.start(down.url, down.headers)
	if resp == nil {
		return false
	}
	down.waitlist.Add(1)
	// shorten previous connection
	longest.length -= newLen
	// add this conn to the collection
	down.connections = append(down.connections, newConn)
	return true
}

func (down *Download) updateStatus() {
	var lastTime time.Time
	connLastReceived := make([]int, down.maxConns)
	var written, lastWritten, conns int
	for {
		select {
		case <-down.stopStatus:
			return
		case now := <-time.After(STAT_INTERVAL):
			duration := int(now.Sub(lastTime))
			lastTime = now
			written, lastWritten, conns = 0, 0, 0
			for i, conn := range down.connections {
				if len(connLastReceived) == i {
					connLastReceived = append(connLastReceived, 0)
				}
				speed := (conn.receivedTemp - connLastReceived[i]) * int(time.Second) / duration
				if speed == 0 {
					conn.eta = LONG_TIME
				} else {
					conn.eta = (conn.length - conn.receivedTemp) / speed  // in seconds
				}
				lastWritten += connLastReceived[i]
				connLastReceived[i] = conn.receivedTemp
				written += conn.receivedTemp
				if conn.received < conn.length {
					conns++
				}
			}
			writtenDelta := written - lastWritten
			if down.emitStatus != nil {
				down.emitStatus <- status{
					speed:   float64(writtenDelta) / float64(duration),
					percent: float64(written) / float64(down.length) * 100,
					written: written,
					conns:   conns,
				}
			}
		}
	}
}

func (down *Download) start() {
	os.Mkdir(PART_DIR_NAME, 666)
	firstConn := connection{done: down.connDone}
	resp := firstConn.start(down.url, down.headers)
	if resp == nil {
		panic("Error")
	}
	// wait for firstConn
	down.waitlist.Add(1)
	// get filename
	down.filename = getFilename(resp)
	down.length = firstConn.length
	down.connections = append(down.connections, &firstConn)
	go down.updateStatus()
	go down.startOthers()
}

func (down *Download) startOthers() {
	// add connections
	toAdd := down.maxConns
	for _, conn := range down.connections {
		if conn.received < conn.length {
			toAdd--
		}
	}
	for i := 0; i < toAdd; i++ {
		select {
		case <-down.stopAdd:
			return
		default:
			if !down.addConn() { // fail
				break
			}
		}
	}
	// retasking
	for {
		select {
		case <-down.stopAdd:
			return
		case <-down.connDone:
			down.addConn()
			down.waitlist.Done()
		}
	}
}

func (down *Download) rebuild() {
	// sort by offset
	sort.Slice(down.connections, func(i, j int) bool { return down.connections[i].offset < down.connections[j].offset })
	file := down.connections[0].file
	file.Truncate(int64(down.connections[0].length))
	for _, conn := range down.connections[1:] {
		conn.file.Seek(0, 0)
		io.CopyN(file, conn.file, int64(conn.length))
		conn.file.Close()
		os.Remove(conn.file.Name())
	}
	file.Close()
	os.Rename(file.Name(), down.filename)
}

func (down *Download) wait(interrupt chan os.Signal) bool {
	over := make(chan bool)
	go func() {
		down.waitlist.Wait()
		over <- true // finished normally
	}()
	var finished bool
	select {
	case <-interrupt:
		// abort/pause
		down.stopAdd <- true
		wg := sync.WaitGroup{}
		for _, conn := range down.connections {
			wg.Add(1)
			go func(conn *connection) {
				conn.stop <- true
				wg.Done()
			}(conn)
		}
		wg.Wait()
	case <-over:
		finished = true
	}
	close(down.stopAdd)
	// stop eta calculation
	down.stopStatus <- true
	close(down.stopStatus)
	if down.emitStatus != nil {
		close(down.emitStatus)
	}
	return finished
}

func (down *Download) saveProgress() {
	prog := progress{Url: down.url, Filename: down.filename}
	for _, conn := range down.connections {
		connProg := map[string]int{
			"offset":   conn.offset,
			"length":   conn.length,
			"received": conn.received,
		}
		prog.Parts = append(prog.Parts, connProg)
	}
	data, err := json.Marshal(prog)
	check(err)
	f, err := os.Create(down.filename + ".dman")
	check(err)
	f.Write(data)
	f.Close()
}

func (down *Download) resume(progressFile string) bool {
	var prog progress
	f, err := os.Open(progressFile)
	check(err)
	json.NewDecoder(f).Decode(&prog)
	down.url = prog.Url
	down.filename = prog.Filename
	check(err)
	go down.updateStatus()
	for _, conn := range prog.Parts {
		newConn := connection{
			offset:   conn["offset"],
			length:   conn["length"],
			received: conn["received"],
			done: down.connDone,
		}
		resp := newConn.start(down.url, down.headers)
		if resp == nil {
			return false
		}
		down.waitlist.Add(1)
		down.connections = append(down.connections, &newConn)
		down.length += conn["length"]
	}
	// add other conns
	go down.startOthers()
	return true
}

type progress struct {
	Url      string           `json:"url"`
	Filename string           `json:"filename"`
	Parts    []map[string]int `json:"parts"`
}

func newDownload(url string, maxConns int) *Download {
	down := Download{
		url:        url,
		maxConns:   maxConns,
		stopStatus: make(chan bool),
		stopAdd:    make(chan bool),
		connDone:   make(chan bool),
	}
	return &down
}
