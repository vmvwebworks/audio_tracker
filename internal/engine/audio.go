package engine

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/go-mp3"
)

// Backing is a decoded stereo audio file at the engine's sample rate.
type Backing struct {
	L, R     []float32
	Duration float64 // seconds
}

// LoadBacking decodes a WAV or MP3 file to stereo at sampleRate.
func LoadBacking(path string, sampleRate float64) (*Backing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var chans [][]float32
	var rate int
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		chans, rate, err = decodeMP3(f)
	case ".wav", ".wave":
		var ti TakeInfo
		if ti, err = readInfo(f); err == nil {
			rate = ti.SampleRate
			chans, err = readChannels(f, ti)
		}
	default:
		return nil, fmt.Errorf("formato no soportado: usa WAV o MP3")
	}
	if err != nil {
		return nil, err
	}
	if len(chans) == 0 || len(chans[0]) == 0 {
		return nil, fmt.Errorf("el archivo no tiene audio")
	}
	l, r := chans[0], chans[0]
	if len(chans) > 1 {
		r = chans[1]
	}
	ratio := sampleRate / float64(rate)
	b := &Backing{L: resample(l, ratio)}
	if len(chans) > 1 {
		b.R = resample(r, ratio)
	} else {
		b.R = b.L
	}
	b.Duration = float64(len(b.L)) / sampleRate
	return b, nil
}

// decodeMP3 decodes an MP3 stream (go-mp3 always yields 16-bit stereo).
func decodeMP3(r io.Reader) ([][]float32, int, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, 0, fmt.Errorf("MP3 inválido: %w", err)
	}
	raw, err := io.ReadAll(d)
	if err != nil && len(raw) == 0 {
		return nil, 0, err
	}
	frames := len(raw) / 4
	l := make([]float32, frames)
	rr := make([]float32, frames)
	for i := range frames {
		l[i] = float32(int16(binary.LittleEndian.Uint16(raw[4*i:]))) / (1 << 15)
		rr[i] = float32(int16(binary.LittleEndian.Uint16(raw[4*i+2:]))) / (1 << 15)
	}
	return [][]float32{l, rr}, d.SampleRate(), nil
}
