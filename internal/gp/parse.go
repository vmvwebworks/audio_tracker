package gp

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Load reads a Guitar Pro 7/8 file (.gp).
func Load(path string) (*Song, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("no es un archivo Guitar Pro 7/8 válido: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		// Some zip tools on Windows store paths with backslashes.
		if strings.ReplaceAll(f.Name, "\\", "/") == "Content/score.gpif" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return Parse(rc)
		}
	}
	return nil, errors.New("el archivo no contiene Content/score.gpif")
}

// XML mirror of the parts of GPIF we use.
type xGPIF struct {
	Score struct {
		Title  string `xml:"Title"`
		Artist string `xml:"Artist"`
		Album  string `xml:"Album"`
	} `xml:"Score"`
	MasterTrack struct {
		Automations []xAutomation `xml:"Automations>Automation"`
	} `xml:"MasterTrack"`
	Tracks     []xTrack     `xml:"Tracks>Track"`
	MasterBars []xMasterBar `xml:"MasterBars>MasterBar"`
	Bars       []xBar       `xml:"Bars>Bar"`
	Voices     []xVoice     `xml:"Voices>Voice"`
	Beats      []xBeat      `xml:"Beats>Beat"`
	Notes      []xNote      `xml:"Notes>Note"`
	Rhythms    []xRhythm    `xml:"Rhythms>Rhythm"`
}

type xAutomation struct {
	Type     string  `xml:"Type"`
	Bar      int     `xml:"Bar"`
	Position float64 `xml:"Position"`
	Value    string  `xml:"Value"`
}

type xTrack struct {
	ID            int    `xml:"id,attr"`
	Name          string `xml:"Name"`
	ShortName     string `xml:"ShortName"`
	InstrumentSet struct {
		Type     string `xml:"Type"`
		Elements []struct {
			Articulations []struct {
				Name             string `xml:"Name"`
				StaffLine        int    `xml:"StaffLine"`
				Noteheads        string `xml:"Noteheads"`
				OutputMidiNumber int    `xml:"OutputMidiNumber"`
			} `xml:"Articulations>Articulation"`
		} `xml:"Elements>Element"`
	} `xml:"InstrumentSet"`
	ChannelStrip string `xml:"RSE>ChannelStrip>Parameters"`
	Sounds       []struct {
		Program int `xml:"MIDI>Program"`
	} `xml:"Sounds>Sound"`
	PrimaryChannel *int `xml:"MidiConnection>PrimaryChannel"`
	GeneralMidi    struct {
		Program        *int `xml:"Program"`
		PrimaryChannel *int `xml:"PrimaryChannel"`
	} `xml:"GeneralMidi"`
	Staves []struct {
		Properties []xProperty `xml:"Properties>Property"`
	} `xml:"Staves>Staff"`
	Properties []xProperty `xml:"Properties>Property"`
}

type xProperty struct {
	Name    string    `xml:"name,attr"`
	Pitches string    `xml:"Pitches"`
	Fret    string    `xml:"Fret"`
	String  string    `xml:"String"`
	Number  string    `xml:"Number"`
	Enable  *struct{} `xml:"Enable"`
}

type xMasterBar struct {
	Time   string `xml:"Time"`
	Bars   string `xml:"Bars"`
	Repeat *struct {
		Start string `xml:"start,attr"`
		End   string `xml:"end,attr"`
		Count int    `xml:"count,attr"`
	} `xml:"Repeat"`
	AlternateEndings string `xml:"AlternateEndings"`
	Section          struct {
		Letter string `xml:"Letter"`
		Text   string `xml:"Text"`
	} `xml:"Section"`
	TripletFeel string `xml:"TripletFeel"`
}

type xBar struct {
	ID         int    `xml:"id,attr"`
	Voices     string `xml:"Voices"`
	SimileMark string `xml:"SimileMark"`
}

type xVoice struct {
	ID    int    `xml:"id,attr"`
	Beats string `xml:"Beats"`
}

type xBeat struct {
	ID         int    `xml:"id,attr"`
	Dynamic    string `xml:"Dynamic"`
	GraceNotes string `xml:"GraceNotes"`
	Rhythm     struct {
		Ref int `xml:"ref,attr"`
	} `xml:"Rhythm"`
	Notes string `xml:"Notes"`
}

