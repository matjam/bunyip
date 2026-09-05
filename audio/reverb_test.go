package audio

import (
	"math"
	"testing"
)

// noise makes a deterministic white noise clip.
func noise(frames, rate int) PCM {
	s := make([]float32, frames)
	state := uint32(12345)
	for i := range s {
		state = state*1664525 + 1013904223
		s[i] = float32(int32(state>>8))/float32(1<<23) - 1
	}
	return PCM{Samples: s, Channels: 1, Rate: rate}
}

// peak is the largest magnitude held in the reverb's comb delays, which
// says whether the feedback is decaying or running away.
func (r *reverb) peak() float64 {
	var worst float64
	for i := range r.combL {
		for _, buf := range [][]float32{r.combL[i].buf, r.combR[i].buf} {
			for _, s := range buf {
				worst = max(worst, math.Abs(float64(s)))
			}
		}
	}
	return worst
}

// TestReverbRoomSizeClamped feeds ten seconds of noise through a reverb
// asked for a room bigger than the model has, which used to push the
// comb feedback to one and run away, first pinning the output to the
// clamp and then reaching NaN.
func TestReverbRoomSizeClamped(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	m.SetReverb(ReverbSettings{RoomSize: 1.5, Damping: 0, Wet: 1})
	src, err := m.NewSound(noise(rate/10, rate))
	if err != nil {
		t.Fatal(err)
	}
	m.Play(src, PlayOptions{Loop: true, Reverb: 1})
	block := make([]float32, 1024*2)
	for b := range rate * 10 / 1024 {
		m.mix(block)
		for i, s := range block {
			if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) || s < -1 || s > 1 {
				t.Fatalf("block %d sample %d is %v", b, i, s)
			}
		}
	}
	// A comb at unity feedback holds thousands within a second and
	// millions within ten; one below unity settles near what it is fed.
	if p := m.reverb.peak(); p > 10 {
		t.Fatalf("reverb energy ran away to %v", p)
	}
}

// nanStream stands in for a decoder or a game Stream that produces a bad
// sample, which must not reach the device.
type nanStream struct{}

func (nanStream) Read(out []float32) int {
	for i := range out {
		switch i % 3 {
		case 0:
			out[i] = float32(math.NaN())
		case 1:
			out[i] = float32(math.Inf(1))
		default:
			out[i] = float32(math.Inf(-1))
		}
	}
	return len(out) / 2
}

func TestOutputScrubsNaN(t *testing.T) {
	m := NewMixer(48000)
	m.PlayStream(nanStream{}, PlayOptions{})
	block := make([]float32, 64*2)
	m.mix(block)
	for i, s := range block {
		if s != 0 {
			t.Fatalf("sample %d is %v, want 0", i, s)
		}
	}
}
