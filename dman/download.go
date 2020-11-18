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

// error checker
func check(err error) {
	if err != nil {
		panic(err)
	}
}

type writeInfo struct {
	start int64
	data []byte
	last bool
}

type connection struct {
	start, length, received, eta, lastReceived int
	bufLen                                     int64
	stop                                       chan bool
	body                                       io.ReadCloser
}

func (conn *connection) DownloadBody(writer chan writeInfo) {
	// also cache chunks for faster writes
	defer conn.body.Close()
	cacheLen := int(20 * conn.bufLen)
	cache := make([]byte, cacheLen)
	var cacheI int
	flushI := cacheLen - int(conn.bufLen)
	for conn.length-conn.received > int(conn.bufLen) {
		select {
		case <-conn.stop:
			return
		default:
			conn.body.Read(cache[cacheI:cacheI + int(conn.bufLen)])
			conn.received += int(conn.bufLen)
			if cacheI == flushI {  // flush cache
				writer <- writeInfo{
					start: int64(conn.start + conn.received - len(cache)),
					data: cache[:],
				}
				cache = make([]byte, cacheLen)
				cacheI = 0
			} else {
				cacheI += int(conn.bufLen)
			}
		}
	}
	select {
	case <-conn.stop:
		return
	default:
		if conn.received < conn.length {
			remaining := conn.length - conn.received
			conn.body.Read(cache[cacheI:cacheI + remaining])
			conn.received += int(remaining)
			cacheI += int(remaining)
		}
		writer <- writeInfo{
			start: int64(conn.start + conn.received - cacheI),
			data: cache[:cacheI],
			last: true,
		}
	}
}

type Download struct {
	// Required:
	url        string
	maxConns   int
	minCutSize int
	minCutEta  int
	bufLen     int64
	stopAdd    chan bool
	// status
	statInterval time.Duration
	written      int
	speed        float64
	percent      float64
	stopStatus   chan bool
	// Dynamically set:
	headers     [][2]string
	destination *os.File
	filename    string
	length      int
	waitlist    sync.WaitGroup
	connections []*connection
	writer    chan writeInfo
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

func (down *Download) newConn(retask bool) (*connection, int) {
	var longest *connection // connection having the longest undownloaded part
	longestFree := 0
	longestI := 0
	for i, conn := range down.connections {
		free := conn.length - conn.received // not yet downloaded
		if free > longestFree {
			longest = conn
			longestFree = free
			longestI = i
		}
	}
	if longest == nil || retask && longest.eta < down.minCutEta {
		return nil, -1
	}
	newLen := int(math.Ceil(float64(longestFree / 2)))
	return &connection{
		start:  longest.start + longest.length - newLen,
		length: newLen,
	}, longestI
}

func (down *Download) addHeaders(req *http.Request) {
	for _, pair := range down.headers {
		req.Header.Add(pair[0], pair[1])
	}
}

func (down *Download) firstConn() (io.ReadCloser, string, int) {
	// first one
	req, err := http.NewRequest("GET", down.url, nil)
	check(err)
	down.addHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	check(err)
	if resp.StatusCode != 200 {
		panic(resp.Status)
	}
	length, err := strconv.Atoi(resp.Header["Content-Length"][0])
	check(err)
	disposition := resp.Header["Content-Disposition"]
	var filename string
	if len(disposition) > 0 {
		filename = disposition[0]
	} else {
		url_parts := strings.Split(down.url, "/")
		filename = url_parts[len(url_parts)-1]
	}
	return resp.Body, filename, length
}

func (down *Download) addConn(conn *connection, cutConnI int) bool {
	req, err := http.NewRequest("GET", down.url, nil)
	check(err)
	down.addHeaders(req)
	req.Header.Add("Range", "bytes="+strconv.Itoa(conn.start + conn.received)+"-"+strconv.Itoa(conn.start+conn.length-1))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	} else if resp.StatusCode != 206 {
		if resp.StatusCode == 200 {
			fmt.Println("Connection not resumable")
			resp.Body.Close()
			return false
		} else {
			panic(resp.Status)
		}
	}
	if cutConnI > -1 {
		cutConn := down.connections[cutConnI]
		if cutConn.start+cutConn.received >= conn.start {
			// no need for this one, go home
			return false
		}
	}
	conn.body = resp.Body
	conn.stop = make(chan bool)
	conn.bufLen = down.bufLen
	down.waitlist.Add(1)
	// shorten last connection
	if cutConnI > -1 {
		down.connections[cutConnI].length -= conn.length
	}
	// add this conn to the collection, make sure to make it < down.maxConns
	down.connections = append(down.connections, conn)
	go conn.DownloadBody(down.writer)
	return true
}

