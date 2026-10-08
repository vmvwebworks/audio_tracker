package tuner

import (
	"math"
	"math/rand"
	"testing"
)

// synth builds a tone with the given harmonic amplitudes (fundamental first).
func synth(freq, sr float64, n int, harmonics ...float64) []float32 {
	x := make([]float32, n)
	for i := range x {
		var v float64
		for h, a := range harmonics {
			v += a * math.Sin(2*math.Pi*freq*float64(h+1)*float64(i)/sr)
		}
		x[i] = float32(v * 0.3)
	}
	return x
}

func TestDetectStrings(t *testing.T) {
	// Open strings of a guitar and a bass, at the sample rates ASIO drivers use.
	for _, sr := range []float64{44100, 48000, 96000} {
		for _, midi := range []int{23, 28, 33, 40, 45, 50, 55, 59, 64, 76} {
			want := Freq(midi, A4)
			x := synth(want, sr, WindowSize(sr), 1, 0.6, 0.4, 0.2)
			got, conf := Detect(x, sr)
			if got == 0 {
				t.Fatalf("%s a %.0f Hz: no detecta nada", FullName(midi), sr)
			}
			if c := math.Abs(1200 * math.Log2(got/want)); c > 1 {
				t.Errorf("%s a %.0f Hz: %.2f Hz, esperado %.2f (%.2f cents de error)", FullName(midi), sr, got, want, c)
			}
			if conf < 0.8 {
				t.Errorf("%s a %.0f Hz: confianza baja %.2f", FullName(midi), sr, conf)
			}
		}
	}
}

func TestDetectWeakFundamental(t *testing.T) {
	// Plucked strings often have a fundamental weaker than the 2nd harmonic.
	const sr = 48000
	want := Freq(40, A4)
	x := synth(want, sr, WindowSize(sr), 0.3, 1, 0.7, 0.4)
	got, _ := Detect(x, sr)
	if c := math.Abs(1200 * math.Log2(got/want)); c > 2 {
		t.Errorf("%.2f Hz, esperado %.2f", got, want)
	}
}

func TestDetectRejects(t *testing.T) {
	const sr = 48000
	n := WindowSize(sr)
	if f, _ := Detect(make([]float32, n), sr); f != 0 {
		t.Errorf("el silencio dio %.1f Hz", f)
	}
	rng := rand.New(rand.NewSource(1))
	noise := make([]float32, n)
	for i := range noise {
		noise[i] = float32(rng.Float64()*2-1) * 0.3
	}
	if f, _ := Detect(noise, sr); f != 0 {
		t.Errorf("el ruido dio %.1f Hz", f)
	}
	if f, _ := Detect(make([]float32, 10), sr); f != 0 {
		t.Errorf("una ventana corta dio %.1f Hz", f)
	}
}

func TestNearest(t *testing.T) {
	n := Nearest(82.41, A4)
	if n.Midi != 40 || math.Abs(n.Cents) > 1 {
		t.Errorf("E2: %+v", n)
	}
	// 20 cents sharp of A4.
	n = Nearest(440*math.Pow(2, 20.0/1200), A4)
	if n.Midi != 69 || math.Abs(n.Cents-20) > 0.01 {
		t.Errorf("A4 +20: %+v", n)
	}
	// With A4 = 432 Hz, 432 Hz is an in-tune A.
	if n = Nearest(432, 432); n.Midi != 69 || math.Abs(n.Cents) > 0.01 {
		t.Errorf("A4=432: %+v", n)
	}
	if got := FullName(40); got != "E2" {
		t.Errorf("FullName(40) = %s", got)
	}
	if got := FullName(61); got != "C#4" {
		t.Errorf("FullName(61) = %s", got)
	}
}
