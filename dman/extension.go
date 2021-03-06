// -{go install}
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/K1DV5/dman/dman/download"
	"io"
	"os"
	"path/filepath"
	"time"
)

var byteOrder = binary.LittleEndian // most likely

type message struct {
	// Incoming types: add, pause, pause-all, resume, info
	// Outgoing types: add, pause, pause-all, resume, info, completed, error
	Type     string            `json:"type"`
	Url      string            `json:"url,omitempty"`
	Id       int               `json:"id,omitempty"`
	Filename string            `json:"filename,omitempty"`
	Size     string            `json:"size,omitempty"`
	Conns    int               `json:"conns,omitempty"`
	Stats    []download.Status `json:"stats,omitempty"`
	Info     bool              `json:"info,omitempty"`
	Error    string            `json:"error,omitempty"`
	Dir      string            `json:"dir,omitempty"`
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
	down *download.Download
	err  error
}

type downloads struct {
	collection map[int]*download.Download
	addChan    chan message
	message    chan message
	insert     chan *download.Download
}

func (downs *downloads) addDownload() {
	for info := range downs.addChan {
		down := download.New(info.Url, info.Conns, info.Id, info.Dir)
		msg := message{
			Type: "add",
			Id:   info.Id,
		}
		var errMsg string
		if info.Filename == "" { // new
			// create dir if it doesn't exist
			os.Mkdir(info.Dir, 666)
			if err := down.Start(); err != nil { // set filename as well
				errMsg = fmt.Sprintf("\rStart error: %s", err.Error())
			}
		} else { // resume
			progressFile := filepath.Join(info.Dir, download.PART_DIR_NAME, fmt.Sprintf("%s.%d%s", info.Filename, info.Id, download.PROG_FILE_EXT))
			if err := down.Resume(progressFile); err != nil { // set filename as well
				errMsg = fmt.Sprintf("\rResume error: %s", err.Error())
			}
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
	var stats []download.Status
	for _, down := range downs.collection {
		// get only the available stats, to not block
		select {
		case stat, ok := <-down.Status:
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
	kill := make(chan bool)
	go downs.coordinate(kill)
	for {
		var msg message
		if err := msg.get(); err != nil {
			if err == io.EOF { // shutdown
				close(downs.message)
				<-kill
				return
			}
			message{
				Type:  "error",
				Error: err.Error(),
			}.send()
			continue
		}
		downs.message <- msg
	}
}

func (downs *downloads) handleMsg(msg message) {
	switch msg.Type {
	case "pause":
		down := downs.collection[msg.Id]
		if down == nil {
			message{
				Type:  "pause",
				Id:    msg.Id,
				Error: "Download not in progress.",
			}.send()
		} else {
			downs.collection[msg.Id].Stop <- os.Interrupt
		}
	case "remove":
		go downs.remove(msg)
	case "add":
		downs.addChan <- msg
	case "pause-all":
		for _, down := range downs.collection {
			down.Stop <- os.Interrupt
		}
	case "open":
		go startFile(filepath.Join(msg.Dir, msg.Filename)) // platform dependent
	default:
		message{
			Type:  "error",
			Error: "Message type not recognized",
		}.send()
	}

}

func (downs *downloads) remove(info message) {
	f, err := os.Open(filepath.Join(info.Dir, download.PART_DIR_NAME, fmt.Sprintf("%s.%d%s", info.Filename, info.Id, download.PROG_FILE_EXT)))
	if err != nil {
		message{Type: "error", Error: err.Error()}.send()
		return
	}
	var prog download.Progress
	if err := json.NewDecoder(f).Decode(&prog); err != nil {
		message{Type: "error", Error: err.Error()}.send()
		return
	}
	if err := f.Close(); err != nil {
		message{Type: "error", Error: err.Error()}.send()
		return
	}
	if err := os.Remove(f.Name()); err != nil {
		message{Type: "error", Error: err.Error()}.send()
		return
	}
	for _, part := range prog.Parts {
		fname := filepath.Join(info.Dir, download.PART_DIR_NAME, fmt.Sprintf("%s.%d.%d", info.Filename, info.Id, part["offset"]))
		if err = os.Remove(fname); err != nil {
			message{Type: "error", Error: err.Error()}.send()
			return
		}
	}
	os.Remove(filepath.Join(info.Dir, download.PART_DIR_NAME))
}

func (downs *downloads) finishInsertDown(down *download.Download, completed chan completedInfo) {
	downs.collection[down.Id] = down
	go func() {
		err := <-down.Err
		completed <- completedInfo{down: down, err: err}
	}()
	var size string
	if down.Length > 0 {
		size = download.ReadableSize(down.Length)
	} else {
		size = "Unknown"
	}
	message{
		Type:     "add",
		Id:       down.Id,
		Url:      down.Url,
		Dir:      down.Dir,
		Filename: down.Filename,
		Size:     size,
	}.send()
}

func (downs *downloads) handleCompleted(info completedInfo) {
	delete(downs.collection, info.down.Id)
	msg := message{Id: info.down.Id}
	if info.err == nil {
		msg.Type = "completed"
		msg.Filename = info.down.Filename
		msg.Size = download.ReadableSize(info.down.Length)
	} else if info.err == download.PausedError {
		msg.Type = "pause"
	} else {
		msg.Type = "failed"
		msg.Error = info.err.Error()
	}
	msg.send()
}

func (downs *downloads) coordinate(kill chan bool) {
	defer close(downs.addChan)
	go downs.addDownload()
	timer := time.NewTimer(download.STAT_INTERVAL)
	defer timer.Stop()
	var sendingInfo, stopping bool
	completed := make(chan completedInfo)
	defer close(completed)
	for {
		select {
		case msg, ok := <-downs.message:
			if !ok {
				if len(downs.collection) == 0 {
					close(kill)
					return
				}
				stopping = true
				for _, down := range downs.collection {
					go func(down *download.Download) {
						down.Stop <- os.Interrupt
					}(down)
				}
				timer.Reset(download.STAT_INTERVAL * 2)
			} else if msg.Type == "info" {
				sendingInfo = msg.Info
				if sendingInfo {
					timer.Reset(download.STAT_INTERVAL)
				}
			} else {
				downs.handleMsg(msg)
			}
		case <-timer.C:
			if !sendingInfo {
				continue
			} else if stopping { // stopping timed out
				close(kill)
				return
			}
			if downs.sendInfo() {
				timer.Reset(download.STAT_INTERVAL)
			} else {
				sendingInfo = false
			}
		case down := <-downs.insert:
			downs.finishInsertDown(down, completed)
		case info := <-completed:
			downs.handleCompleted(info)
			if stopping && len(downs.collection) == 0 {
				close(kill)
				return
			}
		}
	}
}

func extension() {
	downs := downloads{
		addChan:    make(chan message, 10),
		collection: map[int]*download.Download{},
		message:    make(chan message),
		insert:     make(chan *download.Download),
	}
	downs.listen()
}
