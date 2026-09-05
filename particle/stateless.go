package particle

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// A stateless emitter keeps nothing between frames. Particle i is born
// at i/Rate seconds after Start, and everything random about it comes
// from hashing the seed with i, so the same index always gives the same
// particle. Its position at any age is a closed form rather than a sum
// of steps, which is what lets the whole stream be rebuilt each frame
// from the clock alone.

// mix is a 64-bit integer hash (SplitMix64's finaliser). It spreads
// nearby indices apart, which matters because the indices a frame draws
// are consecutive.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// stream draws the numbers one particle needs, in a fixed order, from a
// seed and an index. Reading the same fields in the same order gives the
// same particle every time.
type stream struct{ s uint64 }

func newStream(seed uint64, index int64) stream {
	return stream{s: mix(seed ^ (uint64(index) * 0x9e3779b97f4a7c15))}
}

// next returns the next value in 0..1.
func (h *stream) next() float32 {
	h.s = mix(h.s)
	return float32(h.s>>40) / (1 << 24)
}

// pick draws from a Range the way Range.pick does from an rng.Rand.
func (h *stream) pick(r Range) float32 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + (r.Max-r.Min)*h.next()
}

// shapeOffset picks a birth point in a shape, matching spawnOffset's
// distributions but drawing from the hash stream.
func (h *stream) shapeOffset(sh *Shape) lin.Vec2 {
	switch sh.Kind {
	case ShapeCircle:
		a := h.next() * 2 * math.Pi
		d := sh.Radius
		if !sh.Edge {
			d *= float32(math.Sqrt(float64(h.next())))
		}
		return lin.V2(1, 0).Rotate(a).Mul(d)
	case ShapeRect:
		w, hh := sh.W, sh.H
		if !sh.Edge {
			return lin.V2((h.next()-0.5)*w, (h.next()-0.5)*hh)
		}
		t := h.next() * 2 * (w + hh)
		switch {
		case t < w:
			return lin.V2(t-w/2, -hh/2)
		case t < w+hh:
			return lin.V2(w/2, t-w-hh/2)
		case t < 2*w+hh:
			return lin.V2(w/2-(t-w-hh), hh/2)
		default:
			return lin.V2(-w/2, hh/2-(t-2*w-hh))
		}
	case ShapeLine:
		return sh.To.Mul(h.next())
	}
	return lin.Vec2{}
}

// travel returns how far a particle has moved from its birth point after
// t seconds under a constant acceleration and a damping rate. Without
// damping this is the usual v0*t + a*t*t/2. With it the velocity decays
// towards the terminal a/k, and the integral of that decay is the
// closed form below.
func travel(v0, accel lin.Vec2, damping, t float32) lin.Vec2 {
	if damping <= 0 {
		return v0.Mul(t).Add(accel.Mul(0.5 * t * t))
	}
	k := damping
	decay := float32(math.Exp(-float64(k * t)))
	term := accel.Mul(1 / k)              // the speed the drag settles at
	rise := (1 - decay) / k               // the integral of the decay
	return v0.Sub(term).Mul(rise).Add(term.Mul(t))
}

// statelessRange returns the indices of the particles alive at the
// system's clock, newest last, and how long each has to live at most.
func (s *GPUSystem) statelessRange() (first, last int64, maxLife float32) {
	e := &s.e
	maxLife = max(e.Lifetime.Max, e.Lifetime.Min)
	if maxLife <= 0 {
		maxLife = 1
	}
	rate := float64(e.Rate)
	if rate <= 0 {
		return 0, -1, maxLife
	}
	now := float64(s.clock)
	last = int64(math.Floor(now * rate))
	// The indices run back past zero, so at a clock of zero the stream
	// already holds a lifetime of particles born at negative times. That
	// is what makes a stateless emitter need no Prewarm: rain is already
	// falling on the first frame rather than starting from an empty sky.
	first = int64(math.Ceil((now - float64(maxLife)) * rate))
	// Max caps the stream from the newest end, so raising the rate past
	// the cap thins the oldest rather than refusing to draw the newest.
	if n := int64(e.max()); last-first+1 > n {
		first = last - n + 1
	}
	return first, last, maxLife
}

// buildStatelessQuads fills s.quads from the clock alone.
func (s *GPUSystem) buildStatelessQuads(offset lin.Vec2) {
	e := &s.e
	s.quads = s.quads[:0]
	s.statelessAlive = 0
	first, last, _ := s.statelessRange()
	if last < first || e.Rate <= 0 {
		return
	}
	if n := int(last - first + 1); cap(s.quads) < n {
		s.quads = make([]gfx.ParticleQuad, 0, n)
	}
	_, aspect := e.baseSize()
	if e.Aspect > 0 {
		aspect = e.Aspect
	}
	uv0, uv1 := lin.Vec2{}, lin.V2(1, 1)
	if e.Region.Tex != nil && e.Sheet == nil {
		uv0, uv1 = e.Region.UV0, e.Region.UV1
	}
	frames := len(s.tables.uv0)
	invRate := 1 / e.Rate
	for i := first; i <= last; i++ {
		h := newStream(e.Seed, i)
		// The draws come in the order emit makes them, so an emitter
		// switched between the two paths looks the same.
		spawn := h.shapeOffset(&e.Shape)
		angle := e.Direction + (h.next()-0.5)*e.Spread
		speed := h.pick(e.Speed)
		life := h.pick(e.Lifetime)
		if life <= 0 {
			life = 1
		}
		size := h.pick(e.Size)
		if size <= 0 {
			size, _ = e.baseSize()
		}
		rot0 := h.pick(e.Rotation)
		spin := h.pick(e.Spin)
		var pal uint8
		if n := len(e.Palette); n > 0 {
			h.s = mix(h.s)
			pal = uint8(h.s%uint64(n)) + 1
		}
		age := s.clock - float32(i)*invRate
		if age < 0 || age >= life {
			continue
		}
		t := age / life
		w := size * tableAt(s.tables.size[:], t)
		c := tableAt(s.tables.color[:], t)
		if pal > 0 {
			c = c.Mul(e.Palette[pal-1])
		}
		c.A *= tableAt(s.tables.alpha[:], t)
		if c.A <= 0 || w <= 0 {
			continue
		}
		v0 := lin.V2(1, 0).Rotate(angle).Mul(speed)
		pos := spawn.Add(travel(v0, e.Acceleration, e.Damping, age)).Add(offset)
		q := gfx.ParticleQuad{
			Pos:      lin.V3(pos.X, pos.Y, 0),
			Rotation: rot0 + spin*age,
			Size:     lin.V2(w, w*aspect),
			UV0:      uv0,
			UV1:      uv1,
			Color:    c,
		}
		if frames > 0 {
			u := t
			if len(e.FrameOverLife) > 0 {
				u = e.FrameOverLife.At(t)
			}
			f := min(max(int(u*float32(frames)), 0), frames-1)
			q.UV0, q.UV1 = s.tables.uv0[f], s.tables.uv1[f]
		}
		s.quads = append(s.quads, q)
	}
	s.statelessAlive = len(s.quads)
}
