package tracker

// Performance benchmarks: a 32-channel Impulse Tracker module with a
// note on every channel every four rows, volume envelopes, resonant
// filters on half the channels, vibrato and volume slides, and a
// continuing new-note action so background voices build up to the
// player's cap. They measure what the audio thread pays per block and
// per tick.

import (
	"math"
	"testing"
)

func perf32() *Module {
	const channels, rows = 32, 64
	data := make([]float32, 8192)
	for i := range data {
		data[i] = float32(math.Sin(float64(i)*2*math.Pi/64)) * 0.5
	}
	m := &Module{
		Title: "perf32", Channels: channels, Format: FormatIT, LinearSlides: true,
		Samples: []Sample{{Data: data, LoopStart: 0, LoopEnd: len(data), Loop: LoopForward,
			Volume: 64, GlobalVolume: 64, C4Speed: 8363 * 8}},
		Orders: []int{0}, Speed: 6, Tempo: 125, GlobalVolume: 128, MixVolume: 48,
	}
	for k := range 2 {
		inst := Instrument{NNA: NNAContinue, GlobalVolume: 128, Fadeout: 256,
			FilterCutoff: -1, FilterResonance: -1,
			VolEnv: Envelope{Enabled: true, Points: []EnvPoint{{0, 64}, {10, 48}, {40, 32}, {200, 0}}}}
		if k == 1 {
			inst.FilterCutoff, inst.FilterResonance = 70, 40
		}
		for i := range inst.SampleMap {
			inst.SampleMap[i] = 0
			inst.NoteMap[i] = i
		}
		m.Instruments = append(m.Instruments, inst)
	}
	m.Pan = make([]float32, channels)
	for c := range channels {
		m.Pan[c] = float32(c%5)/2 - 1
	}
	pat := Pattern{Rows: make([][]Cell, rows)}
	for r := range rows {
		pat.Rows[r] = make([]Cell, channels)
		for c := range channels {
			cell := Cell{Note: NoteNone}
			if (r+c)%4 == 0 {
				cell.Note = 36 + (r+c)%24
				cell.Instrument = 1 + c%2
			}
			switch c % 3 {
			case 0:
				cell.Effect, cell.Param = effVibrato, 0x44
			case 1:
				cell.Effect, cell.Param = effVolSlide, 0x01
			}
			pat.Rows[r][c] = cell
		}
	}
	m.Patterns = []Pattern{pat}
	return m
}

// BenchmarkTracker32 renders one 512-frame block of the 32-channel
// module, averaged over a whole pattern so ticks and rows are included.
func BenchmarkTracker32(b *testing.B) {
	for _, cubic := range []bool{false, true} {
		name := "linear"
		if cubic {
			name = "cubic"
		}
		b.Run(name, func(b *testing.B) {
			p := NewPlayer(perf32(), 48000)
			p.Loop = true
			p.Cubic = cubic
			out := make([]float32, 512*2)
			// Warm up: fill the background voice pool.
			for range 2000 {
				p.Read(out)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				p.Read(out)
			}
			b.ReportMetric(float64(len(p.bg)), "bg-voices")
		})
	}
}

// BenchmarkTrackerTick times the tick and row processing alone,
// without rendering, for the 32-channel module.
func BenchmarkTrackerTick(b *testing.B) {
	p := NewPlayer(perf32(), 48000)
	p.Loop = true
	out := make([]float32, 512*2)
	for range 2000 {
		p.Read(out)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.advanceTick()
	}
}
