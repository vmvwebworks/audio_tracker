package ui

import (
	"sort"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

// Staff notation logic: clefs, pitch spelling, accidentals and beaming. The
// drawing lives in glyphs.go and tab.go.
//
// Vertical positions are "steps": 0 is the bottom line of the staff and each
// step is half a staff space, so the five lines are 0, 2, 4, 6 and 8.

type clefKind int

const (
	clefTreble clefKind = iota
	clefBass
	clefPerc
)

// Diatonic number of the bottom line of each clef (C0 = 0, 7 per octave):
// E4 for treble, G2 for bass.
var clefBottom = map[clefKind]int{clefTreble: 4*7 + 2, clefBass: 2*7 + 4}

// Pitch classes spelled with sharps or with flats: letter (C=0 .. B=6) and
// accidental (+1 sharp, -1 flat).
var (
	sharpSpell = [12][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 0}, {3, 0}, {3, 1}, {4, 0}, {4, 1}, {5, 0}, {5, 1}, {6, 0}}
	flatSpell  = [12][2]int{{0, 0}, {1, -1}, {1, 0}, {2, -1}, {2, 0}, {3, 0}, {4, -1}, {4, 0}, {5, -1}, {5, 0}, {6, -1}, {6, 0}}
)

// Letters in the order sharps and flats are added to a key signature.
var (
	sharpOrder = [7]int{3, 0, 4, 1, 5, 2, 6} // F C G D A E B
	flatOrder  = [7]int{6, 2, 5, 1, 4, 0, 3} // B E A D G C F
)

// keySigSteps are the staff steps of the key signature symbols, in order.
var keySigSteps = map[clefKind]map[bool][7]int{
	clefTreble: {true: {8, 5, 9, 6, 3, 7, 4}, false: {4, 7, 3, 6, 2, 5, 1}},
	clefBass:   {true: {6, 3, 7, 4, 1, 5, 2}, false: {2, 5, 1, 4, 0, 3, 6}},
}

// keyAccidental is the accidental a key signature puts on a letter.
func keyAccidental(key, letter int) int {
	switch {
	case key > 0:
		for _, l := range sharpOrder[:key] {
			if l == letter {
				return 1
			}
		}
	case key < 0:
		for _, l := range flatOrder[:-key] {
			if l == letter {
				return -1
			}
		}
	}
	return 0
}

// spell returns the diatonic number of a MIDI key and the accidental it is
// written with, preferring sharps in sharp keys and flats in flat keys.
func spell(midi, key int) (diatonic, acc int) {
	pc := ((midi % 12) + 12) % 12
	oct := (midi - pc) / 12 // the C of this octave is MIDI 12*oct, written octave oct-1
	t := sharpSpell
	if key < 0 {
		t = flatSpell
	}
	return (oct-1)*7 + t[pc][0], t[pc][1]
}

// clefOf picks the clef of a track: drums get the percussion clef, bass
// instruments the bass clef and everything else the treble clef.
func clefOf(t *gp.Track) clefKind {
	switch {
	case t.Percussion:
		return clefPerc
	case t.Program >= 32 && t.Program <= 39:
		return clefBass
	}
	return clefTreble
}

// writtenKey is the MIDI key a note is written at. Guitars and basses sound
// an octave lower than written.
func writtenKey(t *gp.Track, key int) int {
	if len(t.Staves) > 0 && len(t.Staves[0].Tuning) > 0 {
		return key + 12
	}
	return key
}

// noteInfo is where and how a note is written on the staff.
type noteInfo struct {
	step    int  // staff step, 0 = bottom line
	acc     int  // accidental: +1 sharp, -1 flat, 0 natural
	showAcc bool // whether the accidental sign is printed
}

// analyzeBar spells every note of a bar and decides which accidental signs
// are needed, following the usual rule: a sign is repeated only when the
// pitch differs from what the key signature or an earlier sign in the same
// bar already says for that line or space.
func analyzeBar(t *gp.Track, bar *gp.Bar, key int, clef clefKind, out map[*gp.Note]noteInfo) {
	type item struct {
		start int
		voice int
		n     *gp.Note
	}
	var items []item
	for vi, voice := range shownVoices(bar) {
		for _, b := range voice {
			for _, n := range b.Notes {
				items = append(items, item{b.Start, vi, n})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })

	state := map[int]int{} // diatonic number -> accidental in force
	for _, it := range items {
		n := it.n
		if n.Key < 0 {
			continue
		}
		if clef == clefPerc {
			out[n] = noteInfo{step: percStep(t, n)}
			continue
		}
		d, acc := spell(writtenKey(t, n.Key), key)
		cur, seen := state[d]
		if !seen {
			cur = keyAccidental(key, d%7)
		}
		info := noteInfo{step: d - clefBottom[clef], acc: acc}
		// A tied-to note continues the previous one and carries no sign.
		if acc != cur && !n.TieDest {
			info.showAcc = true
		}
		state[d] = acc
		out[n] = info
	}
}

// percStep is the staff step of a drum note.
func percStep(t *gp.Track, n *gp.Note) int {
	line := 4
	if n.Drum >= 0 && n.Drum < len(t.DrumMap) {
		line = t.DrumMap[n.Drum].StaffLine
	}
	return 8 - line
}

// beamGroupLen is the length in ticks of the groups eighth and shorter
// notes are beamed in: dotted quarters in compound time, quarters otherwise.
func beamGroupLen(mb *gp.MasterBar) int {
	if mb.Den == 8 && mb.Num%3 == 0 && mb.Num > 3 {
		return gp.TicksPerQuarter * 3 / 2
	}
	return gp.TicksPerQuarter
}

// flagCount is the number of flags (or beams) of a note value: 0 for quarter
// and longer, 1 for eighth, 2 for sixteenth and so on.
func flagCount(value int) int {
	f := 0
	for v := 8; v <= value && f < 5; v *= 2 {
		f++
	}
	return f
}

// shownVoices are the voices drawn: only the first. The others still play.
func shownVoices(bar *gp.Bar) [][]*gp.Beat {
	if len(bar.Voices) > 1 {
		return bar.Voices[:1]
	}
	return bar.Voices
}
