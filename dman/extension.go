// -{go install}
// -{go fmt %f}
package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"fmt"
	"sync"
	"strings"
	"path/filepath"
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

type downloads struct {
	collection map[int]*Download
	addChan chan message
	statSwitch chan bool
}

func (downs *downloads) startNWait(down *Download) {
	downs.collection[down.id] = down
	err := down.wait()
	if err != nil {
		err = down.saveProgress()
		msg := message{
			Type: "error",
			Error: err.Error(),
		}
		msg.send()
		return
	}
	delete(downs.collection, down.id)
	var msgType string
	if err == nil {
		msgType = "completed"
	} else {
		msgType = "pause"
	}
	msg := message{
		Type: msgType,
		Id: down.id,
	}
	msg.send()
}

func (downs *downloads) addDownload() {
	for info := range downs.addChan {
		down := newDownload(info.Url, info.Conns, info.Id, info.Dir)
		if strings.HasPrefix(down.url, "http://") || strings.HasPrefix(down.url, "https://") {
			// new download
			if err := down.start(); err != nil { // set filename as well
				msg := message{
					Type: "new",
					Id: info.Id,
					Error: err.Error(),
				}
				msg.send()
				continue
			}
		} else {
			// resuming
			if err := down.resume(filepath.Join(info.Dir, PART_DIR_NAME, info.Filename + PROG_FILE_EXT)); err != nil { // set url & filename as well
				msg := message{
					Type: "resume",
					Id: info.Id,
					Error: err.Error(),
				}
				msg.send()
				continue
			}
			os.Remove(down.url)
		}
		go downs.startNWait(down)
		size, unit := readableSize(down.length)
		msg := message{
			Type: info.Type,
			Id: info.Id,
			Url: info.Url,
			Filename: down.filename,
			Size: fmt.Sprintf("%.2f%s", size, unit),
		}
		msg.send()
	}
}

func (downs *downloads) pauseAll() {
	wg := sync.WaitGroup{}
	for _, down := range downs.collection {
		wg.Add(1)
		go func(down *Download) {
			down.stop <- os.Interrupt
			wg.Done()
		}(down)
	}
	wg.Wait()
	var stats []status
	for id, _ := range downs.collection {
		stats = append(stats, status{Id: id})
		delete(downs.collection, id)
	}
	msg := message{
		Type: "pause-all",
		Stats: stats,
	}
	msg.send()
}

func (downs *downloads) sendInfo() {
	var sending bool
	for {
		select {
		case send, ok := <- downs.statSwitch:
			if !ok {
				break
			}
			sending = send
		default:
		}
		if sending {
			var stats []status
			for _, down := range downs.collection {
				stat, ok := <-down.emitStatus
				if !ok {
					continue
				}
				stats = append(stats, stat)
			}
			if len(stats) == 0 {
				msg := message{
					Type: "info",
				}
				msg.send()
				sending = <- downs.statSwitch
				continue
			}
			msg := message{
				Type: "info",
				Stats: stats,
			}
			msg.send()
		} else {
			sending = <- downs.statSwitch
		}
	}
}

func (downs *downloads) listen() {
	go downs.addDownload()
	go downs.sendInfo()
	for {
		var msg message
		if err := msg.get(); err != nil {
			downs.pauseAll()
			msg := message{
				Type: "error",
				Error: err.Error(),
			}
			msg.send()
			return
		}
		// send to workers
		if msg.Type == "info" {
			downs.statSwitch <- msg.Info
		} else if msg.Type == "pause" {
			downs.collection[msg.Id].stop <- os.Interrupt
		} else if msg.Type == "new" || msg.Type == "resume" {
			downs.addChan <- msg
		} else if msg.Type == "pause-all" {
			downs.pauseAll()
		} else {
			msg := message{
				Type: "error",
				Error: "Message type not recognized",
			}
			msg.send()
		}
	}
}

func extension() {
	downs := downloads{
		addChan: make(chan message),
		statSwitch: make(chan bool),
		collection: map[int]*Download{},
	}
	downs.listen()
}
