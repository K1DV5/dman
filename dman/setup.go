// -{go install}
package main

import (
	"os"
	// "strings"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// this is for windows
const (
	BIN = "dman.exe"
	NAME = "com.k1dv5.dman"
	DESCRIPTION = "Download manager"
	MANIFEST_FNAME = "nmh-manifest.json"
)

func setup() error {
	fmt.Println("Setting up dman...")
	idBuf := make([]byte, 1)
	fmt.Print("Extension id: ")
	fmt.Scanln(&idBuf)
	id := string(idBuf)

	manifest := map[string]interface{}{
		"name": NAME,
		"description": DESCRIPTION,
		"path": BIN,
		"type": "stdio",
		"allowed_origins": []string{
			fmt.Sprintf("chrome-extension://%s/", id),
		},
	}

	rootPath, err := os.Executable()
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(filepath.Dir(rootPath), MANIFEST_FNAME)
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(manifestFile).Encode(manifest); err != nil {
		return err
	}

	return execCmd("C:\\Windows\\System32\\cmd.exe", []string{
		"/c",
		"REG",
		"ADD",
		"HKCU\\Software\\Google\\Chrome\\NativeMessagingHosts\\" + NAME,
		"/ve",
		"/t",
		"REG_SZ",
		"/d",
		manifestPath,
		"/f",
	})
}

func execCmd(cmd string, args []string) error {
	proc, err := os.StartProcess(cmd, args, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
	if err != nil {
		return err
	}
	proc.Wait()
	return nil
}