type xNote struct {
	ID  int `xml:"id,attr"`
	Tie *struct {
		Origin      string `xml:"origin,attr"`
		Destination string `xml:"destination,attr"`
	} `xml:"Tie"`
	InstrumentArticulation *int        `xml:"InstrumentArticulation"`
	AntiAccent             string      `xml:"AntiAccent"`
	Accent                 int         `xml:"Accent"`
	LetRing                *struct{}   `xml:"LetRing"`
	Properties             []xProperty `xml:"Properties>Property"`
}

type xRhythm struct {
	ID        int    `xml:"id,attr"`
	NoteValue string `xml:"NoteValue"`
	Dot       struct {
		Count int `xml:"count,attr"`
	} `xml:"AugmentationDot"`
	Tuplet struct {
		Num int `xml:"num,attr"`
		Den int `xml:"den,attr"`
	} `xml:"PrimaryTuplet"`
}

// Parse decodes a score.gpif document.
func Parse(r io.Reader) (*Song, error) {
	var x xGPIF
	if err := xml.NewDecoder(r).Decode(&x); err != nil {
		return nil, fmt.Errorf("error leyendo score.gpif: %w", err)
	}
	p := &parser{x: &x}
	return p.build()
}

type parser struct {
	x       *xGPIF
	bars    map[int]*xBar
	voices  map[int]*xVoice
	beats   map[int]*xBeat
	notes   map[int]*xNote
	rhythms map[int]*xRhythm
}

func (p *parser) build() (*Song, error) {
	x := p.x
	p.bars = index(x.Bars, func(b *xBar) int { return b.ID })
	p.voices = index(x.Voices, func(v *xVoice) int { return v.ID })
	p.beats = index(x.Beats, func(b *xBeat) int { return b.ID })
	p.notes = index(x.Notes, func(n *xNote) int { return n.ID })
	p.rhythms = index(x.Rhythms, func(r *xRhythm) int { return r.ID })

	s := &Song{
		Title:  strings.TrimSpace(x.Score.Title),
		Artist: strings.TrimSpace(x.Score.Artist),
		Album:  strings.TrimSpace(x.Score.Album),
	}
	for i := range x.Tracks {
		s.Tracks = append(s.Tracks, p.track(i, &x.Tracks[i]))
	}
	if len(s.Tracks) == 0 {
		return nil, errors.New("la partitura no tiene pistas")
	}

	for i := range x.MasterBars {
		xm := &x.MasterBars[i]
		mb := &MasterBar{Index: i, Num: 4, Den: 4, RepeatCount: 2}
		if n, d, ok := strings.Cut(xm.Time, "/"); ok {
			mb.Num, _ = strconv.Atoi(n)
			mb.Den, _ = strconv.Atoi(d)
		}
		if mb.Num <= 0 || mb.Den <= 0 {
			mb.Num, mb.Den = 4, 4
		}
		mb.Duration = mb.Num * TicksPerQuarter * 4 / mb.Den
		if xm.Repeat != nil {
			mb.RepeatStart = xm.Repeat.Start == "true"
			mb.RepeatEnd = xm.Repeat.End == "true"
			if xm.Repeat.Count > 0 {
				mb.RepeatCount = xm.Repeat.Count
			}
		}
		for _, v := range ints(xm.AlternateEndings) {
			if v >= 1 && v <= 32 {
				mb.Alternate |= 1 << (v - 1)
			}
		}
		mb.Section = strings.TrimSpace(xm.Section.Text)
		if mb.Section == "" {
			mb.Section = strings.TrimSpace(xm.Section.Letter)
		}
		mb.TripletFeel = xm.TripletFeel != "" && xm.TripletFeel != "NoTripletFeel"
		s.MasterBars = append(s.MasterBars, mb)

		// Bar ids are listed per staff, tracks in document order.
		ids := ints(xm.Bars)
		k := 0
		for _, t := range s.Tracks {
			for _, st := range t.Staves {
				var bar *Bar
				if k < len(ids) {
					bar = p.bar(ids[k], t, st)
				}
				if bar == nil {
					bar = &Bar{}
				}
				st.Bars = append(st.Bars, bar)
				k++
			}
		}
	}

	for _, a := range x.MasterTrack.Automations {
		if a.Type != "Tempo" || a.Bar < 0 || a.Bar >= len(s.MasterBars) {
			continue
		}
		f := strings.Fields(a.Value)
		if len(f) == 0 {
			continue
		}
		bpm, err := strconv.ParseFloat(f[0], 64)
		if err != nil || bpm <= 0 {
			continue
		}
		if len(f) > 1 {
			// The second field is the reference note value of the tempo mark.
			switch f[1] {
			case "1":
				bpm *= 0.5
			case "3":
				bpm *= 1.5
			case "4":
				bpm *= 2
			case "5":
				bpm *= 3
			}
		}
		mb := s.MasterBars[a.Bar]
		mb.TempoChanges = append(mb.TempoChanges, TempoChange{Position: clamp01(a.Position), BPM: bpm})
	}
	return s, nil
}

