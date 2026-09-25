package engine

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ring is a single-producer single-consumer sample queue. The audio thread
// writes, the recorder goroutine reads.
type ring struct {
	buf     []float32
	mask    uint64
	w, r    atomic.Uint64
	dropped atomic.Bool
}

func newRing(sizePow2 int) *ring {
	return &ring{buf: make([]float32, sizePow2), mask: uint64(sizePow2 - 1)}
}

func (q *ring) write(p []float32) {
	w, r := q.w.Load(), q.r.Load()
	if free := uint64(len(q.buf)) - (w - r); uint64(len(p)) > free {
		q.dropped.Store(true)
		p = p[:free]
	}
	for i, v := range p {
		q.buf[(w+uint64(i))&q.mask] = v
	}
	q.w.Store(w + uint64(len(p)))
}

func (q *ring) read(p []float32) int {
	w, r := q.w.Load(), q.r.Load()
	n := min(int(w-r), len(p))
	for i := range n {
		p[i] = q.buf[(r+uint64(i))&q.mask]
	}
	q.r.Store(r + uint64(n))
	return n
}

// Recorder streams the input captured during playback to a Broadcast WAV
// file that starts where recording started and carries its song position.
type Recorder struct {
	Path string

	q          *ring
	wav        *wavWriter
	sampleRate int
	skip       int64 // latency samples still to discard
	// start is the song position (samples) of the first captured sample. The
	// audio thread sets it right before its first write; -1 until then.
	start  atomic.Int64
	padded bool // capture has started
	take   []float32
	stop   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	err    error
}

// newRecorder prepares a take; latency is the round-trip latency to
// compensate. Capture begins when the audio thread sets start.
func newRecorder(path string, sampleRate int, latency int) (*Recorder, error) {
	wav, err := createWav(path, sampleRate)
	if err != nil {
		return nil, err
	}
	rec := &Recorder{
		Path:       path,
		q:          newRing(1 << 21),
		wav:        wav,
		sampleRate: sampleRate,
		skip:       int64(max(latency, 0)),
		stop:       make(chan struct{}),
	}
	rec.start.Store(-1)
	rec.wg.Add(1)
	go rec.loop()
	return rec, nil
}

// Started reports whether the audio thread has begun capturing.
func (rec *Recorder) Started() bool { return rec.start.Load() >= 0 }

// Elapsed returns the seconds captured so far (approximate).
func (rec *Recorder) Elapsed() float64 {
	return float64(rec.q.w.Load()) / float64(rec.sampleRate)
}

func (rec *Recorder) loop() {
	defer rec.wg.Done()
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-rec.stop:
			rec.drain()
			return
		case <-t.C:
			rec.drain()
		}
	}
}

func (rec *Recorder) drain() {
	if !rec.padded {
		start := rec.start.Load()
		if start < 0 {
			return
		}
		// The file starts where recording started; the song position goes in
		// the BWF header so a DAW can place it.
		rec.take = make([]float32, 0, int64(rec.sampleRate)*60)
		rec.wav.timeRef = uint64(start)
		rec.padded = true
	}
	var buf [8192]float32
	for {
		n := rec.q.read(buf[:])
		if n == 0 {
			return
		}
		s := buf[:n]
		if rec.skip > 0 {
			k := min(rec.skip, int64(len(s)))
			rec.skip -= k
			s = s[k:]
		}
		if len(s) == 0 {
			continue
		}
		rec.take = append(rec.take, s...)
		if err := rec.wav.write(s); err != nil {
			rec.mu.Lock()
			rec.err = err
			rec.mu.Unlock()
		}
	}
}

// finish stops the recorder, closes the file and returns the captured
// samples (nil if nothing was captured).
func (rec *Recorder) finish() ([]float32, error) {
	close(rec.stop)
	rec.wg.Wait()
	err := rec.wav.close()
	if !rec.padded {
		// Nothing was captured: don't leave an empty file behind.
		os.Remove(rec.Path)
		return nil, err
	}
	rec.mu.Lock()
	if rec.err != nil {
		err = rec.err
	}
	rec.mu.Unlock()
	return rec.take, err
}
