package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// WriteWAV writes interleaved signed 16-bit PCM WAV to a borrowed writer.
// Samples are rounded and clamped to [-1,1]; non-finite samples and invalid
// PCM are rejected before writing. RIFF files must fit the 32-bit size limit.
// The writer is never closed.
func (p PCM) WriteWAV(w io.Writer) error {
	if err := p.validateWAV(); err != nil {
		return err
	}
	if w == nil {
		return errors.New("audio: nil WAV writer")
	}
	if err := writeWAVHeader(w, p.Rate, p.Channels, uint32(len(p.Samples)*2)); err != nil {
		return err
	}
	return writePCM16(w, p.Samples)
}

// SaveWAV creates or replaces path with 16-bit PCM WAV and closes the file.
func (p PCM) SaveWAV(path string) (err error) {
	if err := p.validateWAV(); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return p.WriteWAV(f)
}

// WriteWAV exports this sound at its mixer rate as stereo 16-bit PCM WAV.
// The writer remains borrowed; see PCM.WriteWAV.
func (s *Sound) WriteWAV(w io.Writer) error {
	if s == nil {
		return errors.New("audio: nil sound")
	}
	return (PCM{Samples: s.samples, Channels: 2, Rate: s.rate}).WriteWAV(w)
}

// SaveWAV creates or replaces path with this sound's stereo PCM WAV.
func (s *Sound) SaveWAV(path string) error {
	if s == nil {
		return errors.New("audio: nil sound")
	}
	return (PCM{Samples: s.samples, Channels: 2, Rate: s.rate}).SaveWAV(path)
}

func wavFormat(rate, channels int) error {
	if rate <= 0 || channels <= 0 || channels > math.MaxUint16/2 || uint64(rate) > math.MaxUint32/2/uint64(channels) {
		return errors.New("audio: invalid WAV rate or channels")
	}
	return nil
}

func (p PCM) validateWAV() error {
	if err := wavFormat(p.Rate, p.Channels); err != nil {
		return err
	}
	if len(p.Samples)%p.Channels != 0 || uint64(len(p.Samples))*2 > math.MaxUint32-36 {
		return errors.New("audio: invalid WAV frame count or RIFF size")
	}
	for _, s := range p.Samples {
		if !finite(s) {
			return errors.New("audio: non-finite PCM sample")
		}
	}
	return nil
}

func writeWAVHeader(w io.Writer, rate, channels int, bytes uint32) error {
	var header [44]byte
	copy(header[:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 36+bytes)
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(header[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], bytes)
	return writeAll(w, header[:])
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return errors.New("audio: invalid writer count")
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func writePCM16(w io.Writer, samples []float32) error {
	var out [4096]byte
	return writePCM16Buffer(w, samples, out[:])
}

func writePCM16Buffer(w io.Writer, samples []float32, out []byte) error {
	for len(samples) > 0 {
		n := min(len(samples), len(out)/2)
		for i, s := range samples[:n] {
			if !finite(s) {
				return fmt.Errorf("audio: non-finite PCM sample")
			}
			v := int16(max(-32768, min(32767, math.Round(float64(s)*32768))))
			binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
		}
		if err := writeAll(w, out[:n*2]); err != nil {
			return err
		}
		samples = samples[n:]
	}
	return nil
}
