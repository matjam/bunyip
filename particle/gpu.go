package particle

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
)

// GPUSystem runs one Emitter over far more particles than System can
// afford: hundreds of thousands rather than thousands. It keeps each
// particle as a few numbers in parallel arrays, moves them with plain
// loops over those arrays, and draws the whole system as one instanced
// draw call through gfx.DrawParticles or gfx.DrawParticles3D.
//
// To use one, call NewGPU with an Emitter, then Update each step and
// Draw each frame, as with System. The difference is what it costs: no
// per-particle sprite is built and no vertices are written, so the frame
// cost is the simulation plus one upload. Raise Emitter.Max, which
// defaults to 1000 for the CPU path.
//
// A GPUSystem draws every particle with one texture and one blend mode.
// Layering against sprites still works: a batch takes the layer the
// emitter names, and sprites on lower layers draw under it.
type GPUSystem struct {
	e       Emitter
	pos     lin.Vec2
	p       soa
	rand    *rng.Rand
	acc     float64 // fractional births carried between updates
	running bool
	// clock is seconds since Start, which a stateless emitter evaluates
	// its whole stream from.
	clock float32
	// stoppedAt is the final permitted birth time while stopped.
	stoppedAt float32
	// statelessAlive counts the instances produced by the last draw.
	statelessAlive int

	// The plane the simulation's 2D coordinates sit in for Draw3D.
	origin, xAxis, yAxis lin.Vec3

	quads  []gfx.ParticleQuad
	tables lookup
}

// soa holds the live particles as parallel arrays, so each step of the
// simulation is one loop over one or two contiguous slices.
type soa struct {
	n          int
	posX, posY []float32
	velX, velY []float32
	orgX, orgY []float32 // where each was born, for radial and tangential acceleration
	age, life  []float32
	size       []float32
	rot, spin  []float32
	tint       []gfx.Color // palette colour chosen at birth; white without a palette
}

func (p *soa) grow(n int) {
	p.posX, p.posY = make([]float32, n), make([]float32, n)
	p.velX, p.velY = make([]float32, n), make([]float32, n)
	p.orgX, p.orgY = make([]float32, n), make([]float32, n)
	p.age, p.life = make([]float32, n), make([]float32, n)
	p.size = make([]float32, n)
	p.rot, p.spin = make([]float32, n), make([]float32, n)
	p.tint = make([]gfx.Color, n)
}

// move copies particle i onto slot j, which compaction uses to close the
// gaps the dead leave.
func (p *soa) move(j, i int) {
	p.posX[j], p.posY[j] = p.posX[i], p.posY[i]
	p.velX[j], p.velY[j] = p.velX[i], p.velY[i]
	p.orgX[j], p.orgY[j] = p.orgX[i], p.orgY[i]
	p.age[j], p.life[j] = p.age[i], p.life[i]
	p.size[j] = p.size[i]
	p.rot[j], p.spin[j] = p.rot[i], p.spin[i]
	p.tint[j] = p.tint[i]
}

// lookup holds an emitter's curves sampled onto small tables, so the
// per-particle work is an index rather than a walk over keyframes.
type lookup struct {
	size  [lookupSize]float32
	alpha [lookupSize]float32
	color [lookupSize]gfx.Color
	uv0   []lin.Vec2 // one entry per sheet frame, empty without a sheet
	uv1   []lin.Vec2
}

// lookupSize is how finely the curves are sampled. A particle's life is
// a fraction of a second on screen, so 64 steps are finer than the eye
// or the frame rate.
const lookupSize = 64

// build samples the emitter's curves and sheet frames.
func (l *lookup) build(e *Emitter) {
	for i := range lookupSize {
		t := float32(i) / (lookupSize - 1)
		l.size[i] = e.SizeOverLife.At(t)
		l.alpha[i] = e.AlphaOverLife.At(t)
		l.color[i] = e.colorAt(t)
	}
	l.uv0, l.uv1 = l.uv0[:0], l.uv1[:0]
	if e.Sheet == nil {
		return
	}
	frames := e.Frames
	if len(frames) == 0 {
		frames = make([]int, e.Sheet.Count())
		for i := range frames {
			frames[i] = i
		}
	}
	for _, f := range frames {
		r := e.Sheet.Region(f)
		l.uv0 = append(l.uv0, r.UV0)
		l.uv1 = append(l.uv1, r.UV1)
	}
}

