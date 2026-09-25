// Package engine plays a score through a SoundFont synthesizer on an ASIO
// device while optionally recording one input channel.
package engine

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/sinshu/go-meltysynth/meltysynth"

	"github.com/vmvwebworks/audio_tracker/internal/asio"
	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

const drumChannel = 9

// Engine owns the audio device and the playback state. Its exported methods
// must be called from one goroutine (the UI goroutine).
type Engine struct {
	sf *meltysynth.SoundFont

	drv        *asio.Driver
	running    bool
	sampleRate float64
	bufSize    int
	latIn      int
	latOut     int
	inChan     int
	outL, outR int
	// LatencyAdjust is extra compensation in samples added to the driver's
	// reported round-trip latency when aligning recordings.
	LatencyAdjust int

	song     *gp.Song
	timeline *gp.Timeline
	channels []int      // MIDI channel per track
	src      []gp.Event // score events in score seconds

	// Session timeline: when a backing track is loaded the playback clock is
	// the audio file's. Score time t plays at session time offset + t*scale.
	syncOffset float64
	syncScale  float64
	backingLen int64 // samples

	recorder *Recorder

	cmds chan func(*rt)
	rt   rt

	// State published by the audio thread.
	pos      atomic.Int64 // song position in samples
	posStamp atomic.Int64 // time.Now().UnixNano() when pos was published
	playing  atomic.Bool
	ended    atomic.Bool
	inPeak   atomic.Uint32
	feedback atomic.Bool  // monitor was cut by the feedback guard
	prevPos  atomic.Int64 // preview position in samples, -1 when idle
	outPeak  atomic.Uint32
}

// rt is state owned by the audio thread while the device runs (and by the
// UI goroutine while it does not).
type rt struct {
	synth       *meltysynth.Synthesizer
	left, right []float32

	events       []event
	idx          int
	pos          int64
	end          int64
	playing      bool
	audible      []bool
	metro        bool
	monitor      bool
	inGain       float32
	hot          int // consecutive saturated input samples while monitoring
	hotMax       int // feedback guard threshold (samples)
	takeOn       bool
	take         []float32
	takeAt       int64     // song position (samples) of take[0]
	backL, backR []float32 // backing track (e.g. a mix exported from Reaper)
	backGain     float32
	backOn       bool
	preview      []float32 // a take being auditioned on its own
	prevPos      int
	rec          *Recorder
	master       float32
	inChan       int
	outL         int
	outR         int
}

type event struct {
	sample int64
	track  int16
	ch     uint8
	on     bool
	key    uint8
	vel    uint8
}

// New loads the SoundFont used for playback.
func New(soundFontPath string) (*Engine, error) {
	f, err := os.Open(soundFontPath)
	if err != nil {
		return nil, fmt.Errorf("no se encontró la SoundFont: %w", err)
	}
	defer f.Close()
	sf, err := meltysynth.NewSoundFont(f)
	if err != nil {
		return nil, fmt.Errorf("SoundFont inválida: %w", err)
	}
	e := &Engine{sf: sf, cmds: make(chan func(*rt), 64), outL: 0, outR: 1, syncScale: 1}
	e.rt.backGain, e.rt.backOn = 1, true
	e.rt.master = 1
	e.rt.inGain = 1
	e.prevPos.Store(-1)
	e.rt.outR = 1
	return e, nil
}

// Drivers lists installed ASIO drivers.
func (e *Engine) Drivers() []asio.DriverInfo {
	d, _ := asio.ListDrivers()
	return d
}

// OpenDriver loads the named ASIO driver and starts streaming.
func (e *Engine) OpenDriver(name string) error {
	e.CloseDriver()
	var info *asio.DriverInfo
	drivers := e.Drivers()
	for i := range drivers {
		if drivers[i].Name == name {
			info = &drivers[i]
		}
	}
	if info == nil {
		if len(drivers) == 0 {
			return errors.New("no hay drivers ASIO instalados (instala ASIO4ALL)")
		}
		info = &drivers[0]
	}
	drv, err := asio.Open(*info)
	if err != nil {
		return err
	}
	e.drv = drv
	return e.startStream()
}

