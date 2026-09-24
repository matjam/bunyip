// Package soft simulates soft bodies, cloth and fluids as particles on
// the entity component system. A Cloth is a sheet that hangs, folds and
// blows in the wind; a SoftBody3 is a closed mesh that squashes and
// springs back while keeping its volume; a Fluid2 is a body of liquid in
// the plane. All three are components stepped by one system.
//
//	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Ground: true})
//	flag := w.SpawnWith(soft.NewCloth(soft.ClothSpec{
//		Width: 24, Height: 16, Spacing: 0.1, Mass: 0.4,
//		Origin: lin.V3(0, 3, 0), Pinned: []int{0, 24 * 15},
//		Wind: lin.V3(6, 0, 1.5),
//	}))
//	w.AddSystem("soft", soft.System)
//
// The solver is extended position-based dynamics. A cloth or soft body
// update is split into Settings.Substeps substeps. A substep predicts
// positions from the velocities, projects the constraints
// Settings.Iterations times, pushes particles out of the solids they
// ended up inside, and reads the new velocities back from how far each
// particle moved. Distance-constraint compliance is in metres per
// newton for SI units: zero is rigid and larger is softer. XPBD reduces
// timestep dependence, but finite iterations still affect convergence.
// A fluid keeps its own
// substep count, one by default, for the reason Fluid2Spec.Substeps
// gives.
//
// Particle positions are world space. A cloth or a soft body carries no
// transform, and is drawn by keeping a gfx.Mesh in step with it:
//
//	cloth.UpdateMesh(mesh)                  // positions and normals
//	ctx.Gfx.DrawMesh(mesh, material, lin.Identity())
//
// Particles collide with the static and kinematic phys colliders in the
// same world, through phys.SignedDistance3 and phys.SignedDistance2, and
// with the ground plane Settings.Ground turns on. The shapes that carry a
// signed distance are Sphere, Box3, Capsule and compounds of those in 3D,
// and Circle, Box2, Polygon2 and Capsule2 in 2D; other shapes, and the
// colliders of dynamic bodies, are ignored. Soft bodies do not push rigid
// bodies back. Triggers are ignored. Particle masks test the collider's
// Layer; its Mask is not used by the soft solver. There is no cloth
// self-collision or collision between separate soft components.
//
// For rigid bodies, joints and queries, see the phys package. Both
// simulations run on the same world and the same colliders.
package soft

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
)

// Settings is the world resource that tunes the soft solver. Zero
// substeps mean 4 and zero iterations 4.
type Settings struct {
	// Gravity3 is the acceleration on cloth and soft bodies. Zero takes
	// the gravity from the phys.Settings3 resource when the world has
	// one, so rigid and soft bodies fall together.
	Gravity3 lin.Vec3
	// Gravity2 is the acceleration on fluids, in view units per second
	// squared, positive downward. Zero takes the gravity from the
	// phys.Settings2 resource when the world has one.
	Gravity2 lin.Vec2
	// Substeps is how many times the solver runs per update; zero means
	// 4. Iterations is how many times it projects the constraints per
	// substep; zero means 4. Raise either for stiffer cloth and firmer
	// jelly, at a cost in time.
	Substeps, Iterations int
	// Ground turns on a floor plane at height GroundY that every particle
	// rests on, for scenes with no collider under them. GroundFriction is
	// the Coulomb coefficient there; zero means 0.5.
	Ground         bool
	GroundY        float32
	GroundFriction float32
}

func (s *Settings) substeps() int {
	if s.Substeps <= 0 {
		return 4
	}
	return s.Substeps
}

func (s *Settings) iterations() int {
	if s.Iterations <= 0 {
		return 4
	}
	return s.Iterations
}

func (s *Settings) groundFriction() float32 {
	if s.GroundFriction <= 0 {
		return 0.5
	}
	return s.GroundFriction
}

// solid3 is one 3D collider gathered for the step, placed once so every
// particle measures against the same rotation and bounds.
type solid3 struct {
	placed   phys.PlacedShape3
	lo, hi   lin.Vec3
	layer    uint32
	friction float32
}

// solid2 is one 2D collider gathered for the step, placed once with its
// world polygon and edge normals.
type solid2 struct {
	placed   phys.PlacedShape2
	lo, hi   lin.Vec2
	layer    uint32
	friction float32
}

