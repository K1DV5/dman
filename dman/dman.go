// -{go run %f download.go http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go run %f download.go gparted-live-1.0.0-5-i686.iso.dman}
// -{go run %f download.go http://localhost/Adobe/_Getintopc.com_Duos_x64_x86_installer.zip}
// -{go fmt %f}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"
)

const (
	KB float64 = 1 << (10 * (iota + 1))
	MB
	GB
)

func showProgress(down *Download, stop chan bool) {
	fmt.Printf("\rDownloading '%s' press Ctrl+C to stop.\n", down.filename)
	var speedUnit string
	for {
		select {
		case <-time.After(STATINTERVAL):  // STATINTERVAL defined in download.go
			speed := down.speed * float64(time.Second)
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
			fmt.Printf("\r%.2f%% %.2f%s/s %d connections    ", down.percent, speed, speedUnit, down.getActiveConns())
		case finished := <-stop:
			if finished {
				fmt.Print("\r100%                             \n")
			} else {
				fmt.Println("\rInterrupted                                   \n")
			}
			stop <- true
			break
		}
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
			resumed := d.fromProgress(os.Args[1])  // set url & filename as well
			if !resumed {
				fmt.Print("\rResume error")
				return
			}
		} else {
			fmt.Print("Starting...")
			d.url = os.Args[1]
			d.startFirst() // set filename as well
		}

		stopProgress := make(chan bool)
		go showProgress(&d, stopProgress)
		go d.startAdd()

		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		finished := d.wait(interrupt)

		stopProgress <- finished
		<-stopProgress
		if !finished {
			d.saveProgress()
		}
		close(stopProgress)
		close(interrupt)
		if resume {
			// delete pause file
		}
	} else { // invocked from chrome
		fmt.Println("No URL given")
	}
}