// CloseDriver stops streaming and unloads the driver.
func (e *Engine) CloseDriver() {
	if e.drv == nil {
		return
	}
	e.stopStream()
	e.drv.Close()
	e.drv = nil
}

func (e *Engine) startStream() error {
	d := e.drv
	sr := d.SampleRate()
	if sr <= 0 {
		if err := d.SetSampleRate(48000); err != nil {
			return err
		}
		sr = 48000
	}
	nIn, nOut := d.Channels()
	if nOut < 1 {
		return errors.New("el dispositivo no tiene salidas")
	}
	e.inChan = min(max(e.inChan, 0), max(nIn-1, 0))
	e.outL = min(max(e.outL, 0), nOut-1)
	e.outR = min(max(e.outR, 0), nOut-1)
	var ins []int
	if nIn > 0 {
		ins = []int{e.inChan}
	}
	outs := []int{e.outL}
	if e.outR != e.outL {
		outs = append(outs, e.outR)
	}

	if sr != e.sampleRate || e.rt.synth == nil {
		settings := meltysynth.NewSynthesizerSettings(int32(sr))
		synth, err := meltysynth.NewSynthesizer(e.sf, settings)
		if err != nil {
			return err
		}
		e.sampleRate = sr
		e.rt.synth = synth
		e.setupChannels()
		e.rebuildEvents()
	}
	_, _, pref, _ := d.BufferSizes()
	e.rt.hotMax = int(sr * 0.3) // 300 ms of saturated input trips the guard
	e.rt.left = make([]float32, pref)
	e.rt.right = make([]float32, pref)
	e.rt.inChan = 0
	if nIn == 0 {
		e.rt.inChan = -1
	}
	e.rt.outL, e.rt.outR = 0, 0
	if len(outs) > 1 {
		e.rt.outR = 1
	}
	if err := d.Start(ins, outs, e.process); err != nil {
		return err
	}
	e.bufSize = d.BufferSize()
	e.latIn, e.latOut = d.Latencies()
	e.running = true
	return nil
}

func (e *Engine) stopStream() {
	if !e.running {
		return
	}
	e.drv.Stop()
	e.running = false
	// Drain commands that were queued for the audio thread.
	for {
		select {
		case f := <-e.cmds:
			f(&e.rt)
		default:
			return
		}
	}
}

// Restart re-creates the stream, e.g. after a driver reset request or a
// channel change.
func (e *Engine) Restart() error {
	if e.drv == nil {
		return errors.New("no hay driver abierto")
	}
	e.finishRecording()
	e.stopStream()
	return e.startStream()
}

// CheckReset restarts the stream if the driver asked for it. It returns
// true if a restart happened.
func (e *Engine) CheckReset() (bool, error) {
	if e.drv == nil || !e.drv.ResetRequested() {
		return false, nil
	}
	return true, e.Restart()
}

// ControlPanel opens the driver's control panel. It blocks until closed, so
// call it from its own goroutine.
func (e *Engine) ControlPanel() {
	if d := e.drv; d != nil {
		d.ControlPanel()
	}
}

// DriverName returns the loaded driver, or "".
func (e *Engine) DriverName() string {
	if e.drv == nil {
		return ""
	}
	return e.drv.Info().Name
}

// Running reports whether the device is streaming.
func (e *Engine) Running() bool { return e.running }

// ChannelNames lists the device's input or output channel names.
func (e *Engine) ChannelNames(input bool) []string {
	if e.drv == nil {
		return nil
	}
	return e.drv.ChannelNames(input)
}

// Input returns the recorded input channel.
func (e *Engine) Input() int { return e.inChan }

// Outputs returns the stereo output channel pair.
func (e *Engine) Outputs() (l, r int) { return e.outL, e.outR }

// SetInput selects the input channel to monitor and record.
func (e *Engine) SetInput(ch int) error {
	e.inChan = ch
	if e.drv == nil {
		return nil
	}
	return e.Restart()
}

// SetOutputs selects the output channel pair.
func (e *Engine) SetOutputs(l, r int) error {
	e.outL, e.outR = l, r
	if e.drv == nil {
		return nil
	}
	return e.Restart()
}