// This is the goroutine that will have access to the destination file
func (down *Download) writeData() {
	defer down.destination.Close()
	for info := range down.writer {
		down.destination.Seek(info.start, 0)
		_, err := down.destination.Write(info.data)
		check(err)
		down.written += len(info.data)
		if !info.last {
			continue
		}
		// last one
		down.waitlist.Done()
		// try to start another one
		conn, cutConnI := down.newConn(true)
		if conn == nil {
			continue
		}
		cutConn := down.connections[cutConnI]
		if cutConn.length-cutConn.received > down.minCutSize {
			// cutConn needs help
			down.addConn(conn, cutConnI)
		}
	}
}

func (down *Download) updateStatus() {
	var lastTime time.Time
	lastWritten := 0
	for {
		select {
		case <-down.stopStatus:
			return
		case now := <-time.After(down.statInterval):
			duration := int(now.Sub(lastTime))
			lastTime = now
			for _, conn := range down.connections {
				receivedDelta := conn.received - conn.lastReceived
				speed := receivedDelta / duration
				if speed == 0 {
					conn.eta = 3 * 24 * 3600 * int(time.Second) // 3 days, arbitrarily large value
				} else {
					conn.eta = (conn.length - conn.received) / speed
				}
				conn.lastReceived = conn.received
			}
			writtenDelta := down.written - lastWritten
			lastWritten = down.written
			down.speed = float64(writtenDelta) / float64(duration)
			down.percent = float64(down.written) / float64(down.length) * 100
		}
	}
}

func (down *Download) startFirst() {
	firstBody, filename, length := down.firstConn()
	down.length = length
	firstConn := &connection{
		body:   firstBody,
		length: length,
		stop:   make(chan bool),
		bufLen: down.bufLen,
	}
	down.connections = append(down.connections, firstConn)
	// prepare destination file
	file, err := os.Create(filename)
	check(err)
	down.destination = file
	down.filename = filename
	// for each goroutine to send their part to the file writer
	down.writer = make(chan writeInfo)
	// prepare file writer routine, accepts info from chan
	go down.writeData()
	// write to file from first connection
	go firstConn.DownloadBody(down.writer)
	// synchronize completions
	down.waitlist.Add(1)
	// update eta of each connection
	down.stopStatus = make(chan bool)
	go down.updateStatus()
}

func (down *Download) startAdd() {
	// add connections
	toAdd := down.maxConns - len(down.connections)
	for i := 0; i < toAdd; i++ {
		select {
		case <-down.stopAdd:
			return
		default:
			conn, cutConnI := down.newConn(false)
			ok := down.addConn(conn, cutConnI)
			if !ok {
				break
			}
		}
	}
}

func (down *Download) wait(interrupt chan os.Signal) bool {
	overChan := make(chan bool)
	go func() {
		down.waitlist.Wait()
		overChan <- true // finished normally
	}()
	var finished bool
	select {
	case <-interrupt:
		// abort/pause
		if len(down.connections) < down.maxConns {
			down.stopAdd <- true
		}
		for _, conn := range down.connections {
			fmt.Println(conn.stop)
			conn.stop <- true
		}
	case <-overChan:
		finished = true
	}
	close(down.stopAdd)
	close(down.writer) // stop writeData
	// stop eta calculation
	down.stopStatus <- true
	close(down.stopStatus)
	return finished
}

func (down *Download) saveProgress() {
	prog := progress{Url: down.url, Filename: down.filename}
	for _, conn := range down.connections {
		connProg := map[string]int{
			"start": conn.start,
			"length": conn.length,
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

func (down *Download) fromProgress(filename string) bool {
	var prog progress
	f, err := os.Open(filename)
	check(err)
	json.NewDecoder(f).Decode(&prog)
	down.url = prog.Url
	down.filename = prog.Filename
	file, err := os.OpenFile(down.filename, os.O_RDWR, 0644)
	down.destination = file
	check(err)
	// for each goroutine to send their part to the file writer
	down.writer = make(chan writeInfo, 10)
	// prepare file writer routine, accepts info from chan
	go down.writeData()
	// update eta of each connection
	down.stopStatus = make(chan bool)
	go down.updateStatus()
	for _, conn := range prog.Parts {
		added := down.addConn(&connection{
			start: conn["start"],
			length: conn["length"],
			received: conn["received"],
		}, -1)
		if !added {
			return false
		}
		down.written += conn["received"]
		down.length += conn["length"]
	}
	return true
}

type progress struct {
	Url string `json:"url"`
	Filename string `json:"filename"`
	Parts []map[string]int `json:"parts"`
}