func (p *parser) track(i int, xt *xTrack) *Track {
	t := &Track{
		Index:     i,
		Name:      strings.TrimSpace(xt.Name),
		ShortName: strings.TrimSpace(xt.ShortName),
		Volume:    0.8,
		Pan:       0.5,
	}
	if t.Name == "" {
		t.Name = fmt.Sprintf("Pista %d", i+1)
	}
	if len(xt.Sounds) > 0 {
		t.Program = xt.Sounds[0].Program
	} else if xt.GeneralMidi.Program != nil {
		t.Program = *xt.GeneralMidi.Program
	}
	ch := -1
	if xt.PrimaryChannel != nil {
		ch = *xt.PrimaryChannel
	} else if xt.GeneralMidi.PrimaryChannel != nil {
		ch = *xt.GeneralMidi.PrimaryChannel
	}
	t.Percussion = strings.EqualFold(xt.InstrumentSet.Type, "drumKit") || ch == 9
	if params := strings.Fields(xt.ChannelStrip); len(params) > 12 {
		if v, err := strconv.ParseFloat(params[11], 64); err == nil {
			t.Pan = clamp01(v)
		}
		if v, err := strconv.ParseFloat(params[12], 64); err == nil {
			t.Volume = clamp01(v)
		}
	}
	for _, el := range xt.InstrumentSet.Elements {
		for _, a := range el.Articulations {
			t.DrumMap = append(t.DrumMap, DrumArticulation{
				Name:      a.Name,
				MidiKey:   a.OutputMidiNumber,
				StaffLine: a.StaffLine,
				Cross:     strings.Contains(a.Noteheads, "X") || strings.Contains(a.Noteheads, "Circle"),
			})
		}
	}
	staves := xt.Staves
	if len(staves) == 0 {
		// Older layouts keep the staff properties on the track itself.
		staves = append(staves, struct {
			Properties []xProperty `xml:"Properties>Property"`
		}{xt.Properties})
	}
	for _, xs := range staves {
		st := &Staff{}
		for _, pr := range xs.Properties {
			switch pr.Name {
			case "Tuning":
				st.Tuning = ints(pr.Pitches)
			case "CapoFret":
				st.Capo, _ = strconv.Atoi(strings.TrimSpace(pr.Fret))
			}
		}
		if t.Percussion || allZero(st.Tuning) {
			st.Tuning = nil
		}
		t.Staves = append(t.Staves, st)
	}
	return t
}

func (p *parser) bar(id int, t *Track, st *Staff) *Bar {
	xb := p.bars[id]
	if xb == nil {
		return nil
	}
	if xb.SimileMark == "Simple" && len(st.Bars) > 0 {
		// "Repeat previous bar" sign.
		return st.Bars[len(st.Bars)-1]
	}
	bar := &Bar{}
	for _, vid := range ints(xb.Voices) {
		xv := p.voices[vid]
		if vid < 0 || xv == nil {
			continue
		}
		var beats []*Beat
		pos := 0
		for _, bid := range ints(xv.Beats) {
			xbt := p.beats[bid]
			if xbt == nil {
				continue
			}
			b := p.beat(xbt, t, st)
			if b.Grace {
				// Grace notes steal a little time just before the main beat.
				b.Start = max(pos-b.Duration, 0)
			} else {
				b.Start = pos
				pos += b.Duration
			}
			beats = append(beats, b)
		}
		if len(beats) > 0 {
			bar.Voices = append(bar.Voices, beats)
		}
	}
	return bar
}

