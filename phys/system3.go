package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Settings3 is the world resource that tunes 3D simulation. Zero
// substeps or iterations mean 4 and 8.
type Settings3 struct {
	Gravity    lin.Vec3
	Substeps   int
	Iterations int
	// SleepTime is how long a body and everything touching it must rest
	// before they sleep and drop out of the simulation until touched;
	// zero means bodies never sleep. Half a second suits most games.
	SleepTime float32
	// SleepThreshold is the speed (units and radians per second) below
	// which a body counts as resting; zero means 0.05.
	SleepThreshold float32
}

// Body3 makes a 3D entity move. Mass zero is static; Kinematic bodies
// move by their velocity and push others without being pushed.
type Body3 struct {
	Vel    lin.Vec3
	AngVel lin.Vec3 // radians per second about each world axis
	Mass   float32
	// Restitution is bounciness, 0 to 1; Friction is the Coulomb
	// coefficient, 0 slides freely.
	Restitution, Friction float32
	// Damping removes a fraction of velocity per second.
	LinearDamping, AngularDamping float32
	// GravityScale multiplies the world gravity; zero means 1.
	GravityScale float32
	Kinematic    bool
	LockRotation bool
	Sleeping     bool // set by the game to freeze a body
	// CCD sweeps the body against static colliders every substep and
	// stops it at the first one, so a fast small body cannot pass
	// through a thin wall.
	CCD bool

	force  lin.Vec3
	torque lin.Vec3
	// Derived each step.
	invMass    float32
	invInertia mat3 // world space
	// Sleeping bookkeeping and the fraction of this substep's motion
	// CCD allows.
	asleep    bool
	sleepTime float32
	fraction  float32
}

// Dynamic3 returns a moving body with sensible defaults.
func Dynamic3(mass float32) Body3 {
	return Body3{Mass: mass, Restitution: 0.1, Friction: 0.5, GravityScale: 1}
}

// Kinematic3 returns a body moved by its velocity that others cannot push.
func Kinematic3() Body3 { return Body3{Kinematic: true, Friction: 0.5, GravityScale: 1} }

// AddForce accumulates a force until the next update.
func (b *Body3) AddForce(f lin.Vec3) { b.force = b.force.Add(f); b.Wake() }

// AddTorque accumulates a torque until the next update.
func (b *Body3) AddTorque(t lin.Vec3) { b.torque = b.torque.Add(t); b.Wake() }

// AddImpulse changes velocity at once by impulse/mass.
func (b *Body3) AddImpulse(i lin.Vec3) {
	if b.Mass > 0 && !b.Kinematic {
		b.Vel = b.Vel.Add(i.Mul(1 / b.Mass))
		b.Wake()
	}
}

// Asleep reports that the body has come to rest and is skipped by the
// simulation until something touches or pushes it.
func (b *Body3) Asleep() bool { return b.asleep }

// Wake puts a sleeping body back in the simulation.
func (b *Body3) Wake() { b.asleep, b.sleepTime = false, 0 }

// Collider3 gives an entity a shape. An entity with a Collider3 and no
// Body3 is a static obstacle.
type Collider3 struct {
	Shape   Shape3
	Offset  lin.Vec3
	Trigger bool
	Layers
}

// Collision3 is emitted for each pair of colliders that touched this
// update; A and B are ordered by entity id. Impulse is the total normal
// impulse the contact applied in the first substep it was seen, a
// measure of how hard the hit was.
type Collision3 struct {
	A, B    ecs.Entity
	Point   lin.Vec3
	Normal  lin.Vec3 // from A to B
	Depth   float32
	Impulse float32
}

// Trigger3 is emitted while a trigger collider overlaps another collider.
type Trigger3 struct {
	Trigger, Other ecs.Entity
}

type entry3 struct {
	e      ecs.Entity
	t      *gfx.Transform
	b      *Body3
	c      *Collider3
	pos    lin.Vec3
	rot    mat3
	lo, hi lin.Vec3
	bi     int // index into the step's dynamic bodies, -1 otherwise
}

