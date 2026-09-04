package audio

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// binauralBlock plays noise from p through the head model and returns one
// settled block of output.
func binauralBlock(t *testing.T, p lin.Vec3) []float32 {
	t.Helper()
	const rate = 48000
	m := NewMixer(rate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	src, err := m.NewSound(noise(rate, rate))
	if err != nil {
		t.Fatal(err)
	}
	m.Play(src, PlayOptions{Positional: true, Position: p, MinDistance: 100, MaxDistance: 1000})
	block := make([]float32, 4096*2)
	m.mix(block)
	m.mix(block)
	return block
}

// lag is the offset in frames at which the right channel best matches
// the left: positive when the right ear hears the sound later.
func lag(buf []float32, limit int) int {
	best, bestLag := math.Inf(-1), 0
	for d := -limit; d <= limit; d++ {
		var sum float64
		for i := limit; i < len(buf)/2-limit; i++ {
			sum += float64(buf[i*2+1]) * float64(buf[(i-d)*2])
		}
		if sum > best {
			best, bestLag = sum, d
		}
	}
	return bestLag
}

// highs is the energy above roughly a quarter of the rate in one
// channel, measured as the difference between neighbouring samples.
func highs(buf []float32, channel int) float64 {
	var sum float64
	for i := 1; i < len(buf)/2; i++ {
		d := float64(buf[i*2+channel] - buf[(i-1)*2+channel])
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(buf)/2))
}

func TestBinauralSourceOnTheLeft(t *testing.T) {
	out := binauralBlock(t, lin.V3(-5, 0, 0))
	l, r := rms(out)
	if l <= r*1.2 {
		t.Errorf("left ear %.4f, right ear %.4f: the near ear should be louder", l, r)
	}
	// An average head puts about 0.66 ms, or 31 frames at 48 kHz,
	// between the ears for a source at one of them.
	if d := lag(out, 96); d < 20 || d > 50 {
		t.Errorf("right channel lags the left by %d frames, want about 31", d)
	}
	if hl, hr := highs(out, 0), highs(out, 1); hr >= hl*0.8 {
		t.Errorf("high energy left %.4f right %.4f: the far ear should be duller", hl, hr)
	}
}

func TestBinauralSourceOnTheRight(t *testing.T) {
	out := binauralBlock(t, lin.V3(5, 0, 0))
	l, r := rms(out)
	if r <= l*1.2 {
		t.Errorf("right ear %.4f, left ear %.4f: the near ear should be louder", r, l)
	}
	if d := lag(out, 96); d > -20 || d < -50 {
		t.Errorf("right channel leads the left by %d frames, want about -31", d)
	}
}

func TestBinauralAheadIsSymmetric(t *testing.T) {
	out := binauralBlock(t, lin.V3(0, 0, -5))
	for i := 0; i < len(out); i += 2 {
		if out[i] != out[i+1] {
			t.Fatalf("frame %d is %v/%v: a source ahead must reach both ears alike", i/2, out[i], out[i+1])
		}
	}
	if l, _ := rms(out); l < 0.1 {
		t.Fatalf("a source ahead is inaudible: %.4f", l)
	}
}

// TestBinauralElevation checks the only cue the model has for height: a
// source above is brighter than the same source below.
func TestBinauralElevation(t *testing.T) {
	above := binauralBlock(t, lin.V3(0, 5, -1))
	below := binauralBlock(t, lin.V3(0, -5, -1))
	if hi, lo := highs(above, 0), highs(below, 0); hi <= lo*1.2 {
		t.Errorf("high energy above %.4f, below %.4f: above should be brighter", hi, lo)
	}
}

// TestBinauralMovingSourceDoesNotZipper sweeps a source past the
// listener while a tone plays, which changes every head parameter every
// block.
func TestBinauralMovingSourceDoesNotZipper(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	tone, _ := m.NewSound(Sine(200, 2, rate))
	v := m.Play(tone, PlayOptions{Positional: true, Position: lin.V3(-10, 0, 0), MinDistance: 20, MaxDistance: 400})
	block := make([]float32, 256*2)
	var out []float32
	for i := range 60 {
		v.SetPosition(lin.V3(float32(i)-30, 2, -3))
		m.mix(block)
		if i > 2 { // past the sine's own fade-in
			out = append(out, block...)
		}
	}
	// A 200 Hz tone at 48 kHz moves 0.026 between samples at full scale.
	if peak := maxStep(out); peak > 0.05 {
		t.Errorf("a moving source stepped by %.3f", peak)
	}
}

// TestBinauralStopRampsOut checks that a spatialised voice keeps its head
// model through the stop ramp instead of jumping back to plain panning.
func TestBinauralStopRampsOut(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	tone, _ := m.NewSound(Sine(200, 2, rate))
	v := m.Play(tone, PlayOptions{Positional: true, Position: lin.V3(-8, 0, 0), MinDistance: 20, MaxDistance: 400})
	block := make([]float32, 256*2)
	var out []float32
	for range 6 {
		m.mix(block)
		out = append(out, block...)
	}
	v.Stop()
	for range 2 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > 0.05 {
		t.Errorf("stopping a spatialised voice stepped by %.3f", peak)
	}
	if m.Playing() != 0 {
		t.Errorf("%d voices left after the ramp", m.Playing())
	}
}

// TestPanLawUnchanged checks that the default mode still produces
// exactly the constant-power pan the mixer has always used, and that
// asking for the zero settings changes nothing.
func TestPanLawUnchanged(t *testing.T) {
	const rate = 48000
	build := func(zero bool) []float32 {
		m := NewMixer(rate)
		if zero {
			m.SetSpatial(SpatialSettings{})
		}
		src, _ := m.NewSound(noise(rate/10, rate))
		m.Play(src, PlayOptions{Positional: true, Position: lin.V3(3, 0, -4), MinDistance: 10, MaxDistance: 100})
		block := make([]float32, 1024*2)
		m.mix(block)
		return block
	}
	def, zero := build(false), build(true)
	for i := range def {
		if def[i] != zero[i] {
			t.Fatalf("the zero settings changed sample %d: %v then %v", i, def[i], zero[i])
		}
	}
	// The same block worked out from the pan law itself: the source is at
	// 3 right and 4 ahead, five units away, half of MinDistance, so the
	// gain is one and the pan is 0.8 of the direction's rightward part,
	// halved because the source is inside the minimum distance.
	m := NewMixer(rate)
	src, _ := m.NewSound(noise(rate/10, rate))
	pan := float32(3.0/5.0) * 0.8 * 0.5
	l, r := sqrt32((1-pan)/2), sqrt32((1+pan)/2)
	for i := range 1024 {
		want := src.samples[i*2] * l
		if def[i*2] != want {
			t.Fatalf("frame %d left is %v, want %v from the pan law", i, def[i*2], want)
		}
		if def[i*2+1] != src.samples[i*2+1]*r {
			t.Fatalf("frame %d right is %v, want %v from the pan law", i, def[i*2+1], src.samples[i*2+1]*r)
		}
	}
}
