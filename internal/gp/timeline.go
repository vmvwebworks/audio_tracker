package gp

import (
	"cmp"
	"slices"
	"sort"
)

// PlayBar is one master bar as it occurs in playback order, after
// expanding repeats and alternate endings.
type PlayBar struct {
	Master int   // index into Song.MasterBars
	Tick   int64 // start in timeline ticks
	Len    int   // length in ticks
	Time   float64
}

// Timeline maps between playback order, ticks and seconds.
type Timeline struct {
	Bars   []PlayBar
	tempo  []tempoPoint
	Length int64 // total ticks
}

type tempoPoint struct {
	tick int64
	sec  float64 // time at tick
	bpm  float64
}

const maxPlayBars = 20000

// NewTimeline expands repeats and builds the tempo map.
func NewTimeline(s *Song) *Timeline {
	tl := &Timeline{}
	mbs := s.MasterBars
	repeatStart, pass := 0, 1
	played := make(map[int]int) // repeat-end bar -> jumps taken
	jumped := false
	var tick int64
	for i := 0; i < len(mbs) && len(tl.Bars) < maxPlayBars; {
		m := mbs[i]
		if m.RepeatStart && !jumped {
			repeatStart, pass = i, 1
		}
		jumped = false
		if m.Alternate != 0 && m.Alternate&(1<<(pass-1)) == 0 {
			i++
			continue
		}
		tl.Bars = append(tl.Bars, PlayBar{Master: i, Tick: tick, Len: m.Duration})
		tick += int64(m.Duration)
		if m.RepeatEnd && played[i] < m.RepeatCount-1 {
			played[i]++
			pass++
			i = repeatStart
			jumped = true
			continue
		}
		if m.RepeatEnd {
			played[i] = 0
		}
		i++
	}
	tl.Length = tick

	// Tempo map.
	type change struct {
		tick int64
		bpm  float64
	}
	var changes []change
	for _, pb := range tl.Bars {
		for _, tc := range mbs[pb.Master].TempoChanges {
			changes = append(changes, change{pb.Tick + int64(tc.Position*float64(pb.Len)), tc.BPM})
		}
	}
	sort.SliceStable(changes, func(a, b int) bool { return changes[a].tick < changes[b].tick })
	tl.tempo = []tempoPoint{{0, 0, 120}}
	for _, c := range changes {
		last := &tl.tempo[len(tl.tempo)-1]
		if c.tick == last.tick {
			last.bpm = c.bpm
			continue
		}
		sec := last.sec + ticksToSec(c.tick-last.tick, last.bpm)
		tl.tempo = append(tl.tempo, tempoPoint{c.tick, sec, c.bpm})
	}
	for i := range tl.Bars {
		tl.Bars[i].Time = tl.TickToSec(tl.Bars[i].Tick)
	}
	return tl
}

func ticksToSec(t int64, bpm float64) float64 {
	return float64(t) / TicksPerQuarter * 60 / bpm
}

// TickToSec converts a timeline tick to seconds.
func (tl *Timeline) TickToSec(t int64) float64 {
	i := sort.Search(len(tl.tempo), func(i int) bool { return tl.tempo[i].tick > t }) - 1
	i = max(i, 0)
	p := tl.tempo[i]
	return p.sec + ticksToSec(t-p.tick, p.bpm)
}

// SecToTick converts seconds to a timeline tick (fractional).
func (tl *Timeline) SecToTick(s float64) float64 {
	i := sort.Search(len(tl.tempo), func(i int) bool { return tl.tempo[i].sec > s }) - 1
	i = max(i, 0)
	p := tl.tempo[i]
	return float64(p.tick) + (s-p.sec)*p.bpm/60*TicksPerQuarter
}

// TempoAt returns the tempo in effect at tick t.
func (tl *Timeline) TempoAt(t int64) float64 {
	i := sort.Search(len(tl.tempo), func(i int) bool { return tl.tempo[i].tick > t }) - 1
	return tl.tempo[max(i, 0)].bpm
}

// Duration is the total playback length in seconds.
func (tl *Timeline) Duration() float64 { return tl.TickToSec(tl.Length) }

// BarAt returns the index into Bars that contains tick t (clamped).
func (tl *Timeline) BarAt(t float64) int {
	i := sort.Search(len(tl.Bars), func(i int) bool { return float64(tl.Bars[i].Tick) > t }) - 1
	return min(max(i, 0), len(tl.Bars)-1)
}

