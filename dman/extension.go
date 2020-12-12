// -{go install}
// -{go fmt %f}
package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
)

type incoming struct {
	Type string // new, resume, info
	Url  string
	Id   int
}

type outgoing struct {
	Type   string   `json:"type"` // new, resume, info, progress
	Status []status `json:"status,omitempty"`
	Id     int      `json:"id,omitempty"`
}

var byteOrder = binary.LittleEndian

func extension() error {
	for {
		// decode message
		length := make([]byte, 4)
		_, err := os.Stdin.Read(length)
		if err != nil {
			return err
		}
		lengthNum := int(byteOrder.Uint32(length))
		content := make([]byte, lengthNum)
		os.Stdin.Read(content)
		var inbox incoming
		json.Unmarshal(content, &inbox)

		// reply
		outbox := outgoing{Type: inbox.Type}
		message, err := json.Marshal(outbox)
		if err != nil {
			return err
		}
		length = make([]byte, 4)
		byteOrder.PutUint32(length, uint32(len(message)))
		os.Stdout.Write(length)
		os.Stdout.Write(message)
	}
}
