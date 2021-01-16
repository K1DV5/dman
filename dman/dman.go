// -{go run %f download.go extension.go platform_windows.go http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go run %f download.go extension.go platform_windows.go ./.dman/gparted-live-1.0.0-5-i686.iso.dman}
// -{go fmt %f}
// -{go install}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func showProgress(statusChan chan status) {
	// max width stat:
	// 100.00% 1004.34KB/s x32 10d23h21m23s
	// min width stat:
	// 1.00% 1.34KB/s x2 3s
	// variation space: 16
	for stat := range statusChan {
		if stat.Rebuilding {
			fmt.Printf("\rRebuilding %.0f%%"+strings.Repeat(" ", 19), stat.Percent)
		} else {
			fmt.Printf("\r%.2f%% %s %s x%d %s"+strings.Repeat(" ", 16), stat.Percent, stat.Written, stat.Speed, stat.Conns, stat.Eta)
		}
	}
}

func standalone(url string, resume bool) {
	d := newDownload("", 32, 0, ".")
	if resume {
		fmt.Print("Resuming...")
		if err := d.resume(url); err != nil { // set url & filename as well
			fmt.Printf("\rResume error: %s\n", err.Error())
			return
		}
	} else {
		fmt.Print("Starting...")
		d.url = url
		if err := d.start(); err != nil { // set filename as well
			fmt.Printf("\rError: %s\n", err.Error())
			return
		}
	}

	fmt.Printf("\rDownloading '%s' press Ctrl+C to stop.\n", d.filename)
	go showProgress(d.emitStatus)

	// enable interrupt
	signal.Notify(d.stop, os.Interrupt)
	err := <-d.err

	if err == nil {
		fmt.Println("\rFinished", strings.Repeat(" ", 70))
	} else if err == pausedError {
		fmt.Printf("\rPaused, saved progress to '%s/%s%s'.", PART_DIR_NAME, d.filename, PROG_FILE_EXT)
	} else {
		fmt.Printf("\rFailed: %v\nProgress saved to '%s/%s%s'.", err, PART_DIR_NAME, d.filename, PROG_FILE_EXT)
	}
}

func main() {
	if len(os.Args) == 1 {
		setup()  // platform dependent
	} else if strings.HasPrefix(os.Args[1], "chrome-extension://") {
		extension()
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		standalone(os.Args[1], false)
	} else {
		standalone(os.Args[1], true)
	}
}