// bodyRec3 is one dynamic body seen this step.
type bodyRec3 struct {
	e ecs.Entity
	t *gfx.Transform
	b *Body3
}

type state3 struct {
	bodies    *ecs.Query2[gfx.Transform, Body3]
	colliders *ecs.Query2[gfx.Transform, Collider3]
	distance  *ecs.Query1[DistanceJoint3]
	hinge     *ecs.Query1[HingeJoint3]
	ball      *ecs.Query1[BallJoint3]
	spring    *ecs.Query1[SpringJoint3]
	fixed     *ecs.Query1[FixedJoint3]
	entries   []entry3
	dynamic   []bodyRec3
	index     slotMap
	parent    []int
	// Scratch kept between steps so a step allocates nothing: ss serves
	// the step and qs the queries, which the game may call while the
	// step's buffers still hold contacts.
	ss, qs          scratch3
	sweep           sweepState
	contacts        []contact3
	arbiters        []arbiter3
	events          []pending3
	reported        pairSet
	rest            []float32
	joints          []jointSolver3
	items           []jointItem3
	distanceSolvers []distanceSolver3
	hingeSolvers    []hingeSolver3
	ballSolvers     []ballSolver3
	springSolvers   []springSolver3
	fixedSolvers    []fixedSolver3
}

// pending3 is a collision event waiting for the solver to say how hard
// the hit was.
type pending3 struct {
	ev  Collision3
	arb int
}

func stateOf3(w *ecs.World) *state3 {
	s := ecs.Resource[state3](w)
	if s == nil {
		ecs.SetResource(w, state3{
			bodies:    ecs.NewQuery2[gfx.Transform, Body3](w),
			colliders: ecs.NewQuery2[gfx.Transform, Collider3](w),
			distance:  ecs.NewQuery1[DistanceJoint3](w),
			hinge:     ecs.NewQuery1[HingeJoint3](w),
			ball:      ecs.NewQuery1[BallJoint3](w),
			spring:    ecs.NewQuery1[SpringJoint3](w),
			fixed:     ecs.NewQuery1[FixedJoint3](w),
		})
		s = ecs.Resource[state3](w)
	}
	return s
}

// System3 advances every 3D body by dt seconds.
func System3(w *ecs.World, dt float64) {
	if dt <= 0 {
		return
	}
	settings := ecs.Resource[Settings3](w)
	if settings == nil {
		ecs.SetResource(w, Settings3{})
		settings = ecs.Resource[Settings3](w)
	}
	substeps, iterations := settings.Substeps, settings.Iterations
	if substeps <= 0 {
		substeps = 4
	}
	if iterations <= 0 {
		iterations = 8
	}
	s := stateOf3(w)
	h := float32(dt) / float32(substeps)
	s.reported.reset()
	for range substeps {
		s.step(w, settings, h, iterations)
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		b.force, b.torque = lin.Vec3{}, lin.Vec3{}
	})
}

// active reports a body that can move others this step: an awake
// dynamic body or a kinematic one that is moving.
func active3(b *Body3) bool {
	if b == nil {
		return false
	}
	if b.Kinematic {
		return !b.Sleeping && (b.Vel != (lin.Vec3{}) || b.AngVel != (lin.Vec3{}))
	}
	return b.invMass > 0
}

