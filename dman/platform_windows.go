// -{go install}
package main

import (
	"os"
	// "strings"
	"encoding/json"
	"fmt"
	"path/filepath"
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

	if err := execCmd("REG", []string{
		"ADD",
		"HKCU\\Software\\Google\\Chrome\\NativeMessagingHosts\\" + NAME,
		"/ve",
		"/t",
		"REG_SZ",
		"/d",
		manifestPath,
		"/f",
	}); err != nil {
		return err
	}
	fmt.Println("\nYou can now reload the extension.\nPress [ENTER] to continue.")
	os.Stdin.Read(make([]byte, 1))
	return nil
}

func execCmd(cmd string, args []string) error {
	proc, err := os.StartProcess(
		"C:\\Windows\\System32\\cmd.exe",
		append([]string{"/c", cmd}, args...),
		&os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}},
	)
	if err != nil {
		return err
	}
	proc.Wait()
	return nil
}

func startFile(path string) {
	execCmd("start", []string{path})
}
