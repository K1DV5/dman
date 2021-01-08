// -{go install}
// -{go fmt %f}
package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"fmt"
	"path/filepath"
	"time"
)

var byteOrder = binary.LittleEndian // most likely

type message struct {
	// Incoming types: new, pause, pause-all, resume, info
	// Outgoing types: new, pause, pause-all, resume, info, completed, error
	Type     string `json:"type"`
	Url      string `json:"url,omitempty"`
	Id       int `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size 		string  `json:"size,omitempty"`
	Conns int `json:"conns,omitempty"`
	Stats []status `json:"stats,omitempty"`
	Info 	bool `json:"info,omitempty"`
	Error string `json:"error,omitempty"`
	Dir string `json:"dir,omitempty"`
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

func (msg *message) send() error {
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
	err error
}

type downloads struct {
	collection map[int]*Download
	addChan chan message
	message chan message
	insertDown chan *Download
}

func (downs *downloads) addDownload() {
	for info := range downs.addChan {
		down := newDownload(info.Url, info.Conns, info.Id, info.Dir)
		msg := message{
			Type: "new",
			Id: info.Id,
		}
		var err error
		if info.Url == "" {
			// resuming, set url & filename as well
			err = down.resume(filepath.Join(info.Dir, PART_DIR_NAME, info.Filename + PROG_FILE_EXT))
		} else {
			// new download, set filename as well
			err = down.start()
		}
		if err != nil {
			msg.Error = err.Error()
			msg.send()
			continue
		}
		downs.insertDown <- down
	}
}

func (downs *downloads) sendInfo() bool {
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
	msg := message{
		Type: "info",
		Stats: stats,
	}
	msg.send()
	if len(stats) > 0 || len(downs.collection) > 0 {
		return true
	}
	return false
}

func (downs *downloads) listen() {
	go downs.coordinate()
	for {
		var msg message
		if err := msg.get(); err != nil {  // shutdown
			close(downs.message)
			return
		}
		downs.message <- msg
	}
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
			if !ok {return}
			switch msg.Type {
			case "info":
				sendingInfo = msg.Info
				if sendingInfo {
					timer.Reset(STAT_INTERVAL)
				}
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
				msg := message{
					Type: "error",
					Error: "Message type not recognized",
				}
				msg.send()
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
		case down := <-downs.insertDown:
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
			msg := message{
				Type: "new",
				Id: down.id,
				Url: down.url,
				Filename: down.filename,
				Size: size,
			}
			msg.send()
		case info := <-completed:
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
	}
}

func extension() {
	downs := downloads{
		addChan: make(chan message, 10),
		collection: map[int]*Download{},
		message: make(chan message),
		insertDown: make(chan *Download),
	}
	downs.listen()
}
