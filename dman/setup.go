// -{go install}
package main

import (
	"os"
	// "strings"
	"encoding/json"
	"fmt"
	"path/filepath"
	"os/exec"
)

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

	cmd := exec.Command(
		"REG",
		"ADD",
		"HKCU\\Software\\Google\\Chrome\\NativeMessagingHosts\\" + NAME,
		"/ve",
		"/t",
		"REG_SZ",
		"/d",
		manifestPath,
		"/f",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}
