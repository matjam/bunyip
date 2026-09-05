package particle

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
)

// Particle is one live particle. Pos is in world units for a WorldSpace
// system and relative to the system's position otherwise.
type Particle struct {
	Pos, Vel lin.Vec2
	Origin   lin.Vec2 // where it was born, the centre for radial and tangential acceleration
	Age      float32  // seconds alive
	Life     float32  // seconds it lives
	Size     float32  // width at birth
	Rotation float32
	Spin     float32
	Tint     gfx.Color // the palette colour, white without one
}

// System runs one Emitter: it births particles, moves them and draws
// them. Update advances by a step; Draw queues every live particle.
// Create it with New; the zero value has no random source for births.
type System struct {
	e       Emitter
	pos     lin.Vec2
	live    []Particle
	rand    *rng.Rand
	acc     float64 // fractional births carried between updates
	running bool
}

// New makes a system for an emitter and starts it: the emitter's Burst
// is born at its Position and its Prewarm runs. The particle slice is
// allocated once, at the emitter's Max.
func New(e Emitter) *System {
	s := &System{e: e, pos: e.Position, live: make([]Particle, 0, e.max()), rand: rng.New(e.Seed)}
	s.Start()
	return s
}

// SetEmitter retunes the system: later births use the new emitter and
// every live particle is drawn with its look. The random stream, the
// position and the particles are kept. Raising Max reallocates; lowering
// it blocks new births until the live count falls below the new cap.
// Existing particles keep their birth palette tint, size and lifetime;
// acceleration, damping and appearance curves use the new settings.
func (s *System) SetEmitter(e Emitter) {
	s.e = e
	if n := e.max(); n > cap(s.live) {
		live := make([]Particle, len(s.live), n)
		copy(live, s.live)
		s.live = live
	}
}

// Emitter returns the emitter the system runs.
func (s *System) Emitter() Emitter { return s.e }

// SetPosition moves the system. Particles in a WorldSpace system stay
// where they are; the rest move with it.
func (s *System) SetPosition(p lin.Vec2) { s.pos = p }

// Position is where the system is.
func (s *System) Position() lin.Vec2 { return s.pos }

// Start begins emitting, births the emitter's Burst and runs its
// Prewarm. New calls it; call it again to restart a stopped system.
func (s *System) Start() {
	s.running = true
	s.Burst(s.e.Burst)
	if s.e.Prewarm > 0 {
		const step = 1.0 / 60
		for t := float32(0); t < s.e.Prewarm; t += step {
			s.Update(step)
		}
	}
}

// Stop ends emission over time. Live particles die out on their own;
// Finished reports when they have.
func (s *System) Stop() { s.running = false }

// Clear kills every live particle at once.
func (s *System) Clear() { s.live = s.live[:0] }

// Burst births n particles now, whether or not the system is emitting,
// up to the emitter's Max.
func (s *System) Burst(n int) {
	for range n {
		if !s.emit() {
			return
		}
	}
}

// Alive is the number of live particles.
func (s *System) Alive() int { return len(s.live) }

// Emitting reports whether the system births particles over time: it
// has been started, not stopped, and its emitter has a Rate.
func (s *System) Emitting() bool { return s.running && s.e.Rate > 0 }

// Finished reports that the system is not emitting and has no live
// particles, so a one-shot effect can be dropped.
func (s *System) Finished() bool { return !s.Emitting() && len(s.live) == 0 }

// Particles returns the live particles, oldest first, for inspection.
// The slice is reused by Update; copy what must be kept.
func (s *System) Particles() []Particle { return s.live }

// Update advances the simulation by dt seconds; nonpositive dt does nothing.
// Particles age, move and
// die, and new ones are born at the emitter's Rate, with fractional
// births carried to the next update.
func (s *System) Update(dt float64) {
	if dt <= 0 {
		return
	}
	e := &s.e
	step := float32(dt)
	keep := s.live[:0]
	for i := range s.live {
		p := s.live[i]
		p.Age += step
		if p.Age >= p.Life {
			continue
		}
		p.Vel = p.Vel.Add(e.Acceleration.Mul(step))
		if e.RadialAccel != 0 || e.TangentialAccel != 0 {
			r := p.Pos.Sub(p.Origin).Norm()
			p.Vel = p.Vel.Add(r.Mul(e.RadialAccel * step)).Add(r.Perp().Mul(e.TangentialAccel * step))
		}
		if e.Damping > 0 {
			p.Vel = p.Vel.Mul(max(0, 1-e.Damping*step))
		}
		p.Pos = p.Pos.Add(p.Vel.Mul(step))
		p.Rotation += p.Spin * step
		keep = append(keep, p)
	}
	s.live = keep
	if s.Emitting() {
		s.acc += float64(e.Rate) * dt
		n := int(s.acc)
		s.acc -= float64(n)
		for range n {
			if !s.emit() {
				s.acc = 0
				break
			}
		}
	}
}