func (s *state3) step(w *ecs.World, settings *Settings3, h float32, iterations int) {
	// Integrate velocities.
	s.dynamic = s.dynamic[:0]
	s.index.reset()
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		b.fraction = 1
		if b.Sleeping {
			b.invMass, b.invInertia = 0, mat3{}
			return
		}
		if b.Kinematic || b.Mass <= 0 {
			b.invMass, b.invInertia = 0, mat3{}
			return
		}
		s.index.set(e, len(s.dynamic))
		s.dynamic = append(s.dynamic, bodyRec3{e: e, t: t, b: b})
		if b.asleep {
			// The game may have pushed it.
			if b.Vel != (lin.Vec3{}) || b.AngVel != (lin.Vec3{}) || b.force != (lin.Vec3{}) || b.torque != (lin.Vec3{}) {
				b.Wake()
			} else {
				b.invMass, b.invInertia = 0, mat3{}
				return
			}
		}
		b.invMass = 1 / b.Mass
		b.invInertia = mat3{}
		if !b.LockRotation {
			if c, ok := ecs.Get[Collider3](w, e); ok && c.Shape != nil {
				i := c.Shape.inertia(b.Mass)
				r := mat3FromQuat(t.Rotation)
				local := diag3(inv(i.X), inv(i.Y), inv(i.Z))
				b.invInertia = r.mul(local).mul(r.transpose())
			}
		}
		gs := b.GravityScale
		if gs == 0 {
			gs = 1
		}
		acc := settings.Gravity.Mul(gs).Add(b.force.Mul(b.invMass))
		b.Vel = b.Vel.Add(acc.Mul(h))
		b.AngVel = b.AngVel.Add(b.invInertia.mulVec(b.torque).Mul(h))
		if b.LinearDamping > 0 {
			b.Vel = b.Vel.Mul(max(0, 1-b.LinearDamping*h))
		}
		if b.AngularDamping > 0 {
			b.AngVel = b.AngVel.Mul(max(0, 1-b.AngularDamping*h))
		}
	})
	// Gather colliders with bounds.
	s.entries = s.entries[:0]
	s.colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil {
			return
		}
		b, _ := ecs.Get[Body3](w, e)
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		lo, hi := c.Shape.bounds(pos, rot)
		bi := -1
		if i, ok := s.index.get(e); ok {
			bi = i
		}
		s.entries = append(s.entries, entry3{e: e, t: t, b: b, c: c, pos: pos, rot: rot, lo: lo, hi: hi, bi: bi})
	})
	// Islands: bodies joined by contacts or joints sleep together.
	s.parent = s.parent[:0]
	for i := range s.dynamic {
		s.parent = append(s.parent, i)
	}
	// Broadphase and contact generation.
	s.arbiters = s.arbiters[:0]
	s.events = s.events[:0]
	en := s.entries
	slo, shi := s.sweep.begin(len(en))
	for i := range en {
		slo[i], shi[i] = en[i].lo.X, en[i].hi.X
	}
	s.sweep.pairs(func(i, j int) {
		a, b := &en[i], &en[j]
		if a.lo.Y > b.hi.Y || b.lo.Y > a.hi.Y || a.lo.Z > b.hi.Z || b.lo.Z > a.hi.Z {
			return
		}
		aActive, bActive := active3(a.b), active3(b.b)
		if !aActive && !bActive && !a.c.Trigger && !b.c.Trigger {
			return
		}
		if !a.c.Layers.collides(b.c.Layers) {
			return
		}
		s.contacts = collide3(&s.ss, s.contacts[:0], a.c.Shape, a.pos, a.rot, b.c.Shape, b.pos, b.rot)
		contacts := s.contacts
		if len(contacts) == 0 {
			return
		}
		key := keyOf(a.e, b.e)
		if a.c.Trigger || b.c.Trigger {
			if !s.reported.add(key) {
				if a.c.Trigger {
					ecs.Emit(w, Trigger3{Trigger: a.e, Other: b.e})
				}
				if b.c.Trigger {
					ecs.Emit(w, Trigger3{Trigger: b.e, Other: a.e})
				}
			}
			return
		}
		// Something moving touched a sleeping body: it joins in next step.
		if a.b != nil && a.b.asleep && bActive {
			a.b.Wake()
		}
		if b.b != nil && b.b.asleep && aActive {
			b.b.Wake()
		}
		if a.bi >= 0 && b.bi >= 0 {
			s.union(a.bi, b.bi)
		}
		if !s.reported.add(key) {
			c := contacts[0]
			ev := Collision3{A: a.e, B: b.e, Point: c.point, Normal: c.normal, Depth: c.depth}
			if key.a != a.e {
				ev.A, ev.B, ev.Normal = b.e, a.e, c.normal.Mul(-1)
			}
			s.events = append(s.events, pending3{ev, len(s.arbiters)})
		}
		initArbiter3(s.nextArbiter(), a, b, contacts, h)
	})
	// Joints.
	joints := gatherJoints3(w, s)
	for _, j := range joints {
		j.prepare(h)
		ja, jb := j.sides()
		if ia, ok := s.index.get(ja); ok {
			if ib, ok := s.index.get(jb); ok {
				s.union(ia, ib)
			}
		}
	}
	// Solve.
	for range iterations {
		for _, j := range joints {
			j.solve()
		}
		for i := range s.arbiters {
			s.arbiters[i].solve()
		}
	}
	for _, p := range s.events {
		for _, c := range s.arbiters[p.arb].contacts {
			p.ev.Impulse += c.pn
		}
		ecs.Emit(w, p.ev)
	}
	// Continuous collision: clamp fast bodies to their first static hit.
	for i := range en {
		e := &en[i]
		if e.b == nil || !e.b.CCD || e.b.invMass == 0 {
			continue
		}
		s.sweepDynamic(e, h)
		delta := e.b.Vel.Mul(h)
		length := delta.Len()
		if length < 1e-6 {
			continue
		}
		if f, ok := s.sweepStatic(e, delta); ok {
			e.b.fraction = min(e.b.fraction, min(1, f+0.5*slop/length))
		}
	}
	// Integrate positions.
	for _, r := range s.dynamic {
		b, t := r.b, r.t
		if b.invMass == 0 {
			continue
		}
		t.Position = t.Position.Add(b.Vel.Mul(h * b.fraction))
		if !b.LockRotation && b.AngVel != (lin.Vec3{}) {
			t.Rotation = integrateQuat(t.Rotation, b.AngVel, h*b.fraction)
		}
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		if b.Kinematic && !b.Sleeping {
			t.Position = t.Position.Add(b.Vel.Mul(h))
			if b.AngVel != (lin.Vec3{}) {
				t.Rotation = integrateQuat(t.Rotation, b.AngVel, h)
			}
		}
	})
	s.sleep(settings, h)
}

