// Command audio_tracker plays Guitar Pro 7/8 files through an ASIO device
// while recording an instrument input.
package main

import (
	"bytes"
	"os"

	"gioui.org/app"
	"golang.org/x/sys/windows"

	"github.com/vmvwebworks/audio_tracker/internal/engine"
	"github.com/vmvwebworks/audio_tracker/internal/ui"
)

// version is set at build time by scripts/release.ps1 (-X main.version=...).
var version = "dev"

// fatal shows an error dialog: the app has no console to print to.
func fatal(err error) {
	text, _ := windows.UTF16PtrFromString(err.Error())
	caption, _ := windows.UTF16PtrFromString("Audio Tracker " + version)
	windows.MessageBox(0, text, caption, windows.MB_OK|windows.MB_ICONERROR)
	os.Exit(1)
}

func main() {
	eng, err := engine.NewFromReader(bytes.NewReader(embeddedSoundFont))
	if err != nil {
		fatal(err)
	}
	cfg := ui.LoadConfig()
	go func() {
		if err := ui.Run(eng, cfg); err != nil {
			fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
