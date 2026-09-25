package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
	"github.com/vmvwebworks/audio_tracker/internal/testutil"
)

func TestBackingAndSync(t *testing.T) {
	e, err := New(testutil.SoundFont(t))
	if err != nil {
		t.Fatal(err)
	}
	s := testutil.Song(t)
	const sr, buf = 48000, 256
	e.LoadSong(s, gp.NewTimeline(s))
	offline(t, e, sr, buf, 0, 0)
	first := e.src[0].Time

	// Score placed 7.5 s into the audio, 2% slower.
	e.SetSync(7.5, 1.02)
	want := int64(math.Round((7.5 + first*1.02) * sr))
	if got := e.rt.events[0].sample; got != want {
		t.Fatalf("primer evento en %d, quiero %d", got, want)
	}

	// A 20 s backing track longer than nothing else extends the end; only
	// the backing should sound when every track is muted.
	n := 20 * sr
	l, r := make([]float32, n), make([]float32, n)
	for i := range l {
		l[i], r[i] = 0.25, -0.25
	}
	e.SetBacking(l, r)
	e.SetBackingLevel(0.5, true)
	e.SetAudible(make([]bool, len(s.Tracks)), make([]float64, len(s.Tracks)))
	e.SetMetronome(false)
	if e.rt.end < int64(n) {
		t.Fatalf("el final (%d) no cubre el tema (%d)", e.rt.end, n)
	}
	e.Seek(1)
	if err := e.Play(); err != nil {
		t.Fatal(err)
	}
	in := [][]float32{make([]float32, buf)}
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	e.process(in, out)
	if d := out[0][10] - 0.125; d > 1e-3 || d < -1e-3 || out[1][10] > -0.12 {
		t.Fatalf("mezcla del tema: L=%v R=%v", out[0][10], out[1][10])
	}
	e.SetBackingLevel(0.5, false)
	e.process(in, out)
	if out[0][10] > 1e-3 || out[0][10] < -1e-3 {
		t.Fatalf("tema silenciado sigue sonando: %v", out[0][10])
	}
}

func TestLoadBackingWav(t *testing.T) {
	// Stereo 16-bit WAV at 44.1 kHz, loaded at 48 kHz.
	path := filepath.Join(t.TempDir(), "tema.wav")
	const rate, frames = 44100, 44100
	data := make([]byte, 44+frames*4)
	copy(data, "RIFF")
	copy(data[8:], "WAVEfmt ")
	le := func(b []byte, v uint32, n int) {
		for i := range n {
			b[i] = byte(v >> (8 * i))
		}
	}
	le(data[4:], uint32(len(data)-8), 4)
	le(data[16:], 16, 4)
	le(data[20:], 1, 2)
	le(data[22:], 2, 2)
	le(data[24:], rate, 4)
	le(data[28:], rate*4, 4)
	le(data[32:], 4, 2)
	le(data[34:], 16, 2)
	copy(data[36:], "data")
	le(data[40:], frames*4, 4)
	for i := range frames {
		le(data[44+4*i:], 16384, 2)
		le(data[46+4*i:], 0x10000-8192, 2) // -8192 in two's complement
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBacking(path, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(b.Duration-1) > 0.001 || len(b.L) != 48000 {
		t.Fatalf("duración %.4fs, %d muestras", b.Duration, len(b.L))
	}
	if math.Abs(float64(b.L[1000])-0.5) > 1e-3 || math.Abs(float64(b.R[1000])+0.25) > 1e-3 {
		t.Fatalf("canales: L=%v R=%v", b.L[1000], b.R[1000])
	}
}
