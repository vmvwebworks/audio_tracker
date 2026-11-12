package engine

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sinshu/go-meltysynth/meltysynth"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
	"github.com/vmvwebworks/audio_tracker/internal/testutil"
	"github.com/vmvwebworks/audio_tracker/internal/tuner"
)

// offline prepares the engine as if a driver were streaming, without one.
func offline(t *testing.T, e *Engine, sr float64, buf, latIn, latOut int) {
	t.Helper()
	synth, err := meltysynth.NewSynthesizer(e.sf, meltysynth.NewSynthesizerSettings(int32(sr)))
	if err != nil {
		t.Fatal(err)
	}
	e.sampleRate, e.bufSize, e.latIn, e.latOut = sr, buf, latIn, latOut
	e.rt.synth = synth
	e.rt.left = make([]float32, buf)
	e.rt.right = make([]float32, buf)
	e.rt.inChan, e.rt.outL, e.rt.outR = 0, 0, 1
	e.setupChannels()
	e.rebuildEvents()
}

func TestRenderAndRecord(t *testing.T) {
	e, err := New(testutil.SoundFont(t))
	if err != nil {
		t.Fatal(err)
	}
	s := testutil.Song(t)
	tl := gp.NewTimeline(s)
	const sr, buf, latIn, latOut = 48000, 256, 300, 400
	e.LoadSong(s, tl)
	offline(t, e, sr, buf, latIn, latOut)

	// The input sample at stream index k carries the value k%1000/1000, so
	// alignment can be checked exactly.
	in := [][]float32{make([]float32, buf)}
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	k := 0
	var peak float32
	run := func(sec float64) {
		for range int(sec * sr / buf) {
			for i := range in[0] {
				in[0][i] = float32(k%1000) / 1000
				k++
			}
			e.process(in, out)
			for _, v := range out[0] {
				peak = max(peak, v, -v)
			}
		}
	}
	lat := latIn + latOut
	check := func(res *TakeResult, startPos int64, streamIdx int) {
		t.Helper()
		if res == nil {
			t.Fatal("no se generó toma")
		}
		take := e.rt.take
		if len(take) < sr/2 {
			t.Fatalf("toma demasiado corta: %d muestras", len(take))
		}
		if e.rt.takeAt != startPos {
			t.Fatalf("la toma empieza en %d, quiero %d", e.rt.takeAt, startPos)
		}
		// No leading silence: the first sample is the one heard at startPos.
		want := float32((streamIdx+lat)%1000) / 1000
		if take[0] != want {
			t.Fatalf("alineación incorrecta: take[0]=%v quiero %v", take[0], want)
		}
		b, err := os.ReadFile(res.Path)
		if err != nil {
			t.Fatal(err)
		}
		le := binary.LittleEndian
		if string(b[:4]) != "RIFF" || string(b[36:40]) != "bext" ||
			int(le.Uint32(b[wavDataSizeOff:])) != len(take)*3 || len(b) != wavHeaderLength+len(take)*3 {
			t.Fatalf("cabecera WAV incorrecta")
		}
		if ref := int64(le.Uint64(b[bextTimeRefOff:])); ref != startPos {
			t.Fatalf("BWF TimeReference %d, quiero %d", ref, startPos)
		}
		t.Logf("toma desde %.2fs, %.2fs grabados: %s", res.Start, res.Duration, filepath.Base(res.Path))
	}

	// 1) Armed before playing, starting two seconds in.
	e.Seek(2)
	if err := e.StartRecording(filepath.Join(t.TempDir(), "armada.wav")); err != nil {
		t.Fatal(err)
	}
	if err := e.Play(); err != nil {
		t.Fatal(err)
	}
	k0 := k
	run(3)
	if peak < 0.01 {
		t.Fatalf("la salida está en silencio (pico %v)", peak)
	}
	res, err := e.Pause()
	if err != nil {
		t.Fatal(err)
	}
	check(res, 2*sr, k0)

	// The WAV reads back with its song position and content.
	loaded, at, err := LoadTake(res.Path, sr)
	if err != nil {
		t.Fatal(err)
	}
	if at != 2*sr || len(loaded) != len(e.rt.take) {
		t.Fatalf("LoadTake: posición %d, %d muestras; quiero %d, %d", at, len(loaded), 2*sr, len(e.rt.take))
	}
	for i := range 1000 {
		if d := loaded[i] - e.rt.take[i]; d > 1e-6 || d < -1e-6 {
			t.Fatalf("muestra %d leída %v, escrita %v", i, loaded[i], e.rt.take[i])
		}
	}
	if half, _, _ := LoadTake(res.Path, sr/2); len(half) != len(loaded)/2 {
		t.Fatalf("remuestreo: %d muestras, quiero %d", len(half), len(loaded)/2)
	}

	// Previewing plays the samples alone and then stops by itself.
	e.rt.synth.NoteOffAll(true) // cut the song's ringing notes
	e.process([][]float32{make([]float32, buf)}, out)
	e.PlayPreview([]float32{0.5, 0.5, 0.5})
	e.process([][]float32{make([]float32, buf)}, out)
	near := func(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }
	if !near(out[0][0], 0.5) || !near(out[0][3], 0) || e.PreviewPosition() != -1 {
		t.Fatalf("preview: salida %v %v, posición %v", out[0][0], out[0][3], e.PreviewPosition())
	}

	// 2) Punch-in while playing, then stop recording without stopping playback.
	e.Seek(0)
	if err := e.Play(); err != nil {
		t.Fatal(err)
	}
	run(1)
	punch, k1 := e.rt.pos, k
	if err := e.StartRecording(filepath.Join(t.TempDir(), "punch.wav")); err != nil {
		t.Fatal(err)
	}
	run(1)
	res, err = e.StopRecording()
	if err != nil {
		t.Fatal(err)
	}
	if !e.Playing() {
		t.Fatal("parar la grabación no debe parar la reproducción")
	}
	check(res, punch, k1)

	// 3) Armed and disarmed without playing leaves no file.
	e.Pause()
	p := filepath.Join(t.TempDir(), "vacia.wav")
	e.StartRecording(p)
	if res, _ := e.StopRecording(); res != nil {
		t.Fatal("una toma sin audio no debería guardarse")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("quedó un WAV vacío")
	}
}

// A count-in clicks one bar before the song starts moving, and the take
// begins exactly where the song does, even when that falls mid-block.
func TestCountIn(t *testing.T) {
	e, err := New(testutil.SoundFont(t))
	if err != nil {
		t.Fatal(err)
	}
	s := testutil.Song(t)
	e.LoadSong(s, gp.NewTimeline(s))
	const sr, buf = 48000, 1000
	offline(t, e, sr, buf, 0, 0)
	const countLen = 4 * 0.6 * sr // a 4/4 bar at 100 bpm: 115200 samples

	in := [][]float32{make([]float32, buf)}
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	k := 0
	var peak float32
	block := func() {
		for i := range in[0] {
			in[0][i] = float32(k%1000) / 1000
			k++
		}
		e.process(in, out)
		for _, v := range out[0] {
			peak = max(peak, v, -v)
		}
	}

	if err := e.StartRecording(filepath.Join(t.TempDir(), "cuenta.wav")); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCountIn(1); err != nil {
		t.Fatal(err)
	}
	if on, beats := e.CountIn(); !on || beats != 4 {
		t.Fatalf("cuenta: %v, %d tiempos", on, beats)
	}
	for range int(countLen) / buf {
		block()
	}
	if e.rt.pos != 0 || e.Position() != 0 || e.recorder.start.Load() >= 0 {
		t.Fatalf("durante la cuenta la canción avanzó (%d) o se grabó", e.rt.pos)
	}
	if on, beats := e.CountIn(); !on || beats != 1 {
		t.Fatalf("último tiempo: %v, %d", on, beats)
	}
	if peak < 0.01 {
		t.Fatal("la cuenta no suena")
	}
	block() // ends the count-in 200 samples in
	if on, _ := e.CountIn(); on || e.rt.pos != buf-200 {
		t.Fatalf("tras la cuenta: %v, posición %d", on, e.rt.pos)
	}
	block()
	res, err := e.Pause()
	if err != nil || res == nil {
		t.Fatal(res, err)
	}
	if e.rt.takeAt != 0 || len(e.rt.take) != 2*buf-200 || e.rt.take[0] != float32(int(countLen)%1000)/1000 {
		t.Fatalf("toma en %d, %d muestras, primera %v", e.rt.takeAt, len(e.rt.take), e.rt.take[0])
	}

	// Pausing during the count-in cancels it.
	e.Seek(0)
	e.PlayCountIn(2)
	block()
	e.Pause()
	if on, _ := e.CountIn(); on || e.rt.countLen != 0 {
		t.Fatal("la pausa no canceló la cuenta")
	}
}