// at reads a table at a lifetime fraction.
func tableAt[T any](table []T, t float32) T {
	i := int(t * (lookupSize - 1))
	return table[min(max(i, 0), len(table)-1)]
}

// NewGPU makes an instanced system for an emitter and starts it: the
// emitter's Burst is born at its Position and its Prewarm runs. The
// arrays are allocated once, at the emitter's Max.
func NewGPU(e Emitter) *GPUSystem {
	s := &GPUSystem{e: e, pos: e.Position, rand: rng.New(e.Seed)}
	s.p.grow(e.max())
	s.setPlaneDefaults()
	s.tables.build(&s.e)
	s.Start()
	return s
}

// setPlaneDefaults puts the simulated plane in the world's xy plane with
// the 2D convention's up pointing at the world's up.
func (s *GPUSystem) setPlaneDefaults() {
	s.origin, s.xAxis, s.yAxis = lin.Vec3{}, lin.V3(1, 0, 0), lin.V3(0, -1, 0)
}

// SetPlane places the simulated plane in the world for Draw3D: a
// particle at (x, y) sits at origin + xAxis*x + yAxis*y. The default is
// the world's xy plane with y inverted, so the emitter's up (-Y, as on
// screen) is the world's up. The axes need not be unit length; scaling
// them scales the effect.
func (s *GPUSystem) SetPlane(origin, xAxis, yAxis lin.Vec3) {
	s.origin, s.xAxis, s.yAxis = origin, xAxis, yAxis
}

// Plane returns the plane Draw3D places particles in.
func (s *GPUSystem) Plane() (origin, xAxis, yAxis lin.Vec3) { return s.origin, s.xAxis, s.yAxis }

// SetEmitter retunes the system: later births use the new emitter and
// every live particle is drawn with its look. Stateful particles keep
// the palette tint chosen at birth. The random stream, the position
// and the particles are kept. Raising Max reallocates.
func (s *GPUSystem) SetEmitter(e Emitter) {
	s.e = e
	if n := e.max(); n > len(s.p.posX) {
		old := s.p
		s.p.grow(n)
		s.p.n = old.n
		copy(s.p.posX, old.posX[:old.n])
		copy(s.p.posY, old.posY[:old.n])
		copy(s.p.velX, old.velX[:old.n])
		copy(s.p.velY, old.velY[:old.n])
		copy(s.p.orgX, old.orgX[:old.n])
		copy(s.p.orgY, old.orgY[:old.n])
		copy(s.p.age, old.age[:old.n])
		copy(s.p.life, old.life[:old.n])
		copy(s.p.size, old.size[:old.n])
		copy(s.p.rot, old.rot[:old.n])
		copy(s.p.spin, old.spin[:old.n])
		copy(s.p.tint, old.tint[:old.n])
	}
	s.p.n = min(s.p.n, e.max())
	s.tables.build(&s.e)
}

// Emitter returns the emitter the system runs.
func (s *GPUSystem) Emitter() Emitter { return s.e }

// SetPosition moves the system. Particles in a WorldSpace system stay
// where they are; the rest move with it.
func (s *GPUSystem) SetPosition(p lin.Vec2) { s.pos = p }

// Position is where the system is.
func (s *GPUSystem) Position() lin.Vec2 { return s.pos }

// Start begins emitting, births the emitter's Burst and runs its
// Prewarm. NewGPU calls it; call it again to restart a stopped system.
// A stateless emitter has neither a burst nor a prewarm: its stream is
// already populated at time zero. Start resets the clock and permits
// new births again.
func (s *GPUSystem) Start() {
	s.running = true
	if s.e.Stateless {
		s.clock = 0
		s.stoppedAt = 0
		return
	}
	s.Burst(s.e.Burst)
	if s.e.Prewarm > 0 {
		const step = 1.0 / 60
		for t := float32(0); t < s.e.Prewarm; t += step {
			s.Update(step)
		}
	}
}

// Stop ends emission over time. Live particles die out on their own;
// Finished reports when they have. A stateless system keeps this clock
// as its final birth time; calling Stop again keeps the original cutoff.
func (s *GPUSystem) Stop() {
	if s.running {
		s.stoppedAt = s.clock
	}
	s.running = false
}

// Clear kills every live particle at once.
func (s *GPUSystem) Clear() {
	s.p.n = 0
	s.statelessAlive = 0
}

// Burst births n particles now, whether or not the system is emitting,
// up to the emitter's Max. A stateless emitter ignores it: its stream is
// a function of the clock and nothing can be added to it.
func (s *GPUSystem) Burst(n int) {
	if s.e.Stateless {
		return
	}
	for range n {
		if !s.emit() {
			return
		}
	}
}