var dynamics = map[string]int{
	"PPP": 30, "PP": 45, "P": 60, "MP": 75, "MF": 90, "F": 105, "FF": 115, "FFF": 127,
}

func (p *parser) beat(xb *xBeat, t *Track, st *Staff) *Beat {
	b := &Beat{Velocity: 90, Value: 4}
	if v, ok := dynamics[xb.Dynamic]; ok {
		b.Velocity = v
	}
	if r := p.rhythms[xb.Rhythm.Ref]; r != nil {
		b.Value = noteValue(r.NoteValue)
		b.Dots = r.Dot.Count
		if r.Tuplet.Num > 0 && r.Tuplet.Den > 0 && r.Tuplet.Num != r.Tuplet.Den {
			b.Tuplet = r.Tuplet.Num
		}
		d := TicksPerQuarter * 4 / b.Value
		switch b.Dots {
		case 1:
			d = d * 3 / 2
		case 2:
			d = d * 7 / 4
		}
		if b.Tuplet > 0 {
			d = d * r.Tuplet.Den / r.Tuplet.Num
		}
		b.Duration = d
	} else {
		b.Duration = TicksPerQuarter
	}
	if xb.GraceNotes != "" {
		b.Grace = true
		b.Duration = TicksPerQuarter / 8
	}
	for _, nid := range ints(xb.Notes) {
		xn := p.notes[nid]
		if xn == nil {
			continue
		}
		b.Notes = append(b.Notes, p.note(xn, t, st))
	}
	return b
}

func (p *parser) note(xn *xNote, t *Track, st *Staff) *Note {
	n := &Note{Drum: -1, Key: -1}
	if xn.Tie != nil {
		n.TieOrigin = xn.Tie.Origin == "true"
		n.TieDest = xn.Tie.Destination == "true"
	}
	n.Ghost = xn.AntiAccent != ""
	n.Accent = xn.Accent != 0
	n.LetRing = xn.LetRing != nil
	for _, pr := range xn.Properties {
		switch pr.Name {
		case "String":
			n.String, _ = strconv.Atoi(strings.TrimSpace(pr.String))
		case "Fret":
			n.Fret, _ = strconv.Atoi(strings.TrimSpace(pr.Fret))
		case "Midi":
			if v, err := strconv.Atoi(strings.TrimSpace(pr.Number)); err == nil {
				n.Key = v
			}
		case "Muted":
			n.Dead = pr.Enable != nil
		case "PalmMuted":
			n.PalmMute = pr.Enable != nil
		}
	}
	if t.Percussion {
		if xn.InstrumentArticulation != nil {
			a := *xn.InstrumentArticulation
			if a >= 0 && a < len(t.DrumMap) {
				n.Drum = a
				n.Key = t.DrumMap[a].MidiKey
			}
		}
		return n
	}
	if n.Key < 0 && n.String >= 0 && n.String < len(st.Tuning) {
		n.Key = st.Tuning[n.String] + st.Capo + n.Fret
	}
	return n
}

func noteValue(s string) int {
	switch s {
	case "Whole":
		return 1
	case "Half":
		return 2
	case "Quarter":
		return 4
	case "Eighth":
		return 8
	case "16th":
		return 16
	case "32nd":
		return 32
	case "64th":
		return 64
	case "128th":
		return 128
	}
	return 4
}

func index[T any](items []T, id func(*T) int) map[int]*T {
	m := make(map[int]*T, len(items))
	for i := range items {
		m[id(&items[i])] = &items[i]
	}
	return m
}

func ints(s string) []int {
	f := strings.Fields(s)
	out := make([]int, 0, len(f))
	for _, v := range f {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func allZero(v []int) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }
