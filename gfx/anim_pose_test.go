package gfx

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// keyPairBinary is keyPair as a plain binary search, the reference the
// faster lookup must agree with exactly.
func keyPairBinary(times []float32, t float32) (lo, hi int, f float32) {
	last := len(times) - 1
	if t <= times[0] {
		return 0, 0, 0
	}
	if t >= times[last] {
		return last, last, 0
	}
	lo, hi = 0, last
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if span := times[hi] - times[lo]; span > 0 {
		f = (t - times[lo]) / span
	}
	return lo, hi, f
}

func TestKeyPairMatchesBinarySearch(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	check := func(times []float32, at float32) {
		t.Helper()
		lo, hi, f := keyPair(times, at)
		wlo, whi, wf := keyPairBinary(times, at)
		if lo != wlo || hi != whi || f != wf {
			t.Fatalf("keyPair(%v, %v) = %d %d %v, want %d %d %v", times, at, lo, hi, f, wlo, whi, wf)
		}
	}
	for range 2000 {
		n := 1 + r.IntN(40)
		times := make([]float32, n)
		// Even spacing, uneven spacing and repeated keys.
		switch r.IntN(3) {
		case 0:
			for i := range times {
				times[i] = float32(i) / 30
			}
		case 1:
			acc := float32(r.Float32())
			for i := range times {
				times[i] = acc
				acc += r.Float32() * r.Float32() * 2
			}
		default:
			acc := float32(0)
			for i := range times {
				times[i] = acc
				if r.IntN(3) > 0 {
					acc += 0.1
				}
			}
		}
		// Every key, just either side of it, and random times across
		// and beyond the range.
		for _, k := range times {
			check(times, k)
			check(times, math.Nextafter32(k, -1))
			check(times, math.Nextafter32(k, 2))
		}
		for range 20 {
			check(times, times[0]-0.5+(times[n-1]-times[0]+1)*r.Float32())
		}
	}
}

// posedWorld composes a player's world matrices from its local pose the
// way it was first written, with three multiplies per TRS and a full
// multiply per parent link.
func posedWorld(p *AnimPlayer) []lin.Mat4 {
	out := make([]lin.Mat4, len(p.world))
	for _, i := range p.model.order {
		tr, r, s := p.NodeLocal(i)
		local := lin.Translate(tr).Mul(r.Mat4()).Mul(lin.Scale(s))
		if parent := p.model.nodes[i].Parent; parent >= 0 {
			out[i] = out[parent].Mul(local)
		} else {
			out[i] = local
		}
	}
	return out
}

// The closed-form TRS and the affine multiply give the pose the full
// multiplies gave, to within 1e-5 of the matrix's scale, on every path.
func TestPoseMatchesFullMultiply(t *testing.T) {
	m := benchSkeleton()
	setups := map[string]func(p *AnimPlayer){
		"clip":      func(p *AnimPlayer) { p.Play("walk", true) },
		"crossfade": func(p *AnimPlayer) { p.Play("walk", true); p.CrossFade("run", true, 0.5) },
		"blend": func(p *AnimPlayer) {
			p.SetBlend([]AnimBlend{{Clip: "idle", Weight: 0.2, Time: 0.3}, {Clip: "walk", Weight: 0.5, Time: 0.3}, {Clip: "run", Weight: 0.3, Time: 0.3}})
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			p := m.NewAnimPlayer()
			setup(p)
			for frame := range 90 {
				p.Advance(1.0 / 60)
				want := posedWorld(p)
				for n := range want {
					got := p.NodeMatrix(n)
					scale := 1.0
					for _, v := range want[n] {
						scale = max(scale, math.Abs(float64(v)))
					}
					for k := range got {
						if d := math.Abs(float64(got[k] - want[n][k])); d > 1e-5*scale {
							t.Fatalf("frame %d node %d entry %d: %v, want %v", frame, n, k, got[k], want[n][k])
						}
					}
				}
			}
		})
	}
}

// SetBlend called every update reuses its storage.
func TestSetBlendAllocs(t *testing.T) {
	p := benchSkeleton().NewAnimPlayer()
	blend := []AnimBlend{{Clip: "idle", Weight: 0.2}, {Clip: "walk", Weight: 0.5}, {Clip: "run", Weight: 0.3}}
	p.SetBlend(blend)
	p.Advance(1.0 / 60)
	allocs := testing.AllocsPerRun(100, func() {
		for i := range blend {
			blend[i].Time += 1.0 / 60
		}
		p.SetBlend(blend)
		p.Advance(1.0 / 60)
	})
	if allocs != 0 {
		t.Fatalf("SetBlend and Advance allocated %v times per update", allocs)
	}
}