// Alive is the number of live particles. For a stateless emitter it is
// what the last Draw or Draw3D produced, because those particles are
// computed as they are drawn rather than kept.
func (s *GPUSystem) Alive() int {
	if s.e.Stateless {
		return s.statelessAlive
	}
	return s.p.n
}

// Emitting reports whether the system births particles over time: it
// has been started, not stopped, and its emitter has a Rate.
func (s *GPUSystem) Emitting() bool { return s.running && s.e.Rate > 0 }

// Clock is how many seconds a stateless system has run. It stays zero
// for a stateful one.
func (s *GPUSystem) Clock() float32 { return s.clock }

// SetClock moves a stateless system to a time, so an effect can be
// scrubbed, rewound or jumped forward without simulating the gap. Every
// particle is computed from the clock, so the result is the same as
// having run to it. It does nothing to a stateful system, whose
// particles are the accumulation of its steps. A stopped stateless
// system retains its final birth time when the clock changes.
func (s *GPUSystem) SetClock(t float32) {
	if s.e.Stateless {
		s.clock = max(t, 0)
	}
}

// Finished reports that the system is not emitting and has no live
// particles, so a one-shot effect can be dropped. For a stateless system
// it checks lifetimes at the current clock, without requiring a Draw.
func (s *GPUSystem) Finished() bool {
	if s.Emitting() {
		return false
	}
	if s.e.Stateless {
		return !s.statelessHasLiveParticles()
	}
	return s.p.n == 0
}

// Update advances the simulation by dt seconds: particles age, move and
// die, and new ones are born at the emitter's Rate, with fractional
// births carried to the next update. A stateless emitter only advances
// its clock here; its particles are computed in Draw.
func (s *GPUSystem) Update(dt float64) {
	if dt <= 0 {
		return
	}
	step := float32(dt)
	if s.e.Stateless {
		s.clock += step
		return
	}
	e := &s.e
	p := &s.p
	n := p.n
	// Age and compact in one pass, so the dead leave no gaps and the
	// loops below run over live particles alone.
	age, life := p.age[:n], p.life[:n]
	j := 0
	for i := range n {
		age[i] += step
		if age[i] >= life[i] {
			continue
		}
		if j != i {
			p.move(j, i)
		}
		j++
	}
	p.n = j
	n = j

	velX, velY := p.velX[:n], p.velY[:n]
	if e.Acceleration != (lin.Vec2{}) {
		ax, ay := e.Acceleration.X*step, e.Acceleration.Y*step
		for i := range velX {
			velX[i] += ax
		}
		for i := range velY {
			velY[i] += ay
		}
	}
	if e.RadialAccel != 0 || e.TangentialAccel != 0 {
		ra, ta := e.RadialAccel*step, e.TangentialAccel*step
		posX, posY, orgX, orgY := p.posX[:n], p.posY[:n], p.orgX[:n], p.orgY[:n]
		for i := range posX {
			dx, dy := posX[i]-orgX[i], posY[i]-orgY[i]
			d := float32(math.Hypot(float64(dx), float64(dy)))
			if d == 0 {
				continue
			}
			dx, dy = dx/d, dy/d
			// The tangent is the radius turned a quarter turn, which is
			// what Vec2.Perp gives.
			velX[i] += dx*ra - dy*ta
			velY[i] += dy*ra + dx*ta
		}
	}
	if e.Damping > 0 {
		k := max(0, 1-e.Damping*step)
		for i := range velX {
			velX[i] *= k
		}
		for i := range velY {
			velY[i] *= k
		}
	}
	posX, posY := p.posX[:n], p.posY[:n]
	for i := range posX {
		posX[i] += velX[i] * step
	}
	for i := range posY {
		posY[i] += velY[i] * step
	}
	rot, spin := p.rot[:n], p.spin[:n]
	for i := range rot {
		rot[i] += spin[i] * step
	}

	if s.Emitting() {
		s.acc += float64(e.Rate) * dt
		births := int(s.acc)
		s.acc -= float64(births)
		for range births {
			if !s.emit() {
				s.acc = 0
				break
			}
		}
	}
}

