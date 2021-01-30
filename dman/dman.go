// -{go build | dman ./.dman/gparted-live-1.0.0-5-i686.iso.0.dman}
// -{go build | dman http://localhost/gparted-live-1.0.0-5-i686.iso}
// -{go build | dman ./.dman/gparted-live-1.0.0-5-i686.iso.0.dman http://localhost/foo}
// -{go fmt %f}
// -{go install}

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"github.com/K1DV5/dman/dman/download"
)

func showProgress(status chan download.Status) {
	// max width stat:
	// 100.00% 1004.43MB 1004.34KB/s x32 10d23h21m23s
	// min width stat:
	// 1.00% 1.22MB 1.34KB/s x2 3s
	// Rebuilding stat:
	// Rebuilding 44.33%
	// variation space: normal: 19, rebuilding: 29
	for stat := range status {
		if stat.Rebuilding {
			fmt.Printf("\rRebuilding %.0f%%"+strings.Repeat(" ", 29), stat.Percent)
		} else {
			fmt.Printf("\r%.2f%% %s %s x%d %s"+strings.Repeat(" ", 19), stat.Percent, stat.Written, stat.Speed, stat.Conns, stat.Eta)
		}
	}
}

func standalone() {
	d := download.New("", 32, 0, ".")
	if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {  // new
		fmt.Print("Starting...")
		d.Url = os.Args[1]
		if err := d.Start(); err != nil { // set filename as well
			fmt.Printf("\rError: %s\n", err.Error())
			return
		}
	} else {
		if len(os.Args) > 2 {
			d.Url = os.Args[2]
		}
		fmt.Print("Resuming...")
		if err := d.Resume(os.Args[1]); err != nil { // set url & filename as well
			fmt.Printf("\rResume error: %s\n", err.Error())
			return
		}
	}

	fmt.Printf("\rDownloading '%s' press Ctrl+C to stop.\n", d.Filename)
	go showProgress(d.Status)

	// enable interrupt
	signal.Notify(d.Stop, os.Interrupt)
	err := <-d.Err

	if err == nil {
		fmt.Println("\rFinished", strings.Repeat(" ", 70))
	} else if err == download.PausedError {
		fmt.Printf("\rPaused, saved progress to '%s/%s.%d%s'.", download.PART_DIR_NAME, d.Filename, d.Id, download.PROG_FILE_EXT)
	} else {
		fmt.Printf("\rFailed: %v\nProgress saved to '%s/%s.%d%s'.", err, download.PART_DIR_NAME, d.Filename, d.Id, download.PROG_FILE_EXT)
	}
}

func main() {
	if len(os.Args) == 1 {
		setup() // platform dependent
	} else if strings.HasPrefix(os.Args[1], "chrome-extension://") {
		extension()
	} else {
		standalone()
	}
}
