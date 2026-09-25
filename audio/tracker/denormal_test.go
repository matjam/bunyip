package tracker

import (
	"math"
	"testing"
)

func subnormal(v float32) bool {
	b := math.Float32bits(v)
	return b&0x7f800000 == 0 && b&0x007fffff != 0
}

// TestSubnormalFiltersFlush plays a resonant filtered note whose sample
// is a short tone looping into digital silence, with the Amiga filter
// on, and checks that neither filter's state is subnormal at the end of
// any block and that both reach exactly zero. Without flushing, a
// decaying biquad settles on the smallest subnormal, and every
// operation on it is a microcode assist on amd64.
func TestSubnormalFiltersFlush(t *testing.T) {
	data := make([]float32, 20000)
	for i := range 1000 {
		data[i] = float32(math.Sin(float64(i)*2*math.Pi/64)) * 0.5
	}
	m := &Module{
		Title: "silence", Channels: 1, Format: FormatIT, LinearSlides: true,
		Samples: []Sample{{Data: data, LoopStart: 1000, LoopEnd: len(data), Loop: LoopForward,
			Volume: 64, GlobalVolume: 64, C4Speed: 8363 * 8}},
		Orders: []int{0}, Speed: 6, Tempo: 125, GlobalVolume: 128, MixVolume: 48,
		Pan: []float32{0},
	}
	inst := Instrument{GlobalVolume: 128, FilterCutoff: 60, FilterResonance: 90}
	for i := range inst.SampleMap {
		inst.NoteMap[i] = i
	}
	m.Instruments = []Instrument{inst}
	pat := Pattern{Rows: make([][]Cell, 64)}
	for r := range pat.Rows {
		pat.Rows[r] = []Cell{{Note: NoteNone}}
	}
	pat.Rows[0][0] = Cell{Note: 48, Instrument: 1}
	m.Patterns = []Pattern{pat}

	p := NewPlayer(m, 48000)
	p.AmigaFilter, p.amigaFilter = true, true
	out := make([]float32, 512*2)
	zeroAt := -1
	for blk := range 400 {
		p.Read(out)
		v := &p.chans[0].voice
		if !v.active {
			t.Fatalf("block %d: the voice stopped; the test needs it looping over silence", blk)
		}
		state := []float32{v.filter.x1, v.filter.x2, v.filter.y1, v.filter.y2,
			p.filterL.x1, p.filterL.x2, p.filterL.y1, p.filterL.y2,
			p.filterR.x1, p.filterR.x2, p.filterR.y1, p.filterR.y2}
		live := 0
		for _, s := range state {
			if subnormal(s) {
				t.Fatalf("block %d ends with subnormal filter state %g", blk, s)
			}
			if s != 0 {
				live++
			}
		}
		if blk > 0 && live == 0 && zeroAt < 0 {
			zeroAt = blk
		}
	}
	if zeroAt < 0 || zeroAt > 100 {
		t.Fatalf("filter state reached zero at block %d, want within 100", zeroAt)
	}
	t.Logf("filter state exactly zero at block %d", zeroAt)
}