// emit births one particle; it reports false when the system is full.
func (s *GPUSystem) emit() bool {
	e := &s.e
	p := &s.p
	if p.n >= e.max() {
		return false
	}
	r := s.rand
	var origin lin.Vec2
	if e.WorldSpace {
		origin = s.pos
	}
	pos := origin.Add(spawnOffset(&e.Shape, r))
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
	if n := len(e.Palette); n > 0 {
		tint = e.Palette[r.Intn(n)]
	}
	i := p.n
	p.posX[i], p.posY[i] = pos.X, pos.Y
	p.orgX[i], p.orgY[i] = pos.X, pos.Y
	vel := lin.V2(1, 0).Rotate(angle).Mul(speed)
	p.velX[i], p.velY[i] = vel.X, vel.Y
	p.age[i], p.life[i] = 0, life
	p.size[i] = size
	p.rot[i], p.spin[i] = e.Rotation.pick(r), e.Spin.pick(r)
	p.tint[i] = tint
	p.n++
	return true
}

// texture is the image the system draws with, and the region of it a
// particle without a sheet frame shows.
func (s *GPUSystem) texture() (*gfx.Texture, lin.Vec2, lin.Vec2) {
	e := &s.e
	switch {
	case e.Sheet != nil:
		return e.Sheet.Texture, lin.Vec2{}, lin.V2(1, 1)
	case e.Region.Tex != nil:
		return e.Region.Tex, e.Region.UV0, e.Region.UV1
	}
	return e.Texture, lin.Vec2{}, lin.V2(1, 1)
}

// buildQuads fills s.quads with this frame's particles, positioned in
// the emitter's own 2D space plus offset.
func (s *GPUSystem) buildQuads(offset lin.Vec2) {
	if s.e.Stateless {
		s.buildStatelessQuads(offset)
		return
	}
	e := &s.e
	p := &s.p
	n := p.n
	s.quads = s.quads[:0]
	if n == 0 {
		return
	}
	if cap(s.quads) < n {
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
	for i := range n {
		t := p.age[i] / p.life[i]
		w := p.size[i] * tableAt(s.tables.size[:], t)
		c := tableAt(s.tables.color[:], t).Mul(p.tint[i])
		c.A *= tableAt(s.tables.alpha[:], t)
		if c.A <= 0 || w <= 0 {
			continue
		}
		q := gfx.ParticleQuad{
			Pos:      lin.V3(p.posX[i]+offset.X, p.posY[i]+offset.Y, 0),
			Rotation: p.rot[i],
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
}

// Draw queues every live particle as one instanced draw, with the
// emitter's blend mode and layer; both are restored afterwards.
func (s *GPUSystem) Draw(g *gfx.Graphics) {
	var offset lin.Vec2
	if !s.e.WorldSpace || s.e.Stateless {
		offset = s.pos
	}
	s.buildQuads(offset)
	if len(s.quads) == 0 {
		return
	}
	prevBlend, prevLayer := g.Blend(), g.Layer()
	g.SetBlend(s.e.Blend)
	g.SetLayer(s.e.Layer)
	tex, _, _ := s.texture()
	g.DrawParticles(tex, s.quads)
	g.SetBlend(prevBlend)
	g.SetLayer(prevLayer)
}

// Draw3D queues every live particle as camera-facing quads in the 3D
// scene, one instanced draw, with the emitter's blend mode. The
// simulated plane is placed by SetPlane, so sizes and positions are in
// world units. soft fades a particle out over that many world units as
// it approaches the geometry behind it; zero draws hard edges.
func (s *GPUSystem) Draw3D(g *gfx.Graphics, soft float32) {
	var offset lin.Vec2
	if !s.e.WorldSpace || s.e.Stateless {
		offset = s.pos
	}
	s.buildQuads(offset)
	if len(s.quads) == 0 {
		return
	}
	// The quads are built in the emitter's plane; lift them into it.
	o, ax, ay := s.origin, s.xAxis, s.yAxis
	for i := range s.quads {
		x, y := s.quads[i].Pos.X, s.quads[i].Pos.Y
		s.quads[i].Pos = lin.V3(
			o.X+ax.X*x+ay.X*y,
			o.Y+ax.Y*x+ay.Y*y,
			o.Z+ax.Z*x+ay.Z*y)
	}
	tex, _, _ := s.texture()
	g.DrawParticles3D(tex, s.quads, gfx.Particles3D{Blend: s.e.Blend, Soft: soft})
}

// Quads returns the instances the last Draw or Draw3D built, for a game
// that wants to place them itself. The slice is reused by the next
// build; copy what must be kept.
func (s *GPUSystem) Quads() []gfx.ParticleQuad { return s.quads }
