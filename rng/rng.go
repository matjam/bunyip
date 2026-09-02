// Package rng is a small, fast, seedable random number generator (PCG32)
// for games: the same seed always gives the same sequence on every
// platform, streams can be forked so systems do not disturb each other,
// and the state can be saved and restored.
package rng

import (
	"math"
	"math/bits"
)

// Rand is one PCG32 stream. It also satisfies math/rand/v2's Source.
type Rand struct {
	state, inc uint64
}

const defaultStream = 0xda3e39cb94b95bdb

// New seeds a generator.
func New(seed uint64) *Rand { return NewStream(seed, defaultStream) }

// NewStream seeds a generator on a particular stream: two generators with
// the same seed and different streams produce unrelated sequences.
func NewStream(seed, stream uint64) *Rand {
	r := &Rand{inc: stream<<1 | 1}
	r.Uint32()
	r.state += seed
	r.Uint32()
	return r
}

// Uint32 returns the next 32 random bits.
func (r *Rand) Uint32() uint32 {
	old := r.state
	r.state = old*6364136223846793005 + r.inc
	xorshifted := uint32(((old >> 18) ^ old) >> 27)
	rot := int(old >> 59)
	return bits.RotateLeft32(xorshifted, -rot)
}

// Uint64 returns the next 64 random bits.
func (r *Rand) Uint64() uint64 { return uint64(r.Uint32())<<32 | uint64(r.Uint32()) }

// Intn returns a value in [0, n). It panics when n is not positive.
func (r *Rand) Intn(n int) int {
	if n <= 0 {
		panic("rng: Intn with n <= 0")
	}
	if n <= math.MaxUint32 {
		// Lemire's nearly divisionless bounded sampling, unbiased.
		bound := uint32(n)
		threshold := -bound % bound
		for {
			x := r.Uint32()
			m := uint64(x) * uint64(bound)
			if uint32(m) >= threshold {
				return int(m >> 32)
			}
		}
	}
	return int(r.Uint64() % uint64(n))
}

// Range returns a value in [lo, hi], inclusive at both ends.
func (r *Rand) Range(lo, hi int) int {
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo + r.Intn(hi-lo+1)
}

// Float returns a value in [0, 1).
func (r *Rand) Float() float32 { return float32(r.Uint32()>>8) / (1 << 24) }

// Float64 returns a value in [0, 1).
func (r *Rand) Float64() float64 { return float64(r.Uint64()>>11) / (1 << 53) }

// Between returns a value in [lo, hi).
func (r *Rand) Between(lo, hi float32) float32 { return lo + (hi-lo)*r.Float() }

// Chance reports true with probability p.
func (r *Rand) Chance(p float32) bool { return r.Float() < p }

// Roll sums dice of the given sides, as in Roll(2, 6) for 2d6.
func (r *Rand) Roll(dice, sides int) int {
	total := 0
	for range dice {
		total += 1 + r.Intn(sides)
	}
	return total
}

// Normal returns a normally distributed value (Box-Muller).
func (r *Rand) Normal(mean, stddev float32) float32 {
	u1 := max(r.Float64(), 1e-12)
	u2 := r.Float64()
	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return mean + stddev*float32(z)
}

// Fork derives an independent generator from this one's next values,
// so a subsystem can take its own stream without disturbing the parent's
// sequence any further.
func (r *Rand) Fork() *Rand { return NewStream(r.Uint64(), r.Uint64()) }

// State returns the generator's full state for saving.
func (r *Rand) State() (state, inc uint64) { return r.state, r.inc }

// Restore sets the state returned by State.
func (r *Rand) Restore(state, inc uint64) { r.state, r.inc = state, inc|1 }

// Pick returns a random element; it panics on an empty slice.
func Pick[T any](r *Rand, items []T) T { return items[r.Intn(len(items))] }

// Shuffle permutes the slice in place (Fisher-Yates).
func Shuffle[T any](r *Rand, items []T) {
	for i := len(items) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		items[i], items[j] = items[j], items[i]
	}
}

// WeightedIndex picks an index with probability proportional to its
// weight; weights at or below zero are never chosen. It returns -1 when
// nothing can be picked.
func WeightedIndex(r *Rand, weights []float32) int {
	var total float32
	for _, w := range weights {
		if w > 0 {
			total += w
		}
	}
	if total <= 0 {
		return -1
	}
	x := r.Float() * total
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		if x < w {
			return i
		}
		x -= w
	}
	for i := len(weights) - 1; i >= 0; i-- {
		if weights[i] > 0 {
			return i
		}
	}
	return -1
}
