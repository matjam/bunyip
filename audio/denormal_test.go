package audio

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// Filter and reverb state that decays on silence reaches the subnormal
// range and, without flushing, stays there: the smallest subnormal times
// a coefficient rounds back to itself. On amd64 every operation on a
// subnormal costs a microcode assist, and Go never sets flush-to-zero, so
// these tests hold every piece of state to exactly zero after silence.

// subnormalState counts the subnormal values in a set of state.
func subnormalState(vals ...float32) int {
	n := 0
	for _, v := range vals {
		if subnormal(v) {
			n++
		}
	}
	return n
}

func TestFlushThreshold(t *testing.T) {
	for _, c := range []struct{ in, want float32 }{
		{0, 0}, {1, 1}, {-1, -1}, {1e-15, 1e-15}, {-2e-15, -2e-15},
		{9e-16, 0}, {-9e-16, 0}, {math.Float32frombits(1), 0}, {-math.Float32frombits(1), 0},
	} {
		if got := flushTiny(c.in); got != c.want || math.Float32bits(got) == 0x80000000 {
			t.Errorf("flushTiny(%g) = %g, want %g", c.in, got, c.want)
		}
	}
}

// TestSubnormalLowPassFlushes runs biquads from a high cutoff that
// decays within a block to a low one that decays over many, and checks
// that the state is never subnormal between blocks and reaches exactly
// zero.
func TestSubnormalLowPassFlushes(t *testing.T) {
	for _, cutoff := range []float32{30, 400, 20000} {
		var f lowPass
		c := newBiquad(cutoff, benchRate)
		buf := make([]float32, benchBlock*2)
		for i := range buf {
			buf[i] = float32(math.Sin(float64(i) * 0.05))
		}
		f.process(c, buf)
		zeroAt := -1
		for blk := range 200 {
			clear(buf)
			f.process(c, buf)
			if n := subnormalState(f.l0, f.l1, f.r0, f.r1); n > 0 {
				t.Fatalf("%g Hz: block %d ends with %d subnormal state values", cutoff, blk, n)
			}
			if f == (lowPass{}) && zeroAt < 0 {
				zeroAt = blk
			}
		}
		if zeroAt < 0 || zeroAt > 64 {
			t.Errorf("%g Hz: state reached zero at block %d, want within 64", cutoff, zeroAt)
		}
		t.Logf("%g Hz: state exactly zero after %d silent blocks", cutoff, zeroAt+1)
	}
}

// TestSubnormalReverbFlushes feeds the reverb a second of signal and
// then silence. No delay cell or damping state may ever hold a
// subnormal, and the whole tail must reach exactly zero.
func TestSubnormalReverbFlushes(t *testing.T) {
	r := newReverb(benchRate)
	r.set(ReverbSettings{RoomSize: 0.8, Wet: 0.3})
	send := make([]float32, benchBlock*2)
	out := make([]float32, benchBlock*2)
	perSec := benchRate / benchBlock
	zeroAt := -1
	for blk := range 60 * perSec {
		clear(send)
		if blk < perSec {
			for i := range send {
				send[i] = 0.5 * float32(math.Sin(float64(blk*benchBlock*2+i)*0.03))
			}
		}
		r.process(send, out)
		n, live := 0, 0
		for i := range r.combL {
			n += countSub(r.combL[i].buf) + countSub(r.combR[i].buf) + subnormalState(r.combL[i].filt, r.combR[i].filt)
			live += countNonzero(r.combL[i].buf) + countNonzero(r.combR[i].buf)
			if r.combL[i].filt != 0 || r.combR[i].filt != 0 {
				live++
			}
		}
		for i := range r.apL {
			n += countSub(r.apL[i].buf) + countSub(r.apR[i].buf)
			live += countNonzero(r.apL[i].buf) + countNonzero(r.apR[i].buf)
		}
		if n > 0 {
			t.Fatalf("block %d: %d subnormal reverb cells", blk, n)
		}
		if blk >= perSec && live == 0 && zeroAt < 0 {
			zeroAt = blk
		}
	}
	if zeroAt < 0 {
		t.Fatal("reverb tail never reached exactly zero in 59 s of silence")
	}
	t.Logf("reverb tail exactly zero %.1f s after the send went silent", float64(zeroAt-perSec)/float64(perSec))
	if limit := 30 * perSec; zeroAt-perSec > limit {
		t.Errorf("reverb tail took %d blocks to reach zero, want within %d", zeroAt-perSec, limit)
	}
}

func countNonzero(s []float32) int {
	n := 0
	for _, v := range s {
		if v != 0 {
			n++
		}
	}
	return n
}

// TestSubnormalVoiceStateFlushes loops a sound that is a short tone and
// then digital silence through a low-passed, occluded voice, both panned
// and through the binaural head model, and checks that none of the
// voice's filter state is subnormal at the end of any block and that all
// of it is exactly zero within a few blocks of the silence starting.
func TestSubnormalVoiceStateFlushes(t *testing.T) {
	for _, binaural := range []bool{false, true} {
		for _, stereo := range []bool{false, true} {
			m := NewMixer(benchRate)
			m.SetSpatial(SpatialSettings{Binaural: binaural})
			pcm := Sine(440, 2, benchRate)
			if stereo {
				s := make([]float32, len(pcm.Samples)*2)
				for i, v := range pcm.Samples {
					s[i*2], s[i*2+1] = v, v*0.5
				}
				pcm = PCM{Samples: s, Channels: 2, Rate: benchRate}
			}
			tone := benchRate / 10
			for i := tone * pcm.Channels; i < len(pcm.Samples); i++ {
				pcm.Samples[i] = 0
			}
			snd, err := m.NewSound(pcm)
			if err != nil {
				t.Fatal(err)
			}
			v := m.Play(snd, PlayOptions{Loop: true, Positional: true, Position: lin.Vec3{X: 3, Y: -1},
				Occlusion: 0.6, LowPass: 300})
			out := make([]float32, benchBlock*2)
			silentFrom := tone/benchBlock + 1 // the first block wholly past the tone
			for blk := range 2 * benchRate / benchBlock {
				m.mix(out)
				m.mu.Lock()
				vals := []float32{v.occ.l0, v.occ.l1, v.occ.r0, v.occ.r1, v.lp.l0, v.lp.l1, v.lp.r0, v.lp.r1}
				ring := []float32(nil)
				if v.bin != nil {
					vals = append(vals, v.bin.lpL, v.bin.lpR, v.bin.low)
					ring = v.bin.ring
				}
				m.mu.Unlock()
				if n := subnormalState(vals...) + countSub(ring); n > 0 {
					t.Fatalf("binaural %v stereo %v: block %d ends with %d subnormal state values", binaural, stereo, blk, n)
				}
				if blk >= silentFrom+16 {
					if nz := countNonzero(vals) + countNonzero(ring); nz > 0 {
						t.Fatalf("binaural %v stereo %v: %d state values still nonzero %d blocks into the silence",
							binaural, stereo, nz, blk-silentFrom)
					}
				}
			}
		}
	}
}