// integrateQuat turns q by angular velocity w for time h.
func integrateQuat(q lin.Quat, w lin.Vec3, h float32) lin.Quat {
	if q == (lin.Quat{}) {
		q = lin.QuatIdentity()
	}
	wq := lin.Quat{X: w.X, Y: w.Y, Z: w.Z, W: 0}
	dq := wq.Mul(q)
	return lin.Quat{X: q.X + 0.5*h*dq.X, Y: q.Y + 0.5*h*dq.Y, Z: q.Z + 0.5*h*dq.Z, W: q.W + 0.5*h*dq.W}.Norm()
}

func (s *state3) find(i int) int {
	for s.parent[i] != i {
		s.parent[i] = s.parent[s.parent[i]]
		i = s.parent[i]
	}
	return i
}

func (s *state3) union(a, b int) {
	ra, rb := s.find(a), s.find(b)
	if ra != rb {
		s.parent[rb] = ra
	}
}

// sleep puts islands that have rested long enough to sleep.
func (s *state3) sleep(settings *Settings3, h float32) {
	thr, after := settings.SleepThreshold, settings.SleepTime
	if after <= 0 {
		return
	}
	if thr <= 0 {
		thr = 0.05
	}
	rest := s.rest[:0]
	for i, r := range s.dynamic {
		b := r.b
		if !b.asleep {
			if b.Vel.Len() < thr && b.AngVel.Len() < thr {
				b.sleepTime += h
			} else {
				b.sleepTime = 0
			}
		}
		root := s.find(i)
		for len(rest) <= root {
			rest = append(rest, float32(math.Inf(1)))
		}
		rest[root] = min(rest[root], b.sleepTime)
	}
	s.rest = rest
	for i, r := range s.dynamic {
		b := r.b
		if !b.asleep && rest[s.find(i)] >= after {
			b.asleep = true
			b.Vel, b.AngVel = lin.Vec3{}, lin.Vec3{}
		}
	}
}