// reach3 is the solids a component's particles can touch while they stay
// inside a box: those whose bounds, grown by the particle radius and a
// margin for rounding, meet it. A component fills it before it projects
// its particles, and a particle that has left the box is measured
// against every solid, so the result is the same as measuring every
// particle against every solid.
type reach3 struct {
	lo, hi lin.Vec3
	near   []int32
}

// reach2 is reach3 for fluids.
type reach2 struct {
	lo, hi lin.Vec2
	near   []int32
}

// slack is how much further than the particle radius a solid is still
// counted as within reach, so float rounding in a signed distance can
// never put a skipped solid inside the radius. It grows with the size
// of the coordinates, where rounding does.
func slack(radius, magnitude float32) float32 {
	return radius + 1e-3*abs32(radius) + 1e-4*(1+magnitude)
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

func mag3(v lin.Vec3) float32 { return max(abs32(v.X), abs32(v.Y), abs32(v.Z)) }

func mag2(v lin.Vec2) float32 { return max(abs32(v.X), abs32(v.Y)) }

// reach3 fills r with the solids within reach of particles of the given
// radius inside the box lo, hi.
func (s *state) reach3(r *reach3, lo, hi lin.Vec3, radius float32) {
	r.lo, r.hi = lo, hi
	r.near = r.near[:0]
	m := max(mag3(lo), mag3(hi))
	for i := range s.solids3 {
		so := &s.solids3[i]
		g := slack(radius, max(m, mag3(so.lo), mag3(so.hi)))
		if so.lo.X-g > hi.X || lo.X > so.hi.X+g || so.lo.Y-g > hi.Y || lo.Y > so.hi.Y+g || so.lo.Z-g > hi.Z || lo.Z > so.hi.Z+g {
			continue
		}
		r.near = append(r.near, int32(i))
	}
}

func (s *state) reach2(r *reach2, lo, hi lin.Vec2, radius float32) {
	r.lo, r.hi = lo, hi
	r.near = r.near[:0]
	m := max(mag2(lo), mag2(hi))
	for i := range s.solids2 {
		so := &s.solids2[i]
		g := slack(radius, max(m, mag2(so.lo), mag2(so.hi)))
		if so.lo.X-g > hi.X || lo.X > so.hi.X+g || so.lo.Y-g > hi.Y || lo.Y > so.hi.Y+g {
			continue
		}
		r.near = append(r.near, int32(i))
	}
}

// bounds3 is the box around the particles with inv above zero, the ones
// a step projects, grown by pad; ok is false when there are none.
func bounds3(pos []lin.Vec3, inv []float32, pad float32) (lo, hi lin.Vec3, ok bool) {
	for i, p := range pos {
		if inv[i] == 0 {
			continue
		}
		if !ok {
			lo, hi, ok = p, p, true
			continue
		}
		lo, hi = lo.Min(p), hi.Max(p)
	}
	d := lin.V3(pad, pad, pad)
	return lo.Sub(d), hi.Add(d), ok
}

func inside3(p, lo, hi lin.Vec3) bool {
	return p.X >= lo.X && p.X <= hi.X && p.Y >= lo.Y && p.Y <= hi.Y && p.Z >= lo.Z && p.Z <= hi.Z
}

func inside2(p, lo, hi lin.Vec2) bool {
	return p.X >= lo.X && p.X <= hi.X && p.Y >= lo.Y && p.Y <= hi.Y
}

// state is the system's own resource: the queries it walks and the
// buffers it reuses, so a step allocates nothing.
type state struct {
	cloths  *ecs.Query1[Cloth]
	bodies  *ecs.Query1[SoftBody3]
	fluids  *ecs.Query1[Fluid2]
	solids3 []solid3
	solids2 []solid2
	grads   []lin.Vec3 // volume-constraint gradients
	rows    []lin.Vec3 // shape-matching scratch
}

func stateOf(w *ecs.World) *state {
	s := w.Resource[state]()
	if s == nil {
		w.SetResource(state{
			cloths: w.Query1[Cloth](),
			bodies: w.Query1[SoftBody3](),
			fluids: w.Query1[Fluid2](),
		})
		s = w.Resource[state]()
	}
	return s
}

// System advances every Cloth, SoftBody3 and Fluid2 in the world by dt
// seconds. Register it after the phys system, so soft bodies see where
// the rigid ones ended the update. Nonpositive dt does nothing. Colliders
// are sampled once at entry and held fixed through the soft substeps.
func System(w *ecs.World, dt float64) {
	if dt <= 0 {
		return
	}
	settings := w.Resource[Settings]()
	if settings == nil {
		w.SetResource(Settings{})
		settings = w.Resource[Settings]()
	}
	s := stateOf(w)
	nCloth, nBody, nFluid := s.cloths.Count(), s.bodies.Count(), s.fluids.Count()
	if nCloth == 0 && nBody == 0 && nFluid == 0 {
		return
	}
	gravity3, gravity2 := settings.Gravity3, settings.Gravity2
	if gravity3 == (lin.Vec3{}) {
		if p := w.Resource[phys.Settings3](); p != nil {
			gravity3 = p.Gravity
		}
	}
	if gravity2 == (lin.Vec2{}) {
		if p := w.Resource[phys.Settings2](); p != nil {
			gravity2 = p.Gravity
		}
	}
	// Colliders are gathered once per update. A kinematic collider moves
	// during the update; particles see where it started.
	if nCloth > 0 || nBody > 0 {
		s.gather3(w)
	}
	if nFluid > 0 {
		s.gather2(w)
	}
	h := float32(dt) / float32(settings.substeps())
	iterations := settings.iterations()
	for range settings.substeps() {
		s.cloths.Each(func(_ ecs.Entity, c *Cloth) { c.step(s, settings, gravity3, h, iterations) })
		s.bodies.Each(func(_ ecs.Entity, b *SoftBody3) { b.step(s, settings, gravity3, h, iterations) })
	}
	// A fluid keeps its own substep count: its density solve is a
	// whole-step pressure solve, and splitting it finer leaves the same
	// residual in a shorter step, which reads back as noise.
	s.fluids.Each(func(_ ecs.Entity, f *Fluid2) {
		fh := float32(dt) / float32(f.substeps())
		for range f.substeps() {
			f.step(s, settings, gravity2, fh, iterations)
		}
	})
}

// gather3 collects the 3D colliders particles can be pushed out of: the
// static and kinematic ones whose shape has a signed distance.
func (s *state) gather3(w *ecs.World) {
	s.solids3 = s.solids3[:0]
	w.Each2(func(e ecs.Entity, t *gfx.Transform, c *phys.Collider3) {
		if c.Shape == nil || c.Trigger {
			return
		}
		friction := float32(0.5)
		if b, ok := w.Get[phys.Body3](e); ok {
			if !b.Kinematic && b.Mass > 0 {
				return // a dynamic body: soft bodies do not push it back
			}
			if b.Friction > 0 {
				friction = b.Friction
			}
		}
		rot := t.Rotation
		if rot == (lin.Quat{}) {
			rot = lin.QuatIdentity()
		}
		pos := t.Position.Add(rot.Rotate(c.Offset))
		placed := phys.PlaceShape3(c.Shape, pos, rot)
		if _, _, ok := placed.SignedDistance(pos); !ok {
			return
		}
		lo, hi := placed.Bounds()
		s.solids3 = append(s.solids3, solid3{placed: placed, lo: lo, hi: hi, layer: c.Layer, friction: friction})
	})
}

// gather2 collects the 2D colliders fluid particles can be pushed out of.
func (s *state) gather2(w *ecs.World) {
	s.solids2 = s.solids2[:0]
	w.Each2(func(e ecs.Entity, t *gfx.Transform2, c *phys.Collider2) {
		if c.Shape == nil || c.Trigger {
			return
		}
		friction := float32(0.5)
		if b, ok := w.Get[phys.Body2](e); ok {
			if !b.Kinematic && b.Mass > 0 {
				return
			}
			if b.Friction > 0 {
				friction = b.Friction
			}
		}
		pos := t.Position.Add(c.Offset.Rotate(t.Rotation))
		placed := phys.PlaceShape2(c.Shape, pos, t.Rotation)
		if _, _, ok := placed.SignedDistance(pos); !ok {
			return
		}
		lo, hi := placed.Bounds()
		s.solids2 = append(s.solids2, solid2{placed: placed, lo: lo, hi: hi, layer: c.Layer, friction: friction})
	})
}

// collides reports whether a component whose mask is mask meets a
// collider on the given layer. A zero mask meets everything, as does a
// collider with no layer.
func collides(mask, layer uint32) bool { return mask == 0 || layer == 0 || mask&layer != 0 }

// project3 pushes one particle out of the gathered solids and the ground
// plane, keeping radius of clearance, and applies friction by holding
// back the sideways part of the motion since prev.
// While the particle is inside the reach box, the solids out of reach
// are skipped; once a push takes it outside, the rest are tried in
// order, as they always were.
func (s *state) project3(r *reach3, pos *lin.Vec3, prev lin.Vec3, radius, friction float32, mask uint32, settings *Settings) {
	ni := 0
	for i := 0; i < len(s.solids3); i++ {
		if inside3(*pos, r.lo, r.hi) {
			for ni < len(r.near) && int(r.near[ni]) < i {
				ni++
			}
			if ni == len(r.near) {
				break
			}
			i = int(r.near[ni])
		}
		so := &s.solids3[i]
		if !collides(mask, so.layer) {
			continue
		}
		d, n, ok := so.placed.SignedDistance(*pos)
		if !ok || d >= radius {
			continue
		}
		*pos = pos.Add(n.Mul(radius - d))
		frictionSlide3(pos, prev, n, radius-d, combine(friction, so.friction))
	}
	if settings.Ground {
		if gap := pos.Y - settings.GroundY; gap < radius {
			pos.Y = settings.GroundY + radius
			frictionSlide3(pos, prev, lin.V3(0, 1, 0), radius-gap, combine(friction, settings.groundFriction()))
		}
	}
}

// frictionSlide3 removes part of the motion along the contact plane. Up
// to the depth the particle was pushed out by, all of it goes; past that
// the friction coefficient decides how much.
func frictionSlide3(pos *lin.Vec3, prev, n lin.Vec3, depth, friction float32) {
	if friction <= 0 || depth <= 0 {
		return
	}
	delta := pos.Sub(prev)
	tangent := delta.Sub(n.Mul(delta.Dot(n)))
	l := tangent.Len()
	if l < 1e-9 {
		return
	}
	*pos = pos.Sub(tangent.Mul(min(1, friction*depth/l)))
}

func (s *state) project2(r *reach2, pos *lin.Vec2, prev lin.Vec2, radius, friction float32, mask uint32) {
	ni := 0
	for i := 0; i < len(s.solids2); i++ {
		if inside2(*pos, r.lo, r.hi) {
			for ni < len(r.near) && int(r.near[ni]) < i {
				ni++
			}
			if ni == len(r.near) {
				break
			}
			i = int(r.near[ni])
		}
		so := &s.solids2[i]
		if !collides(mask, so.layer) {
			continue
		}
		d, n, ok := so.placed.SignedDistance(*pos)
		if !ok || d >= radius {
			continue
		}
		*pos = pos.Add(n.Mul(radius - d))
		frictionSlide2(pos, prev, n, radius-d, combine(friction, so.friction))
	}
}

func frictionSlide2(pos *lin.Vec2, prev, n lin.Vec2, depth, friction float32) {
	if friction <= 0 || depth <= 0 {
		return
	}
	delta := pos.Sub(prev)
	tangent := delta.Sub(n.Mul(delta.Dot(n)))
	l := tangent.Len()
	if l < 1e-9 {
		return
	}
	*pos = pos.Sub(tangent.Mul(min(1, friction*depth/l)))
}

// combine mixes two friction coefficients the way phys does, as the
// geometric mean.
func combine(a, b float32) float32 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return sqrt(a * b)
}

