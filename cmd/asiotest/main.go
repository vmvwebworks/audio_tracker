// Command asiotest runs the engine against a real ASIO driver with every
// track muted, recording a few seconds of input, to check the realtime path.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/vmvwebworks/audio_tracker/internal/engine"
	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("uso: asiotest <archivo.gp> <carpeta de salida>")
	}
	eng, err := engine.New(filepath.Join("assets", "GeneralUser-GS.sf2"))
	if err != nil {
		log.Fatal(err)
	}
	if err := eng.OpenDriver(""); err != nil {
		log.Fatal(err)
	}
	defer eng.CloseDriver()
	fmt.Println(eng.DriverName(), "|", eng.Status())
	s, err := gp.Load(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	eng.LoadSong(s, gp.NewTimeline(s))
	eng.SetAudible(make([]bool, len(s.Tracks)), make([]float64, len(s.Tracks)))
	eng.Seek(4)
	wav := filepath.Join(os.Args[2], "asiotest.wav")
	start := time.Now()
	if err := eng.StartRecording(wav); err != nil {
		log.Fatal(err)
	}
	if err := eng.Play(); err != nil {
		log.Fatal(err)
	}
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		eng.Poll()
	}
	pos := eng.Position()
	res, err := eng.Pause()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tiempo real %.2fs, posición %.2fs (inicio 4.00), toma %.2fs, perdidas=%v, pico entrada %.4f\n",
		time.Since(start).Seconds(), pos, res.Duration, res.Dropped, eng.InputPeak())
	st, _ := os.Stat(res.Path)
	fmt.Println("wav bytes:", st.Size())
}
