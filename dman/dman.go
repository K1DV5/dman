// -{go run %f download.go http://localhost/Getintopc.comWindows_7_Ultimate_SP1_January_2017.iso}
// -{go run %f download.go http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go run %f download.go gparted-live-1.0.0-5-i686.iso.dman}
// -{go run %f download.go http://localhost/Adobe/_Getintopc.com_Adobe_Illustrator_CC_2019_2018-10-29.zip}
// -{go fmt %f}
// -{go install}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"
	"strings"
)

const (
	KB = 1 << (10 * (iota + 1))
	MB
	GB
	SPEED_HIST_LEN = 10
)

func showProgress(statusChan chan status) {
	var speedUnit string
	var speedHist [SPEED_HIST_LEN]float64
	for stat := range statusChan {
		var speed float64
		for i, sp := range speedHist[1:] {
			speedHist[i] = sp
			speed += sp
		}
		speedHist[len(speedHist) - 1] = stat.speed
		speed = (speed + stat.speed) / float64(len(speedHist)) * float64(time.Second)
		switch {
		case speed > GB:
			speed /= GB
			speedUnit = "GB"
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

func main() {
	if len(os.Args) > 1 {
		var resume bool
		if arg := os.Args[1]; arg[:8] != "https://" && arg[:7] != "http://" {
			resume = true
		}
		d := newDownload("", 32)
		if resume {
			fmt.Print("Resuming...")
			resumed := d.resume(os.Args[1]) // set url & filename as well
			if !resumed {
				fmt.Print("\rResume error")
				return
			}
		} else {
			fmt.Print("Starting...")
			d.url = os.Args[1]
			d.start() // set filename as well
		}

		fmt.Printf("\rDownloading '%s' press Ctrl+C to stop.\n", d.filename)
		d.emitStatus = make(chan status, 1)
		go showProgress(d.emitStatus)

		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		finished := d.wait(interrupt)

		if finished {
			fmt.Print("\rRebuilding...", strings.Repeat(" ", 70))
			d.rebuild()
			fmt.Println("\rFinished", strings.Repeat(" ", 70))
		} else {
			fmt.Println("\rPaused", strings.Repeat(" ", 70))
			d.saveProgress()
		}
		close(interrupt)
		if resume {
			os.Remove(os.Args[1])
		}
	} else { // invocked from chrome
		fmt.Println("No URL given")
	}
}
