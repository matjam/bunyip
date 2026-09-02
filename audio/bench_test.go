package audio

import "testing"

const benchRate = 48000

// benchBlock is the frame count of one mixed block, the size the output
// device asks for.
const benchBlock = 512

// benchSound is a looping tone long enough that a block never runs off
// the end of it.
func benchSound(b *testing.B, m *Mixer) *Sound {
	b.Helper()
	snd, err := m.NewSound(Sine(440, 2, benchRate))
	if err != nil {
		b.Fatal(err)
	}
	return snd
}

// BenchmarkLowPass times the biquad on one block of stereo frames, which
// is what every filtered or occluded voice pays per block.
func BenchmarkLowPass(b *testing.B) {
	var f lowPass
	c := newBiquad(400, benchRate)
	buf := make([]float32, benchBlock*2)
	for i := range buf {
		buf[i] = float32(i%97)/97 - 0.5
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		f.process(c, buf)
	}
}

// BenchmarkMixOccluded32 mixes a full block with 32 occluded voices, the
// case that showed the biquad dominating the mixer.
func BenchmarkMixOccluded32(b *testing.B) {
	m := NewMixer(benchRate)
	snd := benchSound(b, m)
	for range 32 {
		m.Play(snd, PlayOptions{Loop: true, Occlusion: 0.7, Bus: m.Effects()})
	}
	out := make([]float32, benchBlock*2)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.mix(out)
	}
}

// BenchmarkMixClear32 is the same mix with no occlusion, so the
// difference from BenchmarkMixOccluded32 is the filter's cost.
func BenchmarkMixClear32(b *testing.B) {
	m := NewMixer(benchRate)
	snd := benchSound(b, m)
	for range 32 {
		m.Play(snd, PlayOptions{Loop: true, Bus: m.Effects()})
	}
	out := make([]float32, benchBlock*2)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.mix(out)
	}
}

// BenchmarkMixBuses mixes with many buses, which the block used to walk
// twice through a map.
func BenchmarkMixBuses(b *testing.B) {
	m := NewMixer(benchRate)
	snd := benchSound(b, m)
	buses := make([]*Bus, 0, 32)
	for i := range 32 {
		buses = append(buses, m.NewBus(string(rune('a'+i))))
	}
	for i := range 32 {
		m.Play(snd, PlayOptions{Loop: true, Bus: buses[i]})
	}
	out := make([]float32, benchBlock*2)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.mix(out)
	}
}
