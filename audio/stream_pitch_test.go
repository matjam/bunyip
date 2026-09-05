package audio

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/matjam/bunyip/lin"
)

type callbackStream struct{ read func([]float32) int }

func (s callbackStream) Read(out []float32) int { return s.read(out) }

func TestStreamReadCanCallMixerSetters(t *testing.T) {
	m := NewMixer(48000)
	var v *Voice
	v = m.PlayStream(callbackStream{read: func(out []float32) int {
		m.SetMasterVolume(0.5)
		v.SetPitch(2)
		clear(out)
		return len(out) / 2
	}}, PlayOptions{})
	done := make(chan struct{})
	go func() { m.mix(make([]float32, 16)); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stream.Read deadlocked while calling setters")
	}
}

type rampStream struct{ at, count int }

func (s *rampStream) Read(dst []float32) int {
	n := min(len(dst)/2, s.count-s.at)
	for i := range n {
		dst[i*2] = float32(s.at+i) / 100
		dst[i*2+1] = -dst[i*2]
	}
	s.at += n
	return n
}

func streamSamples(t *testing.T, pitch float32, blocks []int) []float32 {
	t.Helper()
	v := &Voice{}
	sn := voiceMix{v: v, stream: &rampStream{count: 37}, step: pitch}
	var got []float32
	for i := 0; i < 10000; i++ {
		buf := make([]float32, blocks[i%len(blocks)]*2)
		n, more := sn.readStream(buf)
		got = append(got, buf[:n*2]...)
		if !more {
			return got
		}
	}
	t.Fatal("stream never ended")
	return nil
}

func TestStreamPitchAcrossBlocksAndEOF(t *testing.T) {
	for _, pitch := range []float32{0.01, 0.5, 1, 1.25, 4, 64} {
		large := streamSamples(t, pitch, []int{4096})
		small := streamSamples(t, pitch, []int{1, 3, 16, 127})
		if !slices.Equal(large, small) {
			t.Fatalf("pitch %v changes with block boundaries", pitch)
		}
		frames := int(math.Ceil(37 / float64(pitch)))
		if len(large) != frames*2 {
			t.Fatalf("pitch %v produced %d frames want %d", pitch, len(large)/2, frames)
		}
		for i := range frames {
			pos := float64(i) * float64(pitch)
			want := float32(min(pos, 36)) / 100
			if math.Abs(float64(large[i*2]-want)) > 1e-6 || large[i*2+1] != -large[i*2] {
				t.Fatalf("pitch %v frame %d: %v want %v", pitch, i, large[i*2:i*2+2], want)
			}
		}
	}
}

func TestStreamPitchSteadyAllocations(t *testing.T) {
	v := &Voice{}
	sn := voiceMix{v: v, stream: &rampStream{count: math.MaxInt}, step: 1.25}
	out := make([]float32, 64)
	sn.readStream(out)
	if got := testing.AllocsPerRun(1000, func() { sn.readStream(out) }); got != 0 {
		t.Fatalf("allocations/block=%v", got)
	}
}

type seekRamp struct{ rampStream }

func (s *seekRamp) Seek(seconds float64) error { s.at = int(seconds * 48000); return nil }

func TestStreamSeekFlushesPrefetch(t *testing.T) {
	m := NewMixer(48000)
	v := m.PlayStream(&seekRamp{rampStream{count: 1000}}, PlayOptions{Pitch: 0.5, Pan: -1})
	out := make([]float32, 6)
	m.mix(out)
	if err := v.Seek(10.0 / 48000); err != nil {
		t.Fatal(err)
	}
	m.mix(out)
	if math.Abs(float64(out[0]-0.1)) > 1e-6 {
		t.Fatalf("seek used stale lookahead: %v", out)
	}
	if math.Abs(v.Position()-11.5/48000) > 1e-12 {
		t.Fatalf("position=%v", v.Position())
	}
}

func TestStreamDoppler(t *testing.T) {
	m := NewMixer(48000)
	m.SetDoppler(1)
	m.SetSpeedOfSound(100)
	v := m.PlayStream(&rampStream{count: 1000}, PlayOptions{Positional: true, Position: lin.V3(0, 0, -10), Velocity: lin.V3(0, 0, 50)})
	out := make([]float32, 8)
	m.mix(out)
	if math.Abs(v.Position()-8.0/48000) > 1e-12 {
		t.Fatalf("Doppler ignored: position=%v", v.Position())
	}
}

func TestStreamPitch(t *testing.T) {
	for _, pitch := range []float32{0.5, 1, 2, 3.25} {
		m := NewMixer(48000)
		m.PlayStream(&rampStream{count: 37}, PlayOptions{Pitch: pitch, Pan: -1})
		out := make([]float32, 12)
		m.mix(out)
		for i := range 6 {
			want := float32(i) * pitch / 100
			if math.Abs(float64(out[i*2]-want)) > 1e-6 {
				t.Errorf("pitch %v frame %d: %v want %v", pitch, i, out[i*2], want)
				break
			}
		}
	}
}
