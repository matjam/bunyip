package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

func syntheticFLAC(t testing.TB, count, channels, bits int) ([]byte, PCM) {
	t.Helper()
	var out bytes.Buffer
	info := &meta.StreamInfo{BlockSizeMin: 16, BlockSizeMax: 256, SampleRate: 1000, NChannels: uint8(channels), BitsPerSample: uint8(bits), NSamples: uint64(count)}
	enc, err := flac.NewEncoder(&out, info)
	if err != nil {
		t.Fatal(err)
	}
	enc.EnablePredictionAnalysis(false)
	p := PCM{Rate: 1000, Channels: channels, Samples: make([]float32, count*channels)}
	for start := 0; start < count; start += 256 {
		n := min(256, count-start)
		f := &frame.Frame{Header: frame.Header{BlockSize: uint16(n), SampleRate: 1000, Channels: frame.Channels(channels - 1), BitsPerSample: uint8(bits)}}
		for c := range channels {
			samples := make([]int32, n)
			for i := range n {
				samples[i] = int32((start+i)%31-15)*int32(1<<(bits-6)) + int32(c)
				p.Samples[(start+i)*channels+c] = float32(float64(samples[i]) / float64(uint64(1)<<(bits-1)))
			}
			f.Subframes = append(f.Subframes, &frame.Subframe{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: samples, NSamples: n})
		}
		if err := enc.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), p
}

func TestDecodeFLACFormats(t *testing.T) {
	for _, channels := range []int{1, 2, 6} {
		for _, bits := range []int{16, 24} {
			data, want := syntheticFLAC(t, 600, channels, bits)
			got, err := Decode(data)
			if err != nil || got.Rate != want.Rate || got.Channels != channels || len(got.Samples) != len(want.Samples) {
				t.Fatal(channels, bits, got, err)
			}
			for i, s := range got.Samples {
				if s != want.Samples[i] {
					t.Fatalf("%dch %dbit sample%d: %v != %v", channels, bits, i, s, want.Samples[i])
				}
			}
		}
	}
}

func TestFLACSeekFrameExact(t *testing.T) {
	data, want := syntheticFLAC(t, 900, 2, 24)
	d, err := newFLACDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, pos := range []int64{0, 257, 700, 12, 899, 900, 0} {
		if err := d.SeekFrame(pos); err != nil {
			t.Fatal(err)
		}
		out := make([]float32, 2)
		n, err := d.Read(out)
		if pos == 900 {
			if n != 0 || !errors.Is(err, io.EOF) {
				t.Fatal(n, err)
			}
			continue
		}
		if n != 2 || err != nil || out[0] != want.Samples[pos*2] || out[1] != want.Samples[pos*2+1] {
			t.Fatal(pos, out, n, err)
		}
	}
}

type countedFLACReader struct {
	*bytes.Reader
	read   atomic.Int64
	closed atomic.Bool
}

func (r *countedFLACReader) Read(out []byte) (int, error) {
	n, err := r.Reader.Read(out)
	r.read.Add(int64(n))
	return n, err
}
func (r *countedFLACReader) Close() error { r.closed.Store(true); return nil }

func TestMusicFLACIncrementalLoopAndBorrowedReader(t *testing.T) {
	data, want := syntheticFLAC(t, 20000, 1, 16)
	r := &countedFLACReader{Reader: bytes.NewReader(data)}
	m := NewMixer(1000)
	mu, err := m.OpenMusic(r, true)
	if err != nil {
		t.Fatal(err)
	}
	defer mu.Close()
	if r.read.Load() >= int64(len(data)) {
		t.Fatal("music decoded entire FLAC at open")
	}
	if err := mu.SetLoopRange(10*time.Millisecond, 13*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	out := readRange(t, mu, 9)
	for i := range 9 {
		if math.Abs(float64(out[i*2]-want.Samples[10+i%3])) > 1e-7 {
			t.Fatal(i, out[i*2])
		}
	}
	mu.Close()
	if r.closed.Load() {
		t.Fatal("borrowed FLAC reader closed")
	}
}

func TestFLACCorruptAndEmpty(t *testing.T) {
	data, _ := syntheticFLAC(t, 20, 1, 16)
	bad := append([]byte(nil), data...)
	bad[len(bad)-1] ^= 1
	for _, b := range [][]byte{nil, []byte("fLaC"), data[:len(data)-1], bad} {
		if _, err := DecodeFLAC(b); err == nil {
			t.Fatal("corrupt FLAC accepted")
		}
	}
	empty, _ := syntheticFLAC(t, 0, 1, 16)
	p, err := DecodeFLAC(empty)
	if err != nil || len(p.Samples) != 0 {
		t.Fatal(p, err)
	}
}

func TestFLACUnsupportedSampleWidth(t *testing.T) {
	data, _ := syntheticFLAC(t, 20, 1, 24)
	// STREAMINFO packs the five-bit (width-1) field above its sample count.
	packed := binary.BigEndian.Uint64(data[18:26])
	packed = (packed & ^(uint64(31) << 36)) | uint64(31)<<36
	binary.BigEndian.PutUint64(data[18:26], packed)
	if _, err := DecodeFLAC(data); err == nil {
		t.Fatal("unsupported32-bit FLAC accepted")
	}
}

func FuzzDecodeFLAC(f *testing.F) {
	data, _ := syntheticFLAC(f, 20, 1, 16)
	f.Add(data)
	f.Add([]byte("fLaC"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodeFLAC(data) })
}
