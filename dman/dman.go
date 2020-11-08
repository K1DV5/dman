// -{go run %f download.go http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go install | ..\..\..\..\..\bin\dman http://localhost/Adobe/_Getintopc.com_Duos_x64_x86_installer.zip}
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
		case <-time.After(500 * time.Millisecond):
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
		case <-stop:
			fmt.Print("\r", 100, "%   ")
			stop <- true
			break
		}
	}
}

func main() {
	if len(os.Args) > 1 {
		d := Download{
			url:        os.Args[1],
			maxConns:   32,
			minCutEta:  5 * int(time.Second),
			minCutSize: 1024 * 1024 * 2, // 2MB
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		stopProgress := make(chan bool)
		go showProgress(&d, stopProgress)

		d.start(stop)

		stopProgress <- true
		<-stopProgress
		close(stopProgress)
		close(stop)
	} else { // invocked from chrome
		fmt.Println("No URL given")
	}
}
