// -{go install}
// -{go run %f download.go --parent-window=4}
// -{go run %f download.go --resume=./.dman/gparted-live-1.0.0-5-i686.iso.dman}
// -{go run %f download.go --url=http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go fmt %f}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
)

const (
	KB = 1024
	MB = KB * KB
	SPEED_HIST_LEN = 10
)

func showProgress(statusChan chan status) {
	var speedUnit string
	var speedHist [SPEED_HIST_LEN]float64
	for stat := range statusChan {
		if stat.rebuilding {
			fmt.Printf("\rRebuilding %.0f%%" + strings.Repeat(" ", 30), stat.percent)
		} else {
			// moving average speed
			var speed float64
			for i, sp := range speedHist[1:] {
				speedHist[i] = sp
				speed += sp
			}
			speedHist[len(speedHist)-1] = stat.speed
			speed = (speed + stat.speed) / float64(len(speedHist)) * float64(time.Second)
			switch {
			case speed > MB:
				speed /= MB
				speedUnit = "MB"
			case speed > KB:
				speed /= KB
				speedUnit = "KB"
			default:
				speedUnit = "B"
			}
			fmt.Printf("\r%.2f%% %.2f%s/s %d connections    ", stat.percent, speed, speedUnit, stat.conns)
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

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	finished := d.wait(interrupt)

	if finished {
		d.rebuild()
		fmt.Println("\rFinished", strings.Repeat(" ", 70))
	} else {
		fmt.Printf("\rPaused, saved progress to '%s/%s%s'.", PART_DIR_NAME, d.filename, PROG_FILE_EXT)
		d.saveProgress()
	}
	close(interrupt)
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