// Status describes the running stream.
func (e *Engine) Status() string {
	if !e.running {
		return "Audio detenido"
	}
	ms := func(n int) float64 { return float64(n) / e.sampleRate * 1000 }
	return fmt.Sprintf("%.0f Hz · búfer %d (%.1f ms) · latencia E/S %.1f / %.1f ms",
		e.sampleRate, e.bufSize, ms(e.bufSize), ms(e.latIn), ms(e.latOut))
}

// SampleRate returns the stream's sample rate.
func (e *Engine) SampleRate() float64 { return e.sampleRate }

// exec runs f on the audio thread (or directly when the stream is stopped)
// and waits until it has been applied.
func (e *Engine) exec(f func(*rt)) {
	if !e.running {
		f(&e.rt)
		return
	}
	done := make(chan struct{})
	timeout := time.After(time.Second)
	select {
	case e.cmds <- func(r *rt) { f(r); close(done) }:
	case <-timeout:
		return
	}
	select {
	case <-done:
	case <-timeout:
		// The driver stopped calling us; the command stays queued and is
		// applied by stopStream.
	}
}

// LoadSong replaces the current score.
func (e *Engine) LoadSong(s *gp.Song, tl *gp.Timeline) {
	e.finishRecording()
	e.song, e.timeline = s, tl
	e.src = gp.Events(s, tl)
	e.channels = make([]int, len(s.Tracks))
	next := 0
	for i, t := range s.Tracks {
		if t.Percussion {
			e.channels[i] = drumChannel
			continue
		}
		if next == drumChannel {
			next++
		}
		e.channels[i] = next % 16
		next++
		if e.channels[i] == drumChannel {
			e.channels[i] = (drumChannel + 1) % 16
		}
	}
	e.exec(func(r *rt) {
		r.playing = false
		r.pos = 0
		r.idx = 0
		r.take = nil
		r.audible = make([]bool, len(s.Tracks))
		for i := range r.audible {
			r.audible[i] = true
		}
		if r.synth != nil {
			r.synth.NoteOffAll(true)
		}
	})
	e.playing.Store(false)
	e.pos.Store(0)
	e.setupChannels()
	e.rebuildEvents()
}

// setupChannels sends program, volume and pan for every track.
func (e *Engine) setupChannels() {
	if e.song == nil || e.rt.synth == nil {
		return
	}
	s := e.song
	chans := e.channels
	e.exec(func(r *rt) {
		r.synth.Reset()
		for i, t := range s.Tracks {
			ch := int32(chans[i])
			if !t.Percussion {
				r.synth.ProcessMidiMessage(ch, 0xC0, int32(t.Program), 0)
			}
			r.synth.ProcessMidiMessage(ch, 0xB0, 7, int32(t.Volume*127))
			r.synth.ProcessMidiMessage(ch, 0xB0, 10, int32(t.Pan*127))
		}
	})
}

// rebuildEvents converts the score to sample-timed events at the current
// sample rate.
func (e *Engine) rebuildEvents() {
	if e.song == nil || e.sampleRate == 0 {
		return
	}
	src := e.src
	off, scale := e.syncOffset, e.syncScale
	evs := make([]event, len(src))
	for i, ev := range src {
		ch := uint8(drumChannel)
		if ev.Track >= 0 {
			ch = uint8(e.channels[ev.Track])
		}
		evs[i] = event{
			sample: int64(math.Round((off + ev.Time*scale) * e.sampleRate)),
			track:  int16(ev.Track), ch: ch, on: ev.On, key: ev.Key, vel: ev.Vel,
		}
	}
	end := int64(math.Ceil((off + e.timeline.Duration()*scale) * e.sampleRate))
	end = max(end+int64(e.sampleRate*2), e.backingLen) // let the last notes ring
	e.exec(func(r *rt) {
		if r.playing && r.synth != nil {
			// Event times moved: release notes whose note-off may now be behind us.
			r.synth.NoteOffAll(false)
		}
		r.events = evs
		r.end = end
		r.idx = sort.Search(len(evs), func(i int) bool { return evs[i].sample >= r.pos })
	})
}