// sweepStatic sweeps a body's collider along delta against colliders
// that cannot move this step and returns the fraction at which it first
// touches one.
func (s *state3) sweepStatic(e *entry3, delta lin.Vec3) (float32, bool) {
	parts := convexParts(e.c.Shape, e.pos, e.rot)
	if len(parts) == 0 {
		return 0, false
	}
	slo, shi := e.lo.Min(e.lo.Add(delta)), e.hi.Max(e.hi.Add(delta))
	best, found := float32(1), false
	for i := range s.entries {
		o := &s.entries[i]
		if o == e || o.c.Trigger || active3(o.b) || !e.c.Layers.collides(o.c.Layers) {
			continue
		}
		if o.lo.X > shi.X || slo.X > o.hi.X || o.lo.Y > shi.Y || slo.Y > o.hi.Y || o.lo.Z > shi.Z || slo.Z > o.hi.Z {
			continue
		}
		if t, ok := sweepParts(parts, o.c.Shape, o.pos, o.rot, delta); ok && t < best {
			best, found = t, true
		}
	}
	return best, found
}

// sweepDynamic is the speculative contact between a CCD body and the
// other moving bodies: for each pair whose bounding spheres meet along
// their relative motion this substep, the body's shape is swept against
// the other's and both are held at the moment they would touch, so the
// next substep's contact catches them.
func (s *state3) sweepDynamic(e *entry3, h float32) {
	ca, ra := sphereOfBounds3(e.lo, e.hi)
	var parts []convexPart
	for i := range s.entries {
		o := &s.entries[i]
		if o == e || o.b == nil || o.b.invMass == 0 || o.c.Trigger || !e.c.Layers.collides(o.c.Layers) {
			continue
		}
		if o.b.CCD && o.e.ID() < e.e.ID() {
			continue // the pair was swept from the other side
		}
		cb, rb := sphereOfBounds3(o.lo, o.hi)
		d := cb.Sub(ca)
		delta := e.b.Vel.Sub(o.b.Vel).Mul(h) // e's motion as o sees it
		length := delta.Len()
		if length < 1e-6 || !spheresMeet(d.Dot(d), -d.Dot(delta), length*length, ra+rb) {
			continue
		}
		if parts == nil {
			if parts = convexParts(e.c.Shape, e.pos, e.rot); len(parts) == 0 {
				return
			}
		}
		if t, ok := sweepParts(parts, o.c.Shape, o.pos, o.rot, delta); ok && t < 1 {
			f := min(1, t+0.5*slop/length)
			e.b.fraction = min(e.b.fraction, f)
			o.b.fraction = min(o.b.fraction, f)
		}
	}
}

// sphereOfBounds3 is the sphere around a bounding box.
func sphereOfBounds3(lo, hi lin.Vec3) (lin.Vec3, float32) {
	return lo.Add(hi).Mul(0.5), hi.Sub(lo).Len() / 2
}

// spheresMeet reports whether two spheres of combined radius whose
// centres start dd apart (squared) touch during a relative motion v
// (dv = d·v, vv = v·v), or already overlap.
func spheresMeet(dd, dv, vv, radius float32) bool {
	c := dd - radius*radius
	if c <= 0 {
		return true
	}
	if dv >= 0 || vv < 1e-12 {
		return false
	}
	disc := dv*dv - vv*c
	return disc >= 0 && (-dv-float32(math.Sqrt(float64(disc))))/vv < 1
}

// sweepParts sweeps convex pieces against one placed shape.
func sweepParts(parts []convexPart, shape Shape3, pos lin.Vec3, rot mat3, delta lin.Vec3) (float32, bool) {
	best, found := float32(math.Inf(1)), false
	for i := range parts {
		a := &parts[i].conv
		if m, ok := shape.(MeshShape); ok {
			if t, _, _, hit := sweepMesh(m, pos, rot, a, parts[i].lo, parts[i].hi, delta); hit && t < best {
				best, found = t, true
			}
			continue
		}
		for _, target := range convexParts(shape, pos, rot) {
			if t, _, _, hit := sweepConvex(a, &target.conv, delta); hit && t < best {
				best, found = t, true
			}
		}
	}
	return best, found
}

