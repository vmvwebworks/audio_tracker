package gp_test

import (
	"os"
	"slices"
	"testing"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
	"github.com/vmvwebworks/audio_tracker/internal/testutil"
)

func TestFixture(t *testing.T) {
	s, err := gp.Load(testutil.FixtureGP(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Prueba mínima" || len(s.Tracks) != 2 || len(s.MasterBars) != 5 {
		t.Fatalf("título %q, %d pistas, %d compases", s.Title, len(s.Tracks), len(s.MasterBars))
	}
	gtr, drums := s.Tracks[0], s.Tracks[1]
	if gtr.Percussion || gtr.Program != 29 || !slices.Equal(gtr.Staves[0].Tuning, []int{40, 45, 50, 55, 59, 64}) {
		t.Fatalf("guitarra: %+v", gtr)
	}
	if !drums.Percussion || len(drums.DrumMap) != 2 || drums.Staves[0].Tuning != nil {
		t.Fatalf("batería: %+v", drums)
	}
	if mb := s.MasterBars[3]; mb.Num != 3 || mb.Den != 4 || mb.Duration != 3*gp.TicksPerQuarter {
		t.Fatalf("compás 4: %+v", mb)
	}
	if s.MasterBars[1].Section != "Estribillo" {
		t.Fatalf("sección: %q", s.MasterBars[1].Section)
	}

	// Note pitch: from the Midi property, or tuning + fret when absent.
	first := gtr.Staves[0].Bars[0].Voices[0]
	if first[0].Notes[0].Key != 43 || first[1].Notes[0].Key != 64 {
		t.Fatalf("notas: %d %d", first[0].Notes[0].Key, first[1].Notes[0].Key)
	}
	if k := drums.Staves[0].Bars[0].Voices[0][1].Notes[0].Key; k != 38 {
		t.Fatalf("caja: tecla %d", k)
	}

	tl := gp.NewTimeline(s)
	var order []int
	for _, pb := range tl.Bars {
		order = append(order, pb.Master+1)
	}
	if !slices.Equal(order, []int{1, 2, 3, 2, 3, 4, 5}) {
		t.Fatalf("orden de reproducción %v", order)
	}
	// 27 quarter notes at 100 bpm.
	if d := tl.Duration(); d < 16.199 || d > 16.201 {
		t.Fatalf("duración %.3fs", d)
	}
	for _, sec := range []float64{0, 3.3, 9.9, 16} {
		if got := tl.TickToSec(int64(tl.SecToTick(sec))); got < sec-0.001 || got > sec+0.001 {
			t.Fatalf("ida y vuelta %.3f -> %.3f", sec, got)
		}
	}

	evs := gp.Events(s, tl)
	ons := map[int]int{}
	for _, e := range evs {
		if e.On {
			ons[e.Track]++
		}
	}
	// Guitar: 5 bars x 4 + 3 + one tied note; drums: 6 x 4 + 3; one click per beat.
	if ons[0] != 24 || ons[1] != 27 || ons[gp.MetronomeTrack] != 27 {
		t.Fatalf("notas por pista %v", ons)
	}
	// The tied half notes in the last bar sound as one whole note (2.4 s).
	var last gp.Event
	for _, e := range evs {
		if e.Track == 0 && e.On {
			last = e
		}
	}
	for _, e := range evs {
		if e.Track == 0 && !e.On && e.Key == last.Key && e.Time > last.Time {
			if d := e.Time - last.Time; d < 2.39 || d > 2.41 {
				t.Fatalf("nota ligada dura %.3fs", d)
			}
			return
		}
	}
	t.Fatal("no se encontró el final de la nota ligada")
}

// TestLoadSample parses a real file when GP_SAMPLE points to one.
func TestLoadSample(t *testing.T) {
	path := os.Getenv("GP_SAMPLE")
	if path == "" {
		t.Skip("GP_SAMPLE no definido")
	}
	s, err := gp.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tl := gp.NewTimeline(s)
	t.Logf("%d pistas, %d compases, %d reproducidos, %.1fs", len(s.Tracks), len(s.MasterBars), len(tl.Bars), tl.Duration())
}
