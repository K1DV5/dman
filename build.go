// -{go run %f}
// -{cd dman | go build -o ../bin/ -ldflags "-s -w"}
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// This is a builder package, not the main program
func main() {
	paths, err := preparePaths()
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	archive, err := os.Create("dman.zip")
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer archive.Close()
	archiveWriter := zip.NewWriter(archive)
	defer archiveWriter.Close()
	for _, p := range paths {
		addToZip(p, archiveWriter)
	}
	fmt.Println("Built.")
}

func preparePaths() ([]string, error) {
	paths, err := filepath.Glob("extension/*.*")
	if err != nil {
		return paths, err
	}
	subPaths, err := filepath.Glob("extension/**/*.*")
	if err != nil {
		return paths, err
	}
	paths = append(paths, subPaths...)
	paths = append(paths, "INSTALLATION.txt")
	paths = append(paths, "LICENSE")
	paths = append(paths, "bin/dman.exe")
	return paths, nil
}

func addToZip(path string, archiveWriter *zip.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(path)
	header.Method = zip.Deflate
	writer, err := archiveWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, file); err != nil {
		return err
	}
	return nil
}