// FirstOccurrence returns the index of the first PlayBar for a master bar,
// or -1.
func (tl *Timeline) FirstOccurrence(master int) int {
	for i, pb := range tl.Bars {
		if pb.Master == master {
			return i
		}
	}
	return -1
}

// MetronomeTrack is the Track value of metronome events.
const MetronomeTrack = -1

// Event is a MIDI note event on the timeline.
type Event struct {
	Time  float64 // seconds
	Track int
	On    bool
	Key   uint8
	Vel   uint8
}

// Events renders the song to a time-sorted list of note events, including
// metronome clicks (Track == MetronomeTrack).
func Events(s *Song, tl *Timeline) []Event {
	type sounding struct {
		track int
		key   int
		vel   int
		start int64
		end   int64
	}
	var notes []sounding
	for ti, t := range s.Tracks {
		for si, st := range t.Staves {
			// Ties continue the last note on the same string (or key for drums).
			type tieKey struct{ staff, slot int }
			open := map[tieKey]int{}
			for _, pb := range tl.Bars {
				if pb.Master >= len(st.Bars) {
					continue
				}
				bar := st.Bars[pb.Master]
				swing := s.MasterBars[pb.Master].TripletFeel
				for _, voice := range bar.Voices {
					for _, b := range voice {
						start := pb.Tick + int64(swingPos(b.Start, swing))
						end := pb.Tick + int64(swingPos(b.Start+b.Duration, swing))
						for _, n := range b.Notes {
							if n.Key < 0 || n.Key > 127 {
								continue
							}
							slot := n.String
							if t.Percussion {
								slot = 1000 + n.Key
							}
							k := tieKey{si, slot}
							if n.TieDest {
								if idx, ok := open[k]; ok {
									notes[idx].end = max(notes[idx].end, end)
									continue
								}
							}
							vel := b.Velocity
							noteEnd := end
							switch {
							case n.Dead:
								vel = vel * 2 / 3
								noteEnd = start + min(end-start, TicksPerQuarter/8)
							case n.PalmMute:
								noteEnd = start + (end-start)*2/3
							}
							if n.Ghost {
								vel = vel * 3 / 5
							}
							if n.Accent {
								vel += 20
							}
							if t.Percussion {
								noteEnd = start + min(end-start, TicksPerQuarter/4)
							}
							notes = append(notes, sounding{ti, n.Key, min(max(vel, 1), 127), start, noteEnd})
							open[k] = len(notes) - 1
						}
					}
				}
			}
		}
	}

	evs := make([]Event, 0, len(notes)*2+len(tl.Bars)*4)
	for _, n := range notes {
		if n.end <= n.start {
			n.end = n.start + 1
		}
		evs = append(evs,
			Event{Time: tl.TickToSec(n.start), Track: n.track, On: true, Key: uint8(n.key), Vel: uint8(n.vel)},
			Event{Time: tl.TickToSec(n.end), Track: n.track, On: false, Key: uint8(n.key)})
	}
	for _, pb := range tl.Bars {
		mb := s.MasterBars[pb.Master]
		step := int64(TicksPerQuarter * 4 / mb.Den)
		if mb.Den == 8 && mb.Num%3 == 0 {
			step *= 3 // compound metre: click dotted quarters
		}
		for t, i := int64(0), 0; t < int64(pb.Len); t, i = t+step, i+1 {
			key, vel := uint8(77), uint8(90)
			if i == 0 {
				key, vel = 76, 120
			}
			at := pb.Tick + t
			evs = append(evs,
				Event{Time: tl.TickToSec(at), Track: MetronomeTrack, On: true, Key: key, Vel: vel},
				Event{Time: tl.TickToSec(at + TicksPerQuarter/8), Track: MetronomeTrack, On: false, Key: key})
		}
	}
	slices.SortStableFunc(evs, func(a, b Event) int {
		if c := cmp.Compare(a.Time, b.Time); c != 0 {
			return c
		}
		// Note-offs first so a re-struck key is not cut by its own release.
		if a.On != b.On {
			if !a.On {
				return -1
			}
			return 1
		}
		return 0
	})
	return evs
}

// swingPos applies a triplet-feel shift to eighth-note offbeats.
func swingPos(pos int, swing bool) int {
	if !swing {
		return pos
	}
	const q = TicksPerQuarter
	if pos%q == q/2 {
		return pos - q/2 + q*2/3
	}
	return pos
}