// link is one distance constraint between two particles.
type link struct {
	a, b       int32
	rest       float32
	compliance float32
}

// solveLinks projects the distance constraints once. lambda holds the
// accumulated multiplier of each constraint for this substep, which is
// what makes the stiffness independent of the iteration count.
func solveLinks(pos []lin.Vec3, inv []float32, links []link, lambda []float32, h float32) {
	inv2 := 1 / (h * h)
	for i := range links {
		l := &links[i]
		wa, wb := inv[l.a], inv[l.b]
		w := wa + wb
		if w == 0 {
			continue
		}
		d := pos[l.a].Sub(pos[l.b])
		length := d.Len()
		if length < 1e-9 {
			continue
		}
		c := length - l.rest
		alpha := l.compliance * inv2
		dl := (-c - alpha*lambda[i]) / (w + alpha)
		lambda[i] += dl
		corr := d.Mul(dl / length)
		if wa > 0 {
			pos[l.a] = pos[l.a].Add(corr.Mul(wa))
		}
		if wb > 0 {
			pos[l.b] = pos[l.b].Sub(corr.Mul(wb))
		}
	}
}

func sqrt(v float32) float32 { return float32(math.Sqrt(float64(v))) }

// grow returns a slice of n float32s reusing dst's storage, zeroed.
func grow(dst []float32, n int) []float32 {
	if cap(dst) < n {
		return make([]float32, n)
	}
	dst = dst[:n]
	clear(dst)
	return dst
}
