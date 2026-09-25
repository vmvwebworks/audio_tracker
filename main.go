// Command audio_tracker plays Guitar Pro 7/8 files through an ASIO device
// while recording an instrument input.
package main

import (
	"log"
	"os"
	"path/filepath"

	"gioui.org/app"

	"github.com/vmvwebworks/audio_tracker/internal/engine"
	"github.com/vmvwebworks/audio_tracker/internal/ui"
)

const soundFont = "GeneralUser-GS.sf2"

func findSoundFont() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, d := range dirs {
		for _, p := range []string{filepath.Join(d, "assets", soundFont), filepath.Join(d, soundFont)} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return filepath.Join("assets", soundFont)
}

func main() {
	eng, err := engine.New(findSoundFont())
	if err != nil {
		log.Fatal(err)
	}
	cfg := ui.LoadConfig()
	go func() {
		if err := ui.Run(eng, cfg); err != nil {
			log.Println(err)
		}
		os.Exit(0)
	}()
	app.Main()
}