// Play starts playback from the current position. If a recording was
// started with StartRecording it captures while playing.
func (e *Engine) Play() error {
	if e.song == nil {
		return errors.New("no hay canción cargada")
	}
	if !e.running && e.rt.synth == nil {
		return errors.New("el audio no está en marcha: revisa el driver ASIO")
	}
	e.ended.Store(false)
	e.exec(func(r *rt) {
		if r.pos >= r.end {
			r.pos, r.idx = 0, 0
		}
		r.playing = true
	})
	e.playing.Store(true)
	return nil
}

// StartRecording begins a take written to path. If the song is playing,
// capture starts immediately (punch-in); otherwise it starts with Play.
func (e *Engine) StartRecording(path string) error {
	if e.recorder != nil {
		return nil
	}
	if e.sampleRate == 0 {
		return errors.New("el audio no está en marcha: revisa el driver ASIO")
	}
	rec, err := newRecorder(path, int(e.sampleRate), e.latIn+e.latOut+e.LatencyAdjust)
	if err != nil {
		return fmt.Errorf("no se pudo crear la toma: %w", err)
	}
	e.recorder = rec
	e.exec(func(r *rt) { r.rec = rec })
	return nil
}

// StopRecording ends the current take, leaving playback running.
func (e *Engine) StopRecording() (*TakeResult, error) { return e.finishRecording() }

// RecordingElapsed returns the seconds captured in the current take.
func (e *Engine) RecordingElapsed() float64 {
	if e.recorder == nil {
		return 0
	}
	return e.recorder.Elapsed()
}

// Pause stops playback at the current position and ends any take.
func (e *Engine) Pause() (*TakeResult, error) {
	e.exec(func(r *rt) {
		r.playing = false
		r.synth.NoteOffAll(false)
	})
	e.playing.Store(false)
	return e.finishRecording()
}

// TakeResult describes a finished recording.
type TakeResult struct {
	Path     string
	Dropped  bool
	Start    float64 // song time where the take begins
	Duration float64 // seconds captured
	Peak     float32 // loudest sample, 0..1
}

// Poll must be called regularly by the UI. It finalises a recording when
// playback reached the end of the song.
func (e *Engine) Poll() (*TakeResult, error) {
	if e.ended.Swap(false) {
		return e.finishRecording()
	}
	return nil, nil
}

func (e *Engine) finishRecording() (*TakeResult, error) {
	rec := e.recorder
	if rec == nil {
		return nil, nil
	}
	e.exec(func(r *rt) { r.rec = nil })
	e.recorder = nil
	take, err := rec.finish()
	if take == nil {
		return nil, err
	}
	start := rec.start.Load()
	e.exec(func(r *rt) { r.take, r.takeAt = take, start })
	res := &TakeResult{Path: rec.Path, Dropped: rec.q.dropped.Load()}
	if e.sampleRate > 0 {
		res.Start = float64(start) / e.sampleRate
		res.Duration = float64(len(take)) / e.sampleRate
	}
	for _, v := range take {
		res.Peak = max(res.Peak, v, -v)
	}
	return res, err
}

// Recording reports whether a take is being recorded.
func (e *Engine) Recording() bool { return e.recorder != nil }

// Seek moves the playback position to t seconds.
func (e *Engine) Seek(t float64) {
	if e.sampleRate == 0 {
		return
	}
	p := int64(max(t, 0) * e.sampleRate)
	e.exec(func(r *rt) {
		r.pos = p
		r.idx = sort.Search(len(r.events), func(i int) bool { return r.events[i].sample >= p })
		if r.synth != nil {
			r.synth.NoteOffAll(false)
		}
	})
	e.pos.Store(p)
	e.posStamp.Store(time.Now().UnixNano())
}

// Position returns the song position in seconds as currently heard,
// interpolated between audio callbacks and delayed by the output latency.
func (e *Engine) Position() float64 {
	if e.sampleRate == 0 {
		return 0
	}
	p := float64(e.pos.Load())
	if e.playing.Load() {
		elapsed := float64(time.Now().UnixNano()-e.posStamp.Load()) / 1e9 * e.sampleRate
		p += min(max(elapsed, 0), float64(e.bufSize)) - float64(e.latOut)
	}
	return max(p, 0) / e.sampleRate
}

// Playing reports whether the song is playing.
func (e *Engine) Playing() bool { return e.playing.Load() }