func inv(v float32) float32 {
	if v <= 0 {
		return 0
	}
	return 1 / v
}

// arbiter3 holds a pair's contacts through the solver iterations. It
// keeps the two bodies directly, because the solver reads their velocity
// and inverse inertia three times per contact per iteration.
type arbiter3 struct {
	ba, bb   *Body3 // nil for a static collider
	contacts []solverContact3
	friction float32
}

// nextArbiter extends the arbiter list by one, keeping the contact
// buffer the reused element already holds.
func (s *state3) nextArbiter() *arbiter3 {
	n := len(s.arbiters)
	if n < cap(s.arbiters) {
		s.arbiters = s.arbiters[:n+1]
	} else {
		s.arbiters = append(s.arbiters, arbiter3{})
	}
	return &s.arbiters[n]
}

type solverContact3 struct {
	rA, rB       lin.Vec3
	normal       lin.Vec3
	t1, t2       lin.Vec3
	massNormal   float32
	massT1       float32
	massT2       float32
	bias         float32
	pn, pt1, pt2 float32
}

// effectiveMass is 1 / (imA + imB + n·((IinvA (rA×n))×rA + (IinvB (rB×n))×rB)).
func effectiveMass(n, rA, rB lin.Vec3, ima float32, iia mat3, imb float32, iib mat3) float32 {
	ra := iia.mulVec(rA.Cross(n)).Cross(rA)
	rb := iib.mulVec(rB.Cross(n)).Cross(rB)
	k := ima + imb + n.Dot(ra.Add(rb))
	if k <= 0 {
		return 0
	}
	return 1 / k
}

// initArbiter3 prepares one pair's contacts in place, reusing whatever
// contact storage arb already holds.
func initArbiter3(arb *arbiter3, a, b *entry3, contacts []contact3, h float32) {
	arb.ba, arb.bb = a.b, b.b
	arb.contacts = arb.contacts[:0]
	var fa, fb, ra, rb float32 = 0.5, 0.5, 0, 0
	if a.b != nil {
		fa, ra = a.b.Friction, a.b.Restitution
	}
	if b.b != nil {
		fb, rb = b.b.Friction, b.b.Restitution
	}
	arb.friction = float32(math.Sqrt(float64(fa * fb)))
	restitution := max(ra, rb)
	var va, wa, vb, wb lin.Vec3
	var ima, imb float32
	var iia, iib mat3
	if a.b != nil {
		va, wa, ima, iia = a.b.Vel, a.b.AngVel, a.b.invMass, a.b.invInertia
	}
	if b.b != nil {
		vb, wb, imb, iib = b.b.Vel, b.b.AngVel, b.b.invMass, b.b.invInertia
	}
	for _, c := range contacts {
		sc := solverContact3{normal: c.normal}
		sc.rA = c.point.Sub(a.t.Position)
		sc.rB = c.point.Sub(b.t.Position)
		sc.massNormal = effectiveMass(c.normal, sc.rA, sc.rB, ima, iia, imb, iib)
		// Two tangents spanning the contact plane.
		ref := lin.V3(1, 0, 0)
		if math.Abs(float64(c.normal.X)) > 0.9 {
			ref = lin.V3(0, 1, 0)
		}
		sc.t1 = c.normal.Cross(ref).Norm()
		sc.t2 = c.normal.Cross(sc.t1)
		sc.massT1 = effectiveMass(sc.t1, sc.rA, sc.rB, ima, iia, imb, iib)
		sc.massT2 = effectiveMass(sc.t2, sc.rA, sc.rB, ima, iia, imb, iib)
		sc.bias = baumgarte / h * max(c.depth-slop, 0)
		dv := vb.Add(wb.Cross(sc.rB)).Sub(va).Sub(wa.Cross(sc.rA))
		if vn := dv.Dot(c.normal); vn < -restitutionThreshold {
			sc.bias += -restitution * vn
		}
		arb.contacts = append(arb.contacts, sc)
	}
}

