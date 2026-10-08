package ui

import (
	"testing"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

func TestSpell(t *testing.T) {
	cases := []struct {
		midi, key int
		letter    int // C=0 .. B=6
		oct       int
		acc       int
	}{
		{60, 0, 0, 4, 0},   // C4
		{64, 0, 2, 4, 0},   // E4
		{70, 0, 5, 4, 1},   // A#4 in C major: sharps
		{70, -1, 6, 4, -1}, // Bb4 in F major: flats
		{61, 2, 0, 4, 1},   // C#4
		{61, -3, 1, 4, -1}, // Db4
		{71, 0, 6, 4, 0},   // B4
		{24, 0, 0, 1, 0},   // C1
	}
	for _, c := range cases {
		d, acc := spell(c.midi, c.key)
		if want := c.oct*7 + c.letter; d != want || acc != c.acc {
			t.Errorf("spell(%d, %d) = %d, %d; esperado %d, %d", c.midi, c.key, d, acc, want, c.acc)
		}
	}
}

func TestKeyAccidental(t *testing.T) {
	// D major: F# and C#.
	if keyAccidental(2, 3) != 1 || keyAccidental(2, 0) != 1 || keyAccidental(2, 4) != 0 {
		t.Error("re mayor")
	}
	// Eb major: Bb, Eb, Ab.
	if keyAccidental(-3, 6) != -1 || keyAccidental(-3, 2) != -1 || keyAccidental(-3, 5) != -1 || keyAccidental(-3, 1) != 0 {
		t.Error("mi bemol mayor")
	}
	if keyAccidental(0, 3) != 0 {
		t.Error("do mayor")
	}
}

func note(key int) *gp.Note { return &gp.Note{Key: key, Drum: -1} }

func TestAnalyzeBarAccidentals(t *testing.T) {
	tr := &gp.Track{Staves: []*gp.Staff{{Tuning: []int{40, 45, 50, 55, 59, 64}}}}
	// Written (sounding + 12): F#4 F#4 F4 F4 in D major (F# is in the key).
	n1, n2, n3, n4 := note(54), note(54), note(53), note(53)
	bar := &gp.Bar{Voices: [][]*gp.Beat{{
		{Start: 0, Notes: []*gp.Note{n1}},
		{Start: 960, Notes: []*gp.Note{n2}},
		{Start: 1920, Notes: []*gp.Note{n3}},
		{Start: 2880, Notes: []*gp.Note{n4}},
	}}}
	out := map[*gp.Note]noteInfo{}
	analyzeBar(tr, bar, 2, clefTreble, out)
	if out[n1].showAcc || out[n2].showAcc {
		t.Error("el fa sostenido de la armadura no lleva signo")
	}
	if !out[n3].showAcc || out[n3].acc != 0 {
		t.Errorf("el fa natural necesita becuadro: %+v", out[n3])
	}
	if out[n4].showAcc {
		t.Errorf("el becuadro no se repite en el mismo compás: %+v", out[n4])
	}
	// F#4 written is step 3 above E4 (bottom line): F4 is the first space.
	if out[n1].step != 1 {
		t.Errorf("fa en el pentagrama: paso %d, esperado 1", out[n1].step)
	}
}

func TestAnalyzeBarTie(t *testing.T) {
	tr := &gp.Track{Staves: []*gp.Staff{{Tuning: []int{40, 45, 50, 55, 59, 64}}}}
	a, b := note(58), note(58) // Bb written as A#
	b.TieDest = true
	bar := &gp.Bar{Voices: [][]*gp.Beat{{
		{Start: 0, Notes: []*gp.Note{a}},
		{Start: 960, Notes: []*gp.Note{b}},
	}}}
	out := map[*gp.Note]noteInfo{}
	analyzeBar(tr, bar, 0, clefTreble, out)
	if !out[a].showAcc || out[b].showAcc {
		t.Errorf("la nota ligada no lleva signo: %+v %+v", out[a], out[b])
	}
}

func TestClefAndWritten(t *testing.T) {
	guitar := &gp.Track{Program: 29, Staves: []*gp.Staff{{Tuning: []int{40}}}}
	bass := &gp.Track{Program: 33, Staves: []*gp.Staff{{Tuning: []int{28}}}}
	drums := &gp.Track{Percussion: true, Staves: []*gp.Staff{{}}}
	if clefOf(guitar) != clefTreble || clefOf(bass) != clefBass || clefOf(drums) != clefPerc {
		t.Error("claves")
	}
	if writtenKey(guitar, 40) != 52 {
		t.Error("la guitarra se escribe una octava más alta")
	}
	// Low E of a bass is written on the first ledger line below the bass staff.
	d, _ := spell(writtenKey(bass, 28), 0)
	if step := d - clefBottom[clefBass]; step != -2 {
		t.Errorf("mi grave del bajo: paso %d, esperado -2", step)
	}
	// Low E of a guitar sits three ledger lines under the treble staff.
	d, _ = spell(writtenKey(guitar, 40), 0)
	if step := d - clefBottom[clefTreble]; step != -7 {
		t.Errorf("mi grave de la guitarra: paso %d, esperado -7", step)
	}
}

func TestFlagCountAndGroups(t *testing.T) {
	for value, want := range map[int]int{1: 0, 2: 0, 4: 0, 8: 1, 16: 2, 32: 3, 64: 4, 128: 5} {
		if got := flagCount(value); got != want {
			t.Errorf("flagCount(%d) = %d, esperado %d", value, got, want)
		}
	}
	if beamGroupLen(&gp.MasterBar{Num: 6, Den: 8}) != 1440 || beamGroupLen(&gp.MasterBar{Num: 4, Den: 4}) != 960 ||
		beamGroupLen(&gp.MasterBar{Num: 3, Den: 8}) != 960 {
		t.Error("longitud de grupo")
	}
}