func TestFeedbackGuard(t *testing.T) {
	e, err := New(filepath.Join("..", "..", "assets", "GeneralUser-GS.sf2"))
	if err != nil {
		t.Fatal(err)
	}
	const sr, buf = 48000, 256
	offline(t, e, sr, buf, 0, 0)
	e.rt.hotMax = int(sr * 0.3)
	e.SetMonitor(true)
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	feed := func(level float32, sec float64) {
		in := []float32{}
		for range int(sec * sr / buf) {
			in = make([]float32, buf)
			for i := range in {
				in[i] = level
				if i%2 == 1 {
					in[i] = -level
				}
			}
			e.process([][]float32{in}, out)
		}
	}
	// Loud but healthy playing does not trip it.
	feed(0.7, 2)
	if e.FeedbackTripped() || !e.rt.monitor {
		t.Fatal("el protector saltó con señal normal")
	}
	// Input pinned at full scale for longer than 300 ms does.
	feed(0.99, 0.5)
	if !e.FeedbackTripped() || e.rt.monitor {
		t.Fatal("el protector no cortó el acople")
	}
}

func TestTunerTap(t *testing.T) {
	e, err := New(testutil.SoundFont(t))
	if err != nil {
		t.Fatal(err)
	}
	const sr, buf = 48000, 256
	offline(t, e, sr, buf, 0, 0)
	in := [][]float32{make([]float32, buf)}
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	feed := func(blocks int) {
		for range blocks {
			for i := range in[0] {
				in[0][i] = 0.25
			}
			e.process(in, out)
		}
	}

	feed(4)
	if n := e.TunerRead(make([]float32, 4096)); n != 0 {
		t.Fatalf("con el afinador apagado se leyeron %d muestras", n)
	}
	e.SetTuner(true)
	feed(4)
	dst := make([]float32, 4096)
	n := e.TunerRead(dst)
	if n != 4*buf {
		t.Fatalf("se leyeron %d muestras, esperadas %d", n, 4*buf)
	}
	if dst[0] != 0.25 {
		t.Errorf("muestra %v, esperada 0.25", dst[0])
	}
	e.SetTuner(false)
	feed(2)
	if n := e.TunerRead(dst); n != 0 {
		t.Errorf("tras apagarlo se leyeron %d muestras", n)
	}
}

// A tone fed through the input reaches the tuner intact and is recognised.
func TestTunerPitch(t *testing.T) {
	e, err := New(testutil.SoundFont(t))
	if err != nil {
		t.Fatal(err)
	}
	const sr, buf = 48000, 512
	offline(t, e, sr, buf, 0, 0)
	e.SetTuner(true)
	in := [][]float32{make([]float32, buf)}
	out := [][]float32{make([]float32, buf), make([]float32, buf)}
	const want = 110.0 // open A string
	k := 0
	var got []float32
	for range 12 {
		for i := range in[0] {
			in[0][i] = 0.3 * float32(math.Sin(2*math.Pi*want*float64(k)/sr))
			k++
		}
		e.process(in, out)
		chunk := make([]float32, buf)
		got = append(got, chunk[:e.TunerRead(chunk)]...)
	}
	if len(got) != 12*buf {
		t.Fatalf("llegaron %d muestras, esperadas %d", len(got), 12*buf)
	}
	f, conf := tuner.Detect(got[len(got)-tuner.WindowSize(sr):], sr)
	if math.Abs(f-want) > 0.2 || conf < 0.9 {
		t.Errorf("detectado %.2f Hz (confianza %.2f), esperado %.0f", f, conf, want)
	}
}
