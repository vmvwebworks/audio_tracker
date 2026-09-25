package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// TakeInfo describes a WAV take on disk.
type TakeInfo struct {
	SampleRate int
	Channels   int
	Bits       int
	Float      bool
	Frames     int64
	TimeRef    int64 // BWF song position of the first sample (0 if absent)
	dataOff    int64
}

// Duration returns the length in seconds.
func (ti TakeInfo) Duration() float64 { return float64(ti.Frames) / float64(ti.SampleRate) }

// Start returns the song time where the take begins.
func (ti TakeInfo) Start() float64 { return float64(ti.TimeRef) / float64(ti.SampleRate) }

// ReadTakeInfo parses a WAV header (fmt, bext and data chunks).
func ReadTakeInfo(path string) (TakeInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return TakeInfo{}, err
	}
	defer f.Close()
	return readInfo(f)
}

func readInfo(f io.ReadSeeker) (TakeInfo, error) {
	var ti TakeInfo
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return ti, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return ti, errors.New("no es un WAV")
	}
	le := binary.LittleEndian
	pos := int64(12)
	var dataSize int64 = -1
	for dataSize < 0 {
		var ch [8]byte
		if _, err := io.ReadFull(f, ch[:]); err != nil {
			return ti, fmt.Errorf("WAV sin datos: %w", err)
		}
		size := int64(le.Uint32(ch[4:]))
		body := pos + 8
		switch string(ch[:4]) {
		case "fmt ":
			b := make([]byte, min(size, 40))
			if _, err := io.ReadFull(f, b); err != nil {
				return ti, err
			}
			format := le.Uint16(b[0:])
			ti.Channels = int(le.Uint16(b[2:]))
			ti.SampleRate = int(le.Uint32(b[4:]))
			ti.Bits = int(le.Uint16(b[14:]))
			if format == 0xFFFE && len(b) >= 26 { // WAVE_FORMAT_EXTENSIBLE
				format = le.Uint16(b[24:])
			}
			ti.Float = format == 3
		case "bext":
			if size >= 346 {
				var b [8]byte
				if _, err := f.Seek(body+338, io.SeekStart); err != nil {
					return ti, err
				}
				if _, err := io.ReadFull(f, b[:]); err != nil {
					return ti, err
				}
				ti.TimeRef = int64(le.Uint64(b[:]))
			}
		case "data":
			dataSize = size
			ti.dataOff = body
		}
		pos = body + size + size&1
		if _, err := f.Seek(pos, io.SeekStart); err != nil && dataSize < 0 {
			return ti, err
		}
	}
	if ti.SampleRate <= 0 || ti.Channels <= 0 {
		return ti, errors.New("WAV sin formato")
	}
	switch {
	case ti.Float && ti.Bits == 32, !ti.Float && (ti.Bits == 16 || ti.Bits == 24 || ti.Bits == 32):
	default:
		return ti, fmt.Errorf("formato WAV no soportado (%d bits)", ti.Bits)
	}
	ti.Frames = dataSize / int64(ti.Channels*ti.Bits/8)
	return ti, nil
}

// readChannels reads the sample data of a WAV as one float slice per channel.
func readChannels(f io.ReadSeeker, ti TakeInfo) ([][]float32, error) {
	if _, err := f.Seek(ti.dataOff, io.SeekStart); err != nil {
		return nil, err
	}
	bps := ti.Bits / 8
	raw := make([]byte, ti.Frames*int64(ti.Channels*bps))
	n, err := io.ReadFull(f, raw)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	frames := n / (ti.Channels * bps)
	le := binary.LittleEndian
	chans := make([][]float32, ti.Channels)
	for c := range chans {
		chans[c] = make([]float32, frames)
	}
	for i := range frames {
		for c := range ti.Channels {
			b := raw[(i*ti.Channels+c)*bps:]
			var v float32
			switch {
			case ti.Float:
				v = math.Float32frombits(le.Uint32(b))
			case bps == 2:
				v = float32(int16(le.Uint16(b))) / (1 << 15)
			case bps == 3:
				v = float32(int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24)) / (1 << 31)
			default:
				v = float32(int32(le.Uint32(b))) / (1 << 31)
			}
			chans[c][i] = v
		}
	}
	return chans, nil
}

// LoadTake reads a WAV take as mono samples resampled to sampleRate. The
// returned position is where the take belongs on the session timeline, in
// samples at sampleRate.
func LoadTake(path string, sampleRate float64) ([]float32, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	ti, err := readInfo(f)
	if err != nil {
		return nil, 0, err
	}
	chans, err := readChannels(f, ti)
	if err != nil {
		return nil, 0, err
	}
	mono := chans[0]
	if len(chans) > 1 {
		mono = make([]float32, len(chans[0]))
		for i := range mono {
			var sum float32
			for _, ch := range chans {
				sum += ch[i]
			}
			mono[i] = sum / float32(len(chans))
		}
	}
	ratio := sampleRate / float64(ti.SampleRate)
	at := int64(math.Round(float64(ti.TimeRef) * ratio))
	return resample(mono, ratio), at, nil
}

// resample converts a signal by ratio (out rate / in rate) with cubic
// (Catmull-Rom) interpolation.
func resample(in []float32, ratio float64) []float32 {
	if ratio == 1 || len(in) < 4 {
		return in
	}
	out := make([]float32, int(float64(len(in))*ratio))
	at := func(i int) float32 { return in[min(max(i, 0), len(in)-1)] }
	for i := range out {
		x := float64(i) / ratio
		j := int(x)
		t := float32(x - float64(j))
		p0, p1, p2, p3 := at(j-1), at(j), at(j+1), at(j+2)
		out[i] = p1 + 0.5*t*(p2-p0+t*(2*p0-5*p1+4*p2-p3+t*(3*(p1-p2)+p3-p0)))
	}
	return out
}
