// -{go fmt %f}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LEN_CHECK       = 1 << 16              // 64KB, data interval to check if connection should stop
	LEN_CACHE       = LEN_CHECK * 32         // in memory cache length for less io
	MIN_CUT_ETA     = 5 * int(time.Second) // min ramaining time to split connection, 5 seconds
	STAT_INTERVAL   = 500 * time.Millisecond
	LONG_TIME       = 3 * 24 * int(time.Hour) // 3 days, arbitrarily large duration
	WRITER_QUEUELEN = 10
)

// error checker
func check(err error) {
	if err != nil {
		panic(err)
	}
}

type writeInfo struct {
	offset int64
	data   []byte
	last   bool
	wrote  chan bool
}

type status struct {
	speed   float64
	written int
	percent float64
	conns   int
}

type connection struct {
	offset, length, received, written, eta int
	stop                          chan bool
	writer                        chan writeInfo
}

func (conn *connection) start(url string, headers [][]string) *http.Response {
	req, err := http.NewRequest("GET", url, nil)
	check(err)
	for _, pair := range headers {
		req.Header.Add(pair[0], pair[1])
	}
	if conn.length > 0 {
		// request partial content
		req.Header.Add("Range", "bytes="+strconv.Itoa(conn.offset+conn.written)+"-"+strconv.Itoa(conn.offset+conn.length-1))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	} else if conn.length > 0 && resp.StatusCode != 206 { // partial content
		resp.Body.Close()
		if resp.StatusCode == 200 {
			fmt.Println("Connection not resumable")
			return nil
		} else {
			panic(resp.Status)
		}
	} else if conn.length == 0 { // full content, probably for first connection
		if resp.StatusCode != 200 {
			resp.Body.Close()
			panic(resp.Status)
		}
	}
	length, err := strconv.Atoi(resp.Header["Content-Length"][0])
	conn.length = length
	conn.received = conn.written
	check(err)
	conn.stop = make(chan bool)
	conn.eta = LONG_TIME
	go conn.download(resp.Body)
	return resp
}

func (conn *connection) download(body io.ReadCloser) {
	// also cache chunks for faster writes
	defer body.Close()
	cache := make([]byte, LEN_CACHE)
	var cacheI int
	flushI := LEN_CACHE - LEN_CHECK
	wrote := make(chan bool)  // to indicate that data has been written to disk
	for conn.length-conn.received > LEN_CHECK {
		select {
		case <-conn.stop:
			return
		default:
			body.Read(cache[cacheI : cacheI+LEN_CHECK])
			conn.received += LEN_CHECK
			if cacheI == flushI { // flush cache
				conn.writer <- writeInfo{
					offset: int64(conn.offset + conn.received - LEN_CACHE),
					data:   cache[:],
					wrote: wrote,
				}
				<-wrote
				conn.written = conn.received
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
		if conn.received < conn.length {
			remaining := conn.length - conn.received
			body.Read(cache[cacheI : cacheI+remaining])
			conn.received += remaining
			cacheI += remaining
		}
		conn.writer <- writeInfo{
			offset: int64(conn.offset + conn.received - cacheI),
			data:   cache[:cacheI],
			last:   true,
			wrote: wrote,
		}
		<-wrote
		conn.written = conn.received
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
	destination *os.File
	filename    string
	length      int
	waitlist    sync.WaitGroup
	connections []*connection
	stopAdd     chan bool
	writer      chan writeInfo
}

func (down *Download) getActiveConns() int {
	conns := 0
	for _, conn := range down.connections {
		if conn.received < conn.length {
			conns++
		}
	}
	return conns
}

func (down *Download) addConn() bool {
	var longest *connection // connection having the longest undownloaded part
	longestFree := 0
	for _, conn := range down.connections {
		free := conn.length - conn.received // not yet downloaded
		if free > longestFree {
			longest = conn
			longestFree = free
		}
	}
	if longest == nil || longest.eta < MIN_CUT_ETA {
		return false
	}
	newLen := int(math.Ceil(float64(longestFree / 2)))
	newConn := &connection{
		offset: longest.offset + longest.length - newLen,
		length: newLen,
		writer: down.writer,
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

// This is the goroutine that will have access to the destination file
func (down *Download) writeData() {
	defer down.destination.Close()
	for info := range down.writer {
		down.destination.WriteAt(info.data, info.offset)
		info.wrote <- true
		if !info.last {
			continue
		}
		// last chunk for this connection
		down.waitlist.Done()
		// try to start another one
		down.addConn()
	}
}

func (down *Download) updateStatus() {
	var lastTime time.Time
	connLastReceived := make([]int, down.maxConns * 3)
	var written, lastWritten int
	for {
		select {
		case <-down.stopStatus:
			return
		case now := <-time.After(STAT_INTERVAL):
			duration := int(now.Sub(lastTime))
			lastTime = now
			written, lastWritten = 0, 0
			for i, conn := range down.connections {
				receivedDelta := conn.received - connLastReceived[i]
				speed := receivedDelta / duration
				if speed == 0 {
					conn.eta = LONG_TIME
				} else {
					conn.eta = (conn.length - conn.received) / speed
				}
				lastWritten += connLastReceived[i]
				connLastReceived[i] = conn.received
				written += conn.received
			}
			writtenDelta := written - lastWritten
			if down.emitStatus != nil {
				down.emitStatus <- status{
					speed:   float64(writtenDelta) / float64(duration),
					percent: float64(written) / float64(down.length) * 100,
					written: written,
					conns:   down.getActiveConns(),
				}
			}
		}
	}
}

func (down *Download) start() {
	firstConn := connection{writer: down.writer}
	resp := firstConn.start(down.url, down.headers)
	if resp == nil {
		panic("error")
	}
	// wait for firstConn
	down.waitlist.Add(1)
	// get filename
	if disposition := resp.Header["Content-Disposition"]; len(disposition) > 0 {
		down.filename = disposition[0]
	} else {
		url_parts := strings.Split(down.url, "/")
		down.filename = url_parts[len(url_parts)-1]
	}
	down.length = firstConn.length
	down.connections = append(down.connections, &firstConn)
	// prepare destination file
	file, err := os.Create(down.filename)
	check(err)
	down.destination = file
	go down.writeData()
	go down.updateStatus()
	go down.startOthers()
}

func (down *Download) startOthers() {
	// add connections
	toAdd := down.maxConns - len(down.connections)
	for i := 0; i < toAdd; i++ {
		select {
		case <-down.stopAdd:
			return
		default:
			ok := down.addConn()
			if !ok {
				break
			}
		}
	}
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
		if len(down.connections) < down.maxConns {
			down.stopAdd <- true
		}
		for _, conn := range down.connections {
			conn.stop <- true
		}
	case <-over:
		finished = true
	}
	close(down.stopAdd)
	close(down.writer) // stop writeData
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
	file, err := os.OpenFile(down.filename, os.O_RDWR, 0644)
	check(err)
	down.destination = file
	go down.writeData()
	go down.updateStatus()
	for _, conn := range prog.Parts {
		newConn := connection{
			offset:   conn["offset"],
			length:   conn["length"],
			received: conn["received"],
			writer:   down.writer,
		}
		resp := newConn.start(down.url, down.headers)
		if resp == nil {
			return false
		}
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
		writer:     make(chan writeInfo, WRITER_QUEUELEN),
		stopStatus: make(chan bool),
		stopAdd:    make(chan bool),
	}
	return &down
}
