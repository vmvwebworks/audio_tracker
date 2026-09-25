package engine

import (
	"bufio"
	"encoding/binary"
	"os"
	"time"
)

// Broadcast WAV layout: RIFF header, fmt chunk, bext chunk, data chunk.
// The bext TimeReference tells DAWs such as Reaper where the take belongs
// on the song timeline.
const (
	bextSize        = 602
	bextTimeRefOff  = 12 + 24 + 8 + 338 // TimeReference inside the file
	wavDataSizeOff  = 12 + 24 + 8 + bextSize + 4
	wavHeaderLength = wavDataSizeOff + 4
)

// wavWriter writes mono 24-bit PCM Broadcast WAV files.
type wavWriter struct {
	f       *os.File
	w       *bufio.Writer
	samples int64
	buf     []byte
	timeRef uint64 // sample position of the first sample on the song timeline
}

func createWav(path string, sampleRate int) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	ww := &wavWriter{f: f, w: bufio.NewWriterSize(f, 1<<16)}
	h := make([]byte, wavHeaderLength)
	le := binary.LittleEndian
	copy(h[0:], "RIFF")
	copy(h[8:], "WAVE")

	copy(h[12:], "fmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1) // PCM
	le.PutUint16(h[22:], 1) // mono
	le.PutUint32(h[24:], uint32(sampleRate))
	le.PutUint32(h[28:], uint32(sampleRate*3))
	le.PutUint16(h[32:], 3)
	le.PutUint16(h[34:], 24)

	b := h[36:]
	copy(b[0:], "bext")
	le.PutUint32(b[4:], bextSize)
	bx := b[8:]
	copy(bx[0:256], "Audio Tracker take")
	copy(bx[256:288], "Audio Tracker")
	now := time.Now()
	copy(bx[320:330], now.Format("2006-01-02"))
	copy(bx[330:338], now.Format("15:04:05"))
	le.PutUint16(bx[346:], 1) // bext version

	copy(h[wavDataSizeOff-4:], "data")
	if _, err := ww.w.Write(h); err != nil {
		f.Close()
		return nil, err
	}
	return ww, nil
}

func (ww *wavWriter) write(s []float32) error {
	if cap(ww.buf) < len(s)*3 {
		ww.buf = make([]byte, len(s)*3)
	}
	b := ww.buf[:len(s)*3]
	for i, v := range s {
		x := int32(min(max(v, -1), 1) * 8388607)
		b[3*i], b[3*i+1], b[3*i+2] = byte(x), byte(x>>8), byte(x>>16)
	}
	ww.samples += int64(len(s))
	_, err := ww.w.Write(b)
	return err
}

func (ww *wavWriter) close() error {
	if err := ww.w.Flush(); err != nil {
		ww.f.Close()
		return err
	}
	le := binary.LittleEndian
	data := uint32(ww.samples * 3)
	var b [8]byte
	le.PutUint32(b[:4], wavHeaderLength-8+data)
	ww.f.WriteAt(b[:4], 4)
	le.PutUint32(b[:4], data)
	ww.f.WriteAt(b[:4], wavDataSizeOff)
	le.PutUint64(b[:], ww.timeRef)
	ww.f.WriteAt(b[:], bextTimeRefOff)
	return ww.f.Close()
}