func (arb *arbiter3) applyImpulse(c *solverContact3, p lin.Vec3) {
	if a := arb.ba; a != nil && a.invMass > 0 {
		a.Vel = a.Vel.Sub(p.Mul(a.invMass))
		a.AngVel = a.AngVel.Sub(a.invInertia.mulVec(c.rA.Cross(p)))
	}
	if b := arb.bb; b != nil && b.invMass > 0 {
		b.Vel = b.Vel.Add(p.Mul(b.invMass))
		b.AngVel = b.AngVel.Add(b.invInertia.mulVec(c.rB.Cross(p)))
	}
}

func (arb *arbiter3) relativeVelocity(c *solverContact3) lin.Vec3 {
	var v lin.Vec3
	if b := arb.bb; b != nil {
		v = b.Vel.Add(b.AngVel.Cross(c.rB))
	}
	if a := arb.ba; a != nil {
		v = v.Sub(a.Vel).Sub(a.AngVel.Cross(c.rA))
	}
	return v
}

func (arb *arbiter3) solve() {
	for i := range arb.contacts {
		c := &arb.contacts[i]
		dv := arb.relativeVelocity(c)
		vn := dv.Dot(c.normal)
		dpn := c.massNormal * (-vn + c.bias)
		pn0 := c.pn
		c.pn = max(pn0+dpn, 0)
		arb.applyImpulse(c, c.normal.Mul(c.pn-pn0))
		maxPt := arb.friction * c.pn
		dv = arb.relativeVelocity(c)
		dpt1 := c.massT1 * -dv.Dot(c.t1)
		pt10 := c.pt1
		c.pt1 = lin.Clamp(pt10+dpt1, -maxPt, maxPt)
		arb.applyImpulse(c, c.t1.Mul(c.pt1-pt10))
		dv = arb.relativeVelocity(c)
		dpt2 := c.massT2 * -dv.Dot(c.t2)
		pt20 := c.pt2
		c.pt2 = lin.Clamp(pt20+dpt2, -maxPt, maxPt)
		arb.applyImpulse(c, c.t2.Mul(c.pt2-pt20))
	}
}

// Hit3 is what a query found. Distance is the fraction along the ray or
// sweep for casts, the penetration depth for overlaps and the gap for
// Nearest3.
type Hit3 struct {
	Entity   ecs.Entity
	Point    lin.Vec3
	Normal   lin.Vec3
	Distance float32
}

// Raycast3 finds the nearest collider along the ray, ignoring triggers
// and colliders the mask excludes.
func Raycast3(w *ecs.World, r Ray3, mask uint32) (Hit3, bool) {
	return raycast3(w, r, mask, ecs.None)
}

func raycast3(w *ecs.World, r Ray3, mask uint32, exclude ecs.Entity) (Hit3, bool) {
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	stateOf3(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || c.Trigger || e == exclude || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		lo, hi := c.Shape.bounds(pos, rot)
		if !raySlab3(r, lo, hi, min(best.Distance, 1)) {
			return
		}
		if tt, n, ok := rayShape3(r, c.Shape, pos, rot); ok && tt < best.Distance {
			best = Hit3{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	})
	return best, found
}

// raySlab3 reports whether the ray reaches the box within maxT, as a
// cheap reject before the shape's own test. The ray's parameter runs
// from 0 to 1 over Dir, as the shape tests use it.
func raySlab3(r Ray3, lo, hi lin.Vec3, maxT float32) bool {
	tmin, tmax := float32(0), maxT
	return slabAxis(r.Origin.X, r.Dir.X, lo.X, hi.X, &tmin, &tmax) &&
		slabAxis(r.Origin.Y, r.Dir.Y, lo.Y, hi.Y, &tmin, &tmax) &&
		slabAxis(r.Origin.Z, r.Dir.Z, lo.Z, hi.Z, &tmin, &tmax)
}

func slabAxis(o, d, lo, hi float32, tmin, tmax *float32) bool {
	if d > -1e-20 && d < 1e-20 {
		return o >= lo && o <= hi
	}
	t1, t2 := (lo-o)/d, (hi-o)/d
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	*tmin = max(*tmin, t1)
	*tmax = min(*tmax, t2)
	return *tmin <= *tmax
}
