// Package particle simulates 2D particles on the CPU and draws them
// through the sprite stream: fire, smoke, sparks, rain and confetti from
// an Emitter of plain fields. To make a System, call New with an
// Emitter, then call Update each step and Draw each frame.
//
// An Emitter sets how many particles a second (or a Burst of how many at
// once), where they start (a point, a circle, a rectangle, a line),
// their speed, direction and spread, lifetime, size and colour over life
// as Curves and Gradients, gravity and drag, spin, and the texture or
// region each is drawn with, additive or blended. The presets (Fire,
// Smoke, Sparks, Rain, Confetti and more) are starting points to tweak.
// A System owns the live particles and can be moved, paused, stopped
// when its emitter finishes, and asked how many are alive. Thousands of
// particles are cheap. Tens of thousands still draw as one batch but
// cost CPU in Update.
//
// For hundreds of thousands, use GPUSystem instead. NewGPU takes the
// same Emitter and offers the same methods, but keeps its particles as
// numbers in parallel arrays, moves them with plain loops, and draws
// them as one instanced call rather than as sprites; Draw3D puts the
// same system in the 3D scene as camera-facing quads. Setting
// Emitter.Stateless drops the per-particle state as well, computing
// every particle from the seed and the clock, for effects whose
// particles never interact.
//
// Emitter fields follow "zero means the default". An empty Emitter emits
// nothing but is valid, and every preset is an Emitter a game can tweak
// before or after New. Sizes and positions are in view units, angles in
// radians measured from +X towards +Y (so -Pi/2 points up the screen).
package particle

// Curve is a value over a particle's lifetime: keyframes of (t, value)
// with t from 0 (born) to 1 (dying), interpolated linearly and held flat
// outside the first and last key. An empty Curve is 1 everywhere, so
// leaving a curve field blank means "no change".
type Curve []Key

// Key is one point on a Curve.
type Key struct{ T, V float32 }

// Constant is a Curve that holds v for the whole lifetime.
func Constant(v float32) Curve { return Curve{{0, v}} }

// Linear is a Curve from a at birth to b at death.
func Linear(a, b float32) Curve { return Curve{{0, a}, {1, b}} }

// Keys builds a Curve from t, value pairs: Keys(0, 0, 0.2, 1, 1, 0) rises
// quickly then fades. Keys must be in increasing t; a trailing unpaired
// value is ignored.
func Keys(pairs ...float32) Curve {
	c := make(Curve, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		c = append(c, Key{pairs[i], pairs[i+1]})
	}
	return c
}

// At evaluates the curve at t in 0..1.
func (c Curve) At(t float32) float32 {
	switch len(c) {
	case 0:
		return 1
	case 1:
		return c[0].V
	}
	if t <= c[0].T {
		return c[0].V
	}
	last := c[len(c)-1]
	if t >= last.T {
		return last.V
	}
	for i := 1; i < len(c); i++ {
		if t <= c[i].T {
			a, b := c[i-1], c[i]
			if b.T == a.T {
				return b.V
			}
			return a.V + (b.V-a.V)*(t-a.T)/(b.T-a.T)
		}
	}
	return last.V
}

// Range is a span a particle picks a value from at birth, uniformly.
// When Max is not above Min every particle gets Min, so Range{Min: 2} is
// a fixed value.
type Range struct{ Min, Max float32 }

// pick draws a value from the range.
func (r Range) pick(rand interface{ Between(lo, hi float32) float32 }) float32 {
	if r.Max <= r.Min {
		return r.Min
	}
	return rand.Between(r.Min, r.Max)
}