// emit births one particle; it reports false when the system is full.
func (s *System) emit() bool {
	e := &s.e
	if len(s.live) >= e.max() {
		return false
	}
	r := s.rand
	var origin lin.Vec2
	if e.WorldSpace {
		origin = s.pos
	}
	pos := origin.Add(s.spawnOffset())
	angle := e.Direction + (r.Float()-0.5)*e.Spread
	speed := e.Speed.pick(r)
	life := e.Lifetime.pick(r)
	if life <= 0 {
		life = 1
	}
	size := e.Size.pick(r)
	if size <= 0 {
		size, _ = e.baseSize()
	}
	tint := gfx.White
	if len(e.Palette) > 0 {
		tint = r.Pick(e.Palette)
	}
	s.live = append(s.live, Particle{
		Pos:      pos,
		Vel:      lin.V2(1, 0).Rotate(angle).Mul(speed),
		Origin:   pos,
		Life:     life,
		Size:     size,
		Rotation: e.Rotation.pick(r),
		Spin:     e.Spin.pick(r),
		Tint:     tint,
	})
	return true
}

// spawnOffset picks a birth point in the emitter's shape, relative to
// the system's position.
func (s *System) spawnOffset() lin.Vec2 { return spawnOffset(&s.e.Shape, s.rand) }

// spawnOffset picks a birth point in a shape, relative to the system's
// position. Both the CPU and the instanced system draw from it, so it
// takes the shape and the stream rather than a system.
func spawnOffset(sh *Shape, r *rng.Rand) lin.Vec2 {
	switch sh.Kind {
	case ShapeCircle:
		a := r.Float() * 2 * math.Pi
		d := sh.Radius
		if !sh.Edge {
			d *= float32(math.Sqrt(float64(r.Float())))
		}
		return lin.V2(1, 0).Rotate(a).Mul(d)
	case ShapeRect:
		w, h := sh.W, sh.H
		if !sh.Edge {
			return lin.V2((r.Float()-0.5)*w, (r.Float()-0.5)*h)
		}
		// Walk the border by length so every side is equally dense.
		t := r.Float() * 2 * (w + h)
		switch {
		case t < w:
			return lin.V2(t-w/2, -h/2)
		case t < w+h:
			return lin.V2(w/2, t-w-h/2)
		case t < 2*w+h:
			return lin.V2(w/2-(t-w-h), h/2)
		default:
			return lin.V2(-w/2, h/2-(t-2*w-h))
		}
	case ShapeLine:
		return sh.To.Mul(r.Float())
	}
	return lin.Vec2{}
}

// Draw queues every live particle, oldest first, with the emitter's
// blend mode and layer; both are restored afterwards.
func (s *System) Draw(g *gfx.Graphics) {
	if len(s.live) == 0 {
		return
	}
	e := &s.e
	prevBlend, prevLayer := g.Blend(), g.Layer()
	g.SetBlend(e.Blend)
	g.SetLayer(e.Layer)
	var offset lin.Vec2
	if !e.WorldSpace {
		offset = s.pos
	}
	_, aspect := e.baseSize()
	if e.Aspect > 0 {
		aspect = e.Aspect
	}
	var frames int
	if e.Sheet != nil {
		frames = len(e.Frames)
		if frames == 0 {
			frames = e.Sheet.Count()
		}
	}
	for i := range s.live {
		p := &s.live[i]
		t := p.Age / p.Life
		w := p.Size * e.SizeOverLife.At(t)
		c := e.colorAt(t).Mul(p.Tint)
		c.A *= e.AlphaOverLife.At(t)
		if c.A <= 0 || w <= 0 {
			continue
		}
		sp := gfx.Sprite{
			Pos:      p.Pos.Add(offset),
			Size:     lin.V2(w, w*aspect),
			Origin:   lin.V2(0.5, 0.5),
			Color:    c,
			Rotation: p.Rotation,
		}
		switch {
		case frames > 0:
			u := t
			if len(e.FrameOverLife) > 0 {
				u = e.FrameOverLife.At(t)
			}
			f := min(max(int(u*float32(frames)), 0), frames-1)
			if len(e.Frames) > 0 {
				f = e.Frames[f]
			}
			g.DrawRegion(e.Sheet.Region(f), sp)
		case e.Region.Tex != nil:
			g.DrawRegion(e.Region, sp)
		default:
			g.Draw(e.Texture, sp)
		}
	}
	g.SetBlend(prevBlend)
	g.SetLayer(prevLayer)
}
