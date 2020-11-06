// -{go run %f http://localhost/Adobe/_Getintopc.com_Duos_x64_x86_installer.zip}
// -{go run %f http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go fmt %f}
// -{go build %f}

package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
	// "json"
)

// error checker
func check(err interface{}) {
	if err != nil {
		panic(err)
	}
}

type CopyInfo struct {
	from          io.ReadCloser
	start, length int64
	last          bool
}

type connection struct {
	start, length, received, eta, lastReceived int
	body                                       io.ReadCloser
}

func (conn *connection) DownloadBody(copyInfo chan CopyInfo) {
	bufLen := int64(1024 * 64) // 64KB
	remaining := int64(conn.length)
	for remaining > bufLen {
		copyInfo <- CopyInfo{
			from:   conn.body,
			start:  int64(conn.start + conn.received),
			length: bufLen,
		}
		conn.received += int(bufLen)
		remaining = int64(conn.length - conn.received)
	}
	info := CopyInfo{from: conn.body, last: true}
	if remaining > 0 {
		info.start = int64(conn.start + conn.received)
		info.length = remaining
	}
	copyInfo <- info
	conn.received = conn.length
}

func (conn *connection) updateEta(duration int) {
	receivedDelta := conn.received - conn.lastReceived
	speed := receivedDelta / duration
	if speed == 0 {
		conn.eta = 3 * 24 * 3600 * int(time.Second) // 3 days, arbitrarily large value
	} else {
		conn.eta = (conn.length - conn.received) / speed
	}
	conn.lastReceived = conn.received
}

type Download struct {
	// Required:
	url        string
	maxConns   int
	minCutSize int
	minCutEta  int
	// Dynamically set:
	headers     [][]string
	destination *os.File
	filename    string
	length      int
	written     int
	waitlist    sync.WaitGroup
	connections []*connection
	copyInfo    chan CopyInfo
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

func (down *Download) addConn(conn *connection, cutProgI int) bool {
	req, err := http.NewRequest("GET", down.url, nil)
	check(err)
	down.addHeaders(req)
	req.Header.Add("Range", "bytes="+strconv.Itoa(conn.start)+"-"+strconv.Itoa(conn.start+conn.length-1))
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
	cutProg := down.connections[cutProgI]
	if cutProg.start+cutProg.received >= conn.start {
		// no need for this one, go home
		return false
	}
	conn.body = resp.Body
	down.waitlist.Add(1)
	// shorten last connection
	down.connections[cutProgI].length -= conn.length
	// add this conn to the collection, make sure to make it < down.maxConns
	down.connections = append(down.connections, conn)
	go conn.DownloadBody(down.copyInfo)
	return true
}

// This is the goroutine that will have access to the destination file
func (down *Download) copyData() {
	defer down.destination.Close()
	for info := range down.copyInfo {
		down.destination.Seek(info.start, 0)
		_, err := io.CopyN(down.destination, info.from, info.length)
		check(err)
		down.written += int(info.length)
		if info.last {
			info.from.Close()
			down.waitlist.Done()
			// try to start another one
			conn, cutProgI := down.newConn(true)
			if conn == nil {
				continue
			}
			cutProg := down.connections[cutProgI]
			if cutProg.length-cutProg.received < down.minCutSize {
				continue
			}
			// cutProg needs help
			ok := down.addConn(conn, cutProgI)
			if !ok {
				continue
			}
		}
	}
}

func (down *Download) showProgress(stop chan bool) {
	fmt.Printf("Downloading '%s' press Ctrl+C to stop.\n", down.filename)
	lastTime := time.Now()
	lastReceived := 0
	var duration int
	const (
		KB int = 1 << (10 * (iota + 1))
		MB
		GB
	)
	var speedUnit string
	for {
		select {
		case now := <-time.After(500 * time.Millisecond):
			duration = int(now.Sub(lastTime))
			lastTime = now
			received := 0
			for _, conn := range down.connections {
				received += conn.received
				conn.updateEta(duration)
			}
			receivedDelta := received - lastReceived
			lastReceived = received
			switch {
			case receivedDelta > GB:
				receivedDelta /= GB
				speedUnit = "GB"
			case receivedDelta > MB:
				receivedDelta /= MB
				speedUnit = "MB"
			case receivedDelta > KB:
				receivedDelta /= KB
				speedUnit = "KB"
			default:
				speedUnit = "B"
			}
			speed := float64(receivedDelta) / float64(duration) * float64(time.Second)
			percent := float64(received) / float64(down.length) * 100
			fmt.Printf("\r%.2f%% %.2f%s/s %d connections    ", percent, speed, speedUnit, down.getActiveConns())
		case <-stop:
			fmt.Print("\r", 100, "%   ")
			stop <- true
			break
		}
	}
}

func (down *Download) start() {
	// conn trackers (pointers)
	firstBody, filename, length := down.firstConn()
	down.length = length
	firstProg := &connection{
		body:   firstBody,
		length: length,
	}
	down.connections = append(down.connections, firstProg)
	// prepare destination file
	file, err := os.Create(filename)
	check(err)
	down.destination = file
	down.filename = filename
	// for each goroutine to send their part to the file writer
	down.copyInfo = make(chan CopyInfo)
	// init minCutSize
	if down.minCutSize == 0 {
		down.minCutSize = 2e6 // 2MB
	}
	// prepare file writer routine, accepts info from chan
	go down.copyData()
	// write to file from first connection
	go firstProg.DownloadBody(down.copyInfo)
	// synchronize completions
	down.waitlist.Add(1)
	stopProgress := make(chan bool)
	go down.showProgress(stopProgress)
	for i := 1; i < down.maxConns; i++ {
		conn, cutProgI := down.newConn(false)
		ok := down.addConn(conn, cutProgI)
		if !ok {
			break
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	select {
	case <-stop:
		close(stop)
	default:
		down.waitlist.Wait()
	}
	close(down.copyInfo) // stop copyData
	stopProgress <- true
	<-stopProgress
	close(stopProgress)
}

func main() {
	if len(os.Args) > 1 {
		d := Download{
			url:        os.Args[1],
			maxConns:   32,
			minCutEta:  5 * int(time.Second),
			minCutSize: 1024 * 1024 * 2, // 2MB
		}
		d.start()
	} else { // invocked from chrome
		fmt.Println("No URL given")
	}
}
