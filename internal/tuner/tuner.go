// Package tuner estimates the pitch of a monophonic instrument signal and
// maps it to the nearest note. It has no dependencies on the rest of the app.
package tuner

import (
	"fmt"
	"math"
)

const (
	// MinFreq and MaxFreq bound the detected range: a 7-string guitar or a
	// 5-string bass at the bottom, high frets of a guitar at the top.
	MinFreq = 28.0
	MaxFreq = 1400.0

	// A4 is the default reference pitch in Hz.
	A4 = 440.0

	// yinThreshold is the YIN absolute threshold: lower is stricter.
	yinThreshold = 0.12
	// silence is the RMS below which nothing is detected (about -54 dBFS).
	silence = 0.002
)

// WindowSize returns how many samples Detect needs at the given sample rate
// to see two periods of the lowest note.
func WindowSize(sampleRate float64) int {
	return 2 * int(math.Ceil(sampleRate/MinFreq))
}

// Detect returns the fundamental frequency in Hz of x (the most recent
// samples, at least WindowSize long) and a confidence between 0 and 1. It
// returns 0 when the signal is too quiet or has no clear pitch. It uses the
// YIN algorithm (de Cheveigné and Kawahara, 2002).
func Detect(x []float32, sampleRate float64) (freq, confidence float64) {
	w := len(x) / 2
	tauMax := min(w-1, int(sampleRate/MinFreq))
	tauMin := max(int(sampleRate/MaxFreq), 2)
	if tauMax <= tauMin+2 {
		return 0, 0
	}
	var sum float64
	for _, v := range x[:2*w] {
		sum += float64(v) * float64(v)
	}
	if math.Sqrt(sum/float64(2*w)) < silence {
		return 0, 0
	}

	// Cumulative mean normalised difference function.
	d := make([]float64, tauMax+1)
	var running float64
	d[0] = 1
	for tau := 1; tau <= tauMax; tau++ {
		var s float64
		a, b := x[:w], x[tau:tau+w]
		for j := range a {
			diff := float64(a[j] - b[j])
			s += diff * diff
		}
		running += s
		if running > 0 {
			d[tau] = s * float64(tau) / running
		} else {
			d[tau] = 1
		}
	}

	// First dip under the threshold, followed down to its local minimum.
	best := -1
	for tau := tauMin; tau < tauMax; tau++ {
		if d[tau] < yinThreshold {
			for tau+1 < tauMax && d[tau+1] < d[tau] {
				tau++
			}
			best = tau
			break
		}
	}
	if best < 0 {
		return 0, 0
	}

	// Parabolic interpolation around the minimum for sub-sample precision.
	t := float64(best)
	if best > tauMin && best < tauMax {
		s0, s1, s2 := d[best-1], d[best], d[best+1]
		if den := s0 + s2 - 2*s1; den != 0 {
			t += (s0 - s2) / (2 * den)
		}
	}
	return sampleRate / t, 1 - math.Min(d[best], 1)
}

// Note describes the note nearest to a frequency.
type Note struct {
	Midi  int     // MIDI key number
	Cents float64 // deviation from Midi, -50..+50 (positive = sharp)
}

// Nearest returns the note closest to freq for the reference pitch a4.
func Nearest(freq, a4 float64) Note {
	n := 69 + 12*math.Log2(freq/a4)
	m := int(math.Round(n))
	return Note{Midi: m, Cents: (n - float64(m)) * 100}
}

// Freq returns the frequency of a MIDI key for the reference pitch a4.
func Freq(midi int, a4 float64) float64 {
	return a4 * math.Pow(2, float64(midi-69)/12)
}

// Cents returns how far freq is from the MIDI key target, in cents.
func Cents(freq float64, target int, a4 float64) float64 {
	return 1200 * math.Log2(freq/Freq(target, a4))
}

var names = [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// Name returns the pitch class of a MIDI key ("C#").
func Name(midi int) string { return names[((midi%12)+12)%12] }

// FullName returns the pitch class and octave of a MIDI key ("E2").
func FullName(midi int) string {
	return fmt.Sprintf("%s%d", Name(midi), midi/12-1)
}
