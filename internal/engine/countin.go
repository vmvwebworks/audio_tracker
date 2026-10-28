package engine

import (
	"errors"
	"math"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

// PlayCountIn starts playback like Play, preceded by bars of metronome
// clicks in the metre and tempo of the score at the current position. The
// song position does not move, and nothing is recorded, until they end.
func (e *Engine) PlayCountIn(bars int) error {
	if e.song == nil {
		return errors.New("no hay canción cargada")
	}
	if !e.running && e.rt.synth == nil {
		return errors.New("el audio no está en marcha: revisa el driver ASIO")
	}
	clicks, length, beats := e.countInClicks(bars, e.pos.Load())
	e.ended.Store(false)
	e.countBeat.Store(int32(beats))
	e.counting.Store(length > 0)
	e.exec(func(r *rt) {
		if r.pos >= r.end {
			r.pos, r.idx = 0, 0
		}
		r.count, r.countIdx, r.countPos, r.countLen = clicks, 0, 0, length
		r.playing = true
	})
	e.playing.Store(true)
	return nil
}

// CountIn reports whether the count-in is running and how many beats are
// left, the one sounding included.
func (e *Engine) CountIn() (on bool, beatsLeft int) {
	return e.counting.Load(), int(e.countBeat.Load())
}

// countInClicks builds the clicks of a count-in of bars before session
// sample pos. Event times are relative to the start of the count-in. It also
// returns its length in samples and its number of beats.
func (e *Engine) countInClicks(bars int, pos int64) ([]event, int64, int) {
	if bars <= 0 || e.sampleRate == 0 || e.timeline == nil || len(e.timeline.Bars) == 0 {
		return nil, 0, 0
	}
	tl := e.timeline
	score := (float64(pos)/e.sampleRate - e.syncOffset) / e.syncScale
	t := tl.SecToTick(max(score, 0))
	mb := e.song.MasterBars[tl.Bars[tl.BarAt(t)].Master]
	step := gp.TicksPerQuarter * 4 / mb.Den
	if mb.Den == 8 && mb.Num%3 == 0 {
		step *= 3 // compound metre: click dotted quarters, like the metronome
	}
	perBar := max(mb.Duration/step, 1)
	// Seconds per tick at this tempo, in session time.
	tick := 60 / tl.TempoAt(int64(t)) / gp.TicksPerQuarter * e.syncScale
	at := func(ticks int) int64 { return int64(math.Round(float64(ticks) * tick * e.sampleRate)) }

	beats := bars * perBar
	clicks := make([]event, 0, 2*beats)
	for i := range beats {
		key, vel := uint8(77), uint8(90)
		if i%perBar == 0 {
			key, vel = 76, 120
		}
		start := at(i * step)
		clicks = append(clicks,
			event{sample: start, track: gp.MetronomeTrack, ch: drumChannel, on: true, key: key, vel: vel},
			event{sample: start + at(gp.TicksPerQuarter/8), track: gp.MetronomeTrack, ch: drumChannel, key: key})
	}
	return clicks, at(beats * step), beats
}

// renderCountIn plays the count-in into left and right, at most until it
// ends, and returns how many samples it used. Audio thread.
func (e *Engine) renderCountIn(r *rt, left, right []float32) int {
	n := int(min(int64(len(left)), r.countLen-r.countPos))
	cur := 0
	for r.countIdx < len(r.count) && r.count[r.countIdx].sample < r.countPos+int64(n) {
		ev := &r.count[r.countIdx]
		if off := int(ev.sample - r.countPos); off > cur {
			r.synth.Render(left[cur:off], right[cur:off])
			cur = off
		}
		if ev.on {
			r.synth.NoteOn(int32(ev.ch), int32(ev.key), int32(ev.vel))
			e.countBeat.Store(int32((len(r.count) - r.countIdx) / 2))
		} else {
			r.synth.NoteOff(int32(ev.ch), int32(ev.key))
		}
		r.countIdx++
	}
	if cur < n {
		r.synth.Render(left[cur:n], right[cur:n])
	}
	r.countPos += int64(n)
	if r.countPos >= r.countLen {
		r.stopCountIn()
		e.counting.Store(false)
	}
	return n
}

func (r *rt) stopCountIn() {
	r.count, r.countIdx, r.countPos, r.countLen = nil, 0, 0, 0
}
