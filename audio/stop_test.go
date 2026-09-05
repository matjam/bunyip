package audio

import (
	"math"
	"testing"
)

// maxStep is the largest sample-to-sample change in one channel.
func maxStep(buf []float32) float64 {
	var worst float64
	for i := 2; i < len(buf); i += 2 {
		worst = max(worst, math.Abs(float64(buf[i]-buf[i-2])), math.Abs(float64(buf[i+1]-buf[i-1])))
	}
	return worst
}

// stepBound is what a 440 Hz full-scale sine at 48 kHz can move between
// samples (0.058 at unity, 0.041 after the centre pan) plus the stop
// ramp's own step of a gain of 0.707 over 48 frames.
const stepBound = 0.06

func TestStopRampsOut(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	tone, err := m.NewSound(Sine(440, 1, rate))
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	v := m.Play(tone, PlayOptions{})
	v.OnDone(func() { done++ })
	block := make([]float32, 256*2)
	var out []float32
	for range 4 { // past the sine's own 10 ms fade-in, and mid-cycle
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out[len(out)-512:]); peak > stepBound {
		t.Fatalf("the tone alone steps by %.3f", peak)
	}
	v.Stop()
	for range 2 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > stepBound {
		t.Errorf("stopping stepped by %.3f, want no more than %.3f", peak, stepBound)
	}
	if last := out[len(out)-1]; last != 0 {
		t.Errorf("still audible after the ramp: %v", last)
	}
	if m.Playing() != 0 || v.Playing() {
		t.Errorf("slot not free: %d voices, Playing %v", m.Playing(), v.Playing())
	}
	if done != 1 {
		t.Errorf("OnDone ran %d times, want 1", done)
	}
}

// TestStopRampSpansBlocks stops a voice with a block shorter than the
// ramp, so the ramp has to carry across three of them.
func TestStopRampSpansBlocks(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	tone, _ := m.NewSound(Sine(440, 1, rate))
	v := m.Play(tone, PlayOptions{})
	block := make([]float32, 16*2) // 16 frames, a third of the 48 frame ramp
	var out []float32
	for range 40 {
		m.mix(block)
		out = append(out, block...)
	}
	v.Stop()
	for range 5 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > stepBound {
		t.Errorf("stopping stepped by %.3f, want no more than %.3f", peak, stepBound)
	}
	if m.Playing() != 0 {
		t.Errorf("%d voices left after the ramp", m.Playing())
	}
}

// TestStealRampsOut checks that a voice losing its slot ramps out rather
// than cutting, while the voice that took the slot starts at once.
func TestStealRampsOut(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	m.SetMaxVoices(1)
	tone, _ := m.NewSound(Sine(440, 1, rate))
	quiet, _ := m.NewSound(PCM{Samples: make([]float32, rate), Channels: 1, Rate: rate})
	stolen := 0
	v := m.Play(tone, PlayOptions{Loop: true})
	v.OnDone(func() { stolen++ })
	block := make([]float32, 256*2)
	var out []float32
	for range 4 {
		m.mix(block)
		out = append(out, block...)
	}
	m.Play(quiet, PlayOptions{}) // silent, so only the ramp is heard
	if v.Playing() || m.Playing() != 1 {
		t.Fatalf("stolen voice kept its slot: Playing %v, %d voices", v.Playing(), m.Playing())
	}
	if stolen != 1 {
		t.Fatalf("OnDone ran %d times on a steal, want 1", stolen)
	}
	for range 2 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > stepBound {
		t.Errorf("stealing stepped by %.3f, want no more than %.3f", peak, stepBound)
	}
	if m.Playing() != 1 {
		t.Errorf("%d voices after the ramp, want the new one alone", m.Playing())
	}
}

// TestStopAllRampsOut checks that StopAll frees every slot at once and
// still ramps the sound out.
func TestStopAllRampsOut(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	tone, _ := m.NewSound(Sine(440, 1, rate))
	m.Play(tone, PlayOptions{Loop: true})
	block := make([]float32, 256*2)
	var out []float32
	for range 4 {
		m.mix(block)
		out = append(out, block...)
	}
	m.StopAll()
	if m.Playing() != 0 {
		t.Fatalf("StopAll left %d voices", m.Playing())
	}
	for range 2 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > stepBound {
		t.Errorf("StopAll stepped by %.3f, want no more than %.3f", peak, stepBound)
	}
	if last := out[len(out)-1]; last != 0 {
		t.Errorf("still audible after the ramp: %v", last)
	}
}

// TestFadeOutShorterThanABlock checks the end of a fade that finishes
// inside one block, which used to cut at whatever level it had reached.
func TestFadeOutShorterThanABlock(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	tone, _ := m.NewSound(Sine(440, 1, rate))
	v := m.Play(tone, PlayOptions{})
	block := make([]float32, 512*2)
	var out []float32
	for range 2 {
		m.mix(block)
		out = append(out, block...)
	}
	v.FadeOut(0.005) // 240 frames, shorter than the 512 frame block
	for range 3 {
		m.mix(block)
		out = append(out, block...)
	}
	if peak := maxStep(out); peak > stepBound {
		t.Errorf("short fade-out stepped by %.3f, want no more than %.3f", peak, stepBound)
	}
	if last := out[len(out)-1]; last != 0 {
		t.Errorf("still audible after the fade: %v", last)
	}
	if m.Playing() != 0 {
		t.Errorf("%d voices left after the fade", m.Playing())
	}
}
