package main

import (
	"encoding/binary"
	"os"
	"sync"
)

// wavWriter records 16-bit stereo PCM, patching the sizes on Close.
type wavWriter struct {
	mu   sync.Mutex
	f    *os.File
	n    int
	rate int
}

func newWAVWriter(path string, rate int) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{f: f, rate: rate}
	w.f.Write(make([]byte, 44))
	return w, nil
}

func (w *wavWriter) Write(samples []float32) {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(max(-1, min(1, s))*32767)))
	}
	w.mu.Lock()
	w.f.Write(buf)
	w.n += len(buf)
	w.mu.Unlock()
}

func (w *wavWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	var h []byte
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(36+w.n))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint32(h, uint32(w.rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(w.rate*4))
	h = binary.LittleEndian.AppendUint16(h, 4)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(w.n))
	w.f.WriteAt(h, 0)
	w.f.Close()
}
