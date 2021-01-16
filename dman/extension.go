// -{go install}
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var byteOrder = binary.LittleEndian // most likely

type message struct {
	// Incoming types: new, pause, pause-all, resume, info
	// Outgoing types: new, pause, pause-all, resume, info, completed, error
	Type     string   `json:"type"`
	Url      string   `json:"url,omitempty"`
	Id       int      `json:"id,omitempty"`
	Filename string   `json:"filename,omitempty"`
	Size     string   `json:"size,omitempty"`
	Conns    int      `json:"conns,omitempty"`
	Stats    []status `json:"stats,omitempty"`
	Info     bool     `json:"info,omitempty"`
	Error    string   `json:"error,omitempty"`
	Dir      string   `json:"dir,omitempty"`
}

func (msg *message) get() error {
	length := make([]byte, 4)
	_, err := os.Stdin.Read(length)
	if err != nil {
		return err
	}
	lengthNum := int(byteOrder.Uint32(length))
	content := make([]byte, lengthNum)
	if _, err := os.Stdin.Read(content); err != nil {
		return err
	}
	if err := json.Unmarshal(content, msg); err != nil {
		return err
	}
	return nil
}

func (msg message) send() error {
	message, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	length := make([]byte, 4)
	byteOrder.PutUint32(length, uint32(len(message)))
	if _, err := os.Stdout.Write(append(length, message...)); err != nil {
		return err
	}
	return nil
}

type completedInfo struct {
	down *Download
	err  error
}

type downloads struct {
	collection map[int]*Download
	addChan    chan message
	message    chan message
	insert     chan *Download
}

func (downs *downloads) addDownload() {
	for info := range downs.addChan {
		down := newDownload(info.Url, info.Conns, info.Id, info.Dir)
		msg := message{
			Type: "new",
			Id:   info.Id,
		}
		var errMsg string
		if info.Url == "" {
			// resuming, set url & filename as well
			if err := down.resume(filepath.Join(info.Dir, PART_DIR_NAME, info.Filename+PROG_FILE_EXT)); err != nil {
				errMsg = fmt.Sprintf("\rResume error: %s", err.Error())
			}
		} else if err := down.start(); err != nil {
			// new download, set filename as well
			errMsg = fmt.Sprintf("\rStart error: %s", err.Error())
		}
		if errMsg == "" {
			downs.insert <- down
		} else {
			msg.Error = errMsg
			msg.send()
		}
	}
}

func (downs *downloads) sendInfo() bool {
	if len(downs.collection) == 0 {
		return false
	}
	var stats []status
	for _, down := range downs.collection {
		// get only the available stats, to not block
		select {
		case stat, ok := <-down.emitStatus:
			if ok {
				stats = append(stats, stat)
			}
		default:
			continue
		}
	}
	if len(stats) > 0 {
		message{
			Type:  "info",
			Stats: stats,
		}.send()
	}
	return true
}

func (downs *downloads) listen() {
	go downs.coordinate()
	for {
		var msg message
		if err := msg.get(); err != nil { // shutdown
			close(downs.message)
			return
		}
		downs.message <- msg
	}
}

func (downs *downloads) handleMsg(msg message) {
	switch msg.Type {
	case "pause":
		downs.collection[msg.Id].stop <- os.Interrupt
	case "new":
		downs.addChan <- msg
	case "pause-all":
		pause := func(down *Download) {
			down.stop <- os.Interrupt
		}
		for _, down := range downs.collection {
			go pause(down)
		}
	default:
		message{
			Type:  "error",
			Error: "Message type not recognized",
		}.send()
	}

}

func (downs *downloads) finishInsertDown(down *Download, completed chan completedInfo) {
	downs.collection[down.id] = down
	go func() {
		err := <-down.err
		completed <- completedInfo{down: down, err: err}
	}()
	var size string
	if down.length > 0 {
		sizeVal, unit := readableSize(down.length)
		size = fmt.Sprintf("%.2f%s", sizeVal, unit)
	} else {
		size = "Unknown"
	}
	message{
		Type:     "new",
		Id:       down.id,
		Url:      down.url,
		Dir:      down.dir,
		Filename: down.filename,
		Size:     size,
	}.send()
}

func (downs *downloads) handleCompleted(info completedInfo) {
	delete(downs.collection, info.down.id)
	msg := message{Id: info.down.id}
	if info.err == nil {
		msg.Type = "completed"
	} else if info.err == pausedError {
		msg.Type = "pause"
	} else {
		msg.Type = "failed"
		msg.Error = info.err.Error()
	}
	msg.send()
}

func (downs *downloads) coordinate() {
	defer close(downs.addChan)
	go downs.addDownload()
	timer := time.NewTimer(STAT_INTERVAL)
	defer timer.Stop()
	var sendingInfo bool
	completed := make(chan completedInfo)
	defer close(completed)
	for {
		select {
		case msg, ok := <-downs.message:
			if !ok {
				return
			} else if msg.Type == "info" {
				sendingInfo = msg.Info
				if sendingInfo {
					timer.Reset(STAT_INTERVAL)
				}
			} else {
				downs.handleMsg(msg)
			}
		case <-timer.C:
			if !sendingInfo {
				continue
			}
			if downs.sendInfo() {
				timer.Reset(STAT_INTERVAL)
			} else {
				sendingInfo = false
			}
		case down := <-downs.insert:
			downs.finishInsertDown(down, completed)
		case info := <-completed:
			downs.handleCompleted(info)
		}
	}
}

func extension() {
	downs := downloads{
		addChan:    make(chan message, 10),
		collection: map[int]*Download{},
		message:    make(chan message),
		insert:     make(chan *Download),
	}
	downs.listen()
}