// SetAudible sets which tracks sound (mute/solo already resolved) and their
// volumes (0..1).
func (e *Engine) SetAudible(audible []bool, volumes []float64) {
	if e.song == nil {
		return
	}
	a := append([]bool(nil), audible...)
	chans := e.channels
	vols := append([]float64(nil), volumes...)
	e.exec(func(r *rt) {
		for i := range a {
			if i < len(r.audible) && r.audible[i] && !a[i] && r.synth != nil {
				r.synth.NoteOffAllChannel(int32(chans[i]), false)
			}
		}
		r.audible = a
		if r.synth != nil {
			for i, v := range vols {
				r.synth.ProcessMidiMessage(int32(chans[i]), 0xB0, 7, int32(min(max(v, 0), 1)*127))
			}
		}
	})
}

// SetMetronome toggles the click.
func (e *Engine) SetMetronome(on bool) { e.exec(func(r *rt) { r.metro = on }) }

// SetMonitor toggles software monitoring of the input.
func (e *Engine) SetMonitor(on bool) { e.exec(func(r *rt) { r.monitor = on }) }

// SetInputGain sets the gain in dB applied to the input before monitoring and
// recording.
func (e *Engine) SetInputGain(db float64) {
	g := float32(math.Pow(10, db/20))
	e.exec(func(r *rt) { r.inGain = g })
}

// SetTakePlayback toggles playing back the last take along with the song.
func (e *Engine) SetTakePlayback(on bool) { e.exec(func(r *rt) { r.takeOn = on }) }

// HasTake reports whether a take is available for playback.
func (e *Engine) HasTake() bool {
	var ok bool
	e.exec(func(r *rt) { ok = len(r.take) > 0 })
	return ok
}

// InputPeak returns and resets the input peak level (0..1).
func (e *Engine) InputPeak() float32 { return math.Float32frombits(e.inPeak.Swap(0)) }

// OutputPeak returns and resets the output peak level (0..1).
func (e *Engine) OutputPeak() float32 { return math.Float32frombits(e.outPeak.Swap(0)) }

// process runs on the ASIO thread.
func (e *Engine) process(in, out [][]float32) {
	r := &e.rt
drain:
	for range 16 {
		select {
		case f := <-e.cmds:
			f(r)
		default:
			break drain
		}
	}
	n := len(out[0])
	if len(r.left) < n {
		return // buffer size changed under us; wait for restart
	}
	left, right := r.left[:n], r.right[:n]

	var input []float32
	if r.inChan >= 0 && r.inChan < len(in) {
		input = in[r.inChan]
		var pk float32
		for i, v := range input {
			v *= r.inGain
			input[i] = v
			pk = max(pk, float32(math.Abs(float64(v))))
		}
		storeMax(&e.inPeak, pk)
		// Feedback guard: input pinned near full scale while monitoring means the
		// output is leaking back into the input. Cut the monitor.
		if r.monitor && pk > 0.9 {
			r.hot += len(input)
			if r.hotMax > 0 && r.hot >= r.hotMax {
				r.monitor = false
				r.hot = 0
				e.feedback.Store(true)
			}
		} else {
			r.hot = 0
		}
	}

	startPos := r.pos
	if r.synth == nil {
		clear(left)
		clear(right)
	} else if r.playing {
		cur := 0
		for r.idx < len(r.events) && r.events[r.idx].sample < r.pos+int64(n) {
			ev := &r.events[r.idx]
			if off := int(ev.sample - r.pos); off > cur {
				r.synth.Render(left[cur:off], right[cur:off])
				cur = off
			}
			r.dispatch(ev)
			r.idx++
		}
		if cur < n {
			r.synth.Render(left[cur:], right[cur:])
		}
		if r.rec != nil && input != nil {
			if r.rec.start.Load() < 0 {
				r.rec.start.Store(r.pos)
			}
			r.rec.q.write(input)
		}
		r.pos += int64(n)
		if r.pos >= r.end {
			r.playing = false
			r.rec = nil
			r.pos = 0
			r.idx = 0
			r.synth.NoteOffAll(false)
			e.playing.Store(false)
			e.ended.Store(true)
		}
	} else {
		r.synth.Render(left, right)
	}

	var pk float32
	for i := range n {
		l, rr := left[i]*r.master, right[i]*r.master
		if r.monitor && input != nil {
			l += input[i]
			rr += input[i]
		}
		if r.takeOn && r.playing {
			if p := startPos + int64(i) - r.takeAt; p >= 0 && p < int64(len(r.take)) {
				l += r.take[p]
				rr += r.take[p]
			}
		}
		if r.backOn && r.playing {
			if p := startPos + int64(i); p < int64(len(r.backL)) {
				l += r.backL[p] * r.backGain
				rr += r.backR[p] * r.backGain
			}
		}
		if r.prevPos < len(r.preview) {
			v := r.preview[r.prevPos]
			r.prevPos++
			l += v
			rr += v
		}
		left[i], right[i] = l, rr
		pk = max(pk, float32(math.Abs(float64(l))), float32(math.Abs(float64(rr))))
	}
	copy(out[r.outL], left)
	if r.outR != r.outL {
		copy(out[r.outR], right)
	}
	if r.preview != nil {
		if r.prevPos >= len(r.preview) {
			r.preview = nil
			e.prevPos.Store(-1)
		} else {
			e.prevPos.Store(int64(r.prevPos))
		}
	}
	storeMax(&e.outPeak, pk)
	e.pos.Store(r.pos)
	e.posStamp.Store(time.Now().UnixNano())
}

