package audio

import (
	"math"
	"testing"
)

// TestStreamCopyMatchesResampler reads streams at pitch 1 through the
// direct copy and through the resampler, over block sizes that straddle
// the lookahead buffer's refills and a stream that ends mid-block, and
// requires the same samples, the same end, the same playhead and the same
// lookahead state after every block.
func TestStreamCopyMatchesResampler(t *testing.T) {
	for _, count := range []int{0, 1, 2, 37, 512, 513, 1500, 4096} {
		for _, blocks := range [][]int{{1}, {300}, {512}, {1000}, {7, 513, 64}} {
			fast := voiceMix{v: &Voice{}, stream: &rampStream{count: count}, step: 1}
			slow := voiceMix{v: &Voice{}, stream: &rampStream{count: count}, step: 1}
			for i := 0; ; i++ {
				if i > 10000 {
					t.Fatalf("count %d blocks %v: stream never ended", count, blocks)
				}
				size := blocks[i%len(blocks)]
				a, b := make([]float32, size*2), make([]float32, size*2)
				na, moreA := fast.readStream(a)
				nb, moreB := slow.resampleStream(b)
				if na != nb || moreA != moreB {
					t.Fatalf("count %d blocks %v block %d: copy gave %d frames (more %v), resampler %d (more %v)",
						count, blocks, i, na, moreA, nb, moreB)
				}
				for j := range a[:na*2] {
					if math.Float32bits(a[j]) != math.Float32bits(b[j]) {
						t.Fatalf("count %d blocks %v block %d sample %d: copy %g, resampler %g", count, blocks, i, j, a[j], b[j])
					}
				}
				if fast.pos != slow.pos || fast.v.streamRate != slow.v.streamRate {
					t.Fatalf("count %d blocks %v block %d: state differs\ncopy      pos %v %+v\nresampler pos %v %+v",
						count, blocks, i, fast.pos, fast.v.streamRate.phase, slow.pos, slow.v.streamRate.phase)
				}
				if !moreA {
					break
				}
			}
		}
	}
}

// TestStreamCopyAfterPitchChange starts a stream at another pitch, which
// leaves the phase fractional, and returns it to pitch 1; the output
// must still come from the resampler's interpolation, and pitch 1 from a
// whole phase must go through the copy.
func TestStreamCopyAfterPitchChange(t *testing.T) {
	fast := voiceMix{v: &Voice{}, stream: &rampStream{count: 3000}, step: 1.5}
	slow := voiceMix{v: &Voice{}, stream: &rampStream{count: 3000}, step: 1.5}
	for i := range 12 {
		if i == 3 {
			fast.step, slow.step = 1, 1
		}
		a, b := make([]float32, 256), make([]float32, 256)
		na, _ := fast.readStream(a)
		nb, _ := slow.resampleStream(b)
		if na != nb || fast.v.streamRate != slow.v.streamRate || fast.pos != slow.pos {
			t.Fatalf("block %d: streams diverged", i)
		}
		for j := range a {
			if math.Float32bits(a[j]) != math.Float32bits(b[j]) {
				t.Fatalf("block %d sample %d: %g, want %g", i, j, a[j], b[j])
			}
		}
	}
}
