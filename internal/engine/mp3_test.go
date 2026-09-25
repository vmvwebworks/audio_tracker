package engine

import (
	"os"
	"testing"
	"time"
)

// TestLoadBackingMP3 decodes a real MP3 when MP3_SAMPLE points to one.
func TestLoadBackingMP3(t *testing.T) {
	path := os.Getenv("MP3_SAMPLE")
	if path == "" {
		t.Skip("MP3_SAMPLE no definido")
	}
	start := time.Now()
	b, err := LoadBacking(path, 44100)
	if err != nil {
		t.Fatal(err)
	}
	var peak float32
	for i := range b.L {
		peak = max(peak, b.L[i], -b.L[i], b.R[i], -b.R[i])
	}
	t.Logf("%.1fs de audio, pico %.2f, decodificado en %v", b.Duration, peak, time.Since(start).Round(time.Millisecond))
	if b.Duration < 1 || peak < 0.01 {
		t.Fatal("MP3 vacío o en silencio")
	}
}
