// -{go run %f download.go http://localhost/gparted-live-1.0.0-5-i686.iso}
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
	fmt.Printf("Downloading '%s' press Ctrl+C to stop.\n", down.filename)
	var speedUnit string
	for {
		select {
		case <-time.After(down.statInterval):
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
		d := Download{
			url:          os.Args[1],
			maxConns:     32,
			minCutEta:    5 * int(time.Second),
			minCutSize:   1024 * 1024 * 2, // 2MB
			statInterval: 500 * time.Millisecond,
			bufLen:       1024 * 64, // 128KB
			stopAdd:      make(chan bool),
		}
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		stopProgress := make(chan bool)

		d.startFirst() // set filename as well
		go showProgress(&d, stopProgress)
		go d.startAdd()
		finished := d.wait(interrupt)
		stopProgress <- finished
		<-stopProgress
		close(stopProgress)
		close(interrupt)
	} else { // invocked from chrome
		fmt.Println("No URL given")
	}
}
