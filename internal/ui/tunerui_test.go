package ui

import (
	"slices"
	"testing"
)

func TestTunerInstruments(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // Config.Save must not touch the real settings
	a := &App{cfg: &Config{}}
	a.tn.sel = -1

	// Default: the track's tuning, none without a song.
	if a.instrumentLabel() != "De la pista" || len(a.stringNotes()) != 0 {
		t.Fatal("sin pista no hay cuerdas")
	}
	a.stepInstrument(1)
	if a.cfg.TunerInstrument != "Guitarra" || !slices.Equal(a.stringNotes(), []int{40, 45, 50, 55, 59, 64}) {
		t.Fatalf("guitarra: %q %v", a.cfg.TunerInstrument, a.stringNotes())
	}
	a.stepInstrument(-1)
	a.stepInstrument(-1) // wraps to the last preset
	if a.cfg.TunerInstrument != "Cromático" || len(a.stringNotes()) != 0 {
		t.Fatalf("cromático: %q", a.cfg.TunerInstrument)
	}

	// Edit one string of a preset; it is remembered and can be restored.
	a.cfg.TunerInstrument = "Guitarra"
	a.tn.sel = 0
	a.shiftString(-2) // drop D
	if got := a.stringNotes(); got[0] != 38 || got[1] != 45 || !a.tuningEdited() {
		t.Fatalf("drop D: %v", got)
	}
	a.shiftString(12)
	if a.stringNotes()[0] != 50 {
		t.Fatalf("octava: %v", a.stringNotes())
	}
	a.resetTuning()
	if a.stringNotes()[0] != 40 || a.tuningEdited() {
		t.Fatalf("restaurar: %v", a.stringNotes())
	}
}

func TestZoom(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := &App{cfg: &Config{}, tab: &TabView{}}
	if a.zoom() != 1 {
		t.Fatal("zoom por defecto")
	}
	for range 30 {
		a.stepZoom(1)
	}
	if a.zoom() != maxZoom {
		t.Errorf("máximo: %v", a.zoom())
	}
	for range 40 {
		a.stepZoom(-1)
	}
	if a.zoom() != minZoom || a.tab.Zoom != minZoom {
		t.Errorf("mínimo: %v %v", a.zoom(), a.tab.Zoom)
	}
}