func (r *rt) dispatch(ev *event) {
	if !ev.on {
		r.synth.NoteOff(int32(ev.ch), int32(ev.key))
		return
	}
	if ev.track < 0 {
		if !r.metro {
			return
		}
	} else if int(ev.track) < len(r.audible) && !r.audible[ev.track] {
		return
	}
	r.synth.NoteOn(int32(ev.ch), int32(ev.key), int32(ev.vel))
}

func storeMax(a *atomic.Uint32, v float32) {
	for {
		old := a.Load()
		if v <= math.Float32frombits(old) || a.CompareAndSwap(old, math.Float32bits(v)) {
			return
		}
	}
}

// PlayPreview plays samples (at the engine's sample rate) on their own,
// e.g. to listen back to a take. It replaces any preview in progress.
func (e *Engine) PlayPreview(samples []float32) {
	e.prevPos.Store(0)
	e.exec(func(r *rt) { r.preview, r.prevPos = samples, 0 })
}

// StopPreview stops the preview.
func (e *Engine) StopPreview() {
	e.exec(func(r *rt) { r.preview = nil })
	e.prevPos.Store(-1)
}

// PreviewPosition returns the preview position in seconds, or -1 if no
// preview is playing.
func (e *Engine) PreviewPosition() float64 {
	p := e.prevPos.Load()
	if p < 0 || e.sampleRate == 0 {
		return -1
	}
	return float64(p) / e.sampleRate
}

// SetTake sets the take heard with "Oír toma", placed at song position at
// (samples at the engine's sample rate).
func (e *Engine) SetTake(samples []float32, at int64) {
	e.exec(func(r *rt) { r.take, r.takeAt = samples, at })
}

// FeedbackTripped reports (and clears) whether the feedback guard cut the
// monitor.
func (e *Engine) FeedbackTripped() bool { return e.feedback.Swap(false) }

// SetSync places the score on the session timeline: score time t plays at
// offset + t*scale seconds. Without a backing track use (0, 1).
func (e *Engine) SetSync(offset, scale float64) {
	if scale <= 0 {
		scale = 1
	}
	if offset == e.syncOffset && scale == e.syncScale {
		return
	}
	e.syncOffset, e.syncScale = offset, scale
	e.rebuildEvents()
}

// SetBacking sets the backing track (stereo, at the engine's sample rate).
// Pass nil to remove it.
func (e *Engine) SetBacking(l, r []float32) {
	e.backingLen = int64(len(l))
	e.exec(func(rt *rt) { rt.backL, rt.backR = l, r })
	e.rebuildEvents()
}

// SetBackingLevel sets the backing track volume (linear) and mute.
func (e *Engine) SetBackingLevel(gain float32, on bool) {
	e.exec(func(r *rt) { r.backGain, r.backOn = gain, on })
}
