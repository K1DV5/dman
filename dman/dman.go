// -{go run %f download.go extension.go setup.go http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go install}
// -{go run %f download.go extension.go setup.go ./.dman/gparted-live-1.0.0-5-i686.iso.dman}
// -{go fmt %f}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
)

const (
	KB = 1024
	MB = KB * KB
	GB = MB * KB
)

func readableSize(length int) (float64, string) {
	var value = float64(length)
	var unit string
	switch {
	case value > GB:
		value /= GB
		unit = "GB"
	case value > MB:
		value /= MB
		unit = "MB"
	case value > KB:
		value /= KB
		unit = "KB"
	default:
		unit = "B"
	}
	return value, unit
}

func showProgress(statusChan chan status) {
	for stat := range statusChan {
		if stat.rebuilding {
			fmt.Printf("\rRebuilding %.0f%%" + strings.Repeat(" ", 30), stat.percent)
		} else {
			speedVal, unit := readableSize(stat.speed)
			fmt.Printf("\r%.2f%% %.2f%s/s %d connections %s    ", stat.percent, speedVal, unit, stat.conns, stat.eta)
		}
	}
}

func standalone(url string, resume bool) {
	d := newDownload("", 32)
	if resume {
		fmt.Print("Resuming...")
		if err := d.resume(url); err != nil { // set url & filename as well
			fmt.Printf("\rResume error: %s\n", err.Error())
			return
		}
		os.Remove(url)
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
	finished := d.wait()

	if finished {
		d.rebuild()
		fmt.Println("\rFinished", strings.Repeat(" ", 70))
	} else {
		fmt.Printf("\rPaused, saved progress to '%s/%s%s'.", PART_DIR_NAME, d.filename, PROG_FILE_EXT)
		d.saveProgress()
	}
}

func main() {
	if len(os.Args) == 1 {
		setup()
	} else if strings.HasPrefix(os.Args[1], "chrome-extension://") {
		extension()
	} else if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
		standalone(os.Args[1], false)
	} else {
		standalone(os.Args[1], true)
	}
}
