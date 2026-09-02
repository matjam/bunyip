package phys

import (
	"math"
	"sort"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// DistanceJoint2 keeps two anchors a fixed distance apart, like a rod,
// or within a range, like a rope. AnchorA and AnchorB are in each
// body's frame; with B set to ecs.None, AnchorB is a point in the
// world. Length zero measures the distance on the first step. When Max
// is above zero the joint only acts outside [Min, Max].
type DistanceJoint2 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec2
	Length           float32
	Min, Max         float32

	measured bool
}

// RevoluteJoint2 pins two bodies together at an anchor so they turn
// freely about it. A side set to ecs.None fixes that anchor in the
// world.
//
// The angle is how far B has turned relative to A since the first
// step, anticlockwise positive, in (-π, π]. MinAngle and MaxAngle
// limit it; both zero means unlimited. A motor drives the angle at
// MotorSpeed radians per second with up to MaxMotorTorque; zero torque
// means no motor.
type RevoluteJoint2 struct {
	A, B               ecs.Entity
	AnchorA, AnchorB   lin.Vec2
	MinAngle, MaxAngle float32
	MotorSpeed         float32
	MaxMotorTorque     float32

	rel      float32 // B's rotation less A's on the first step
	measured bool
}

// Angle is the joint angle: how far B has turned relative to A since
// the first step, in radians.
func (j *RevoluteJoint2) Angle(w *ecs.World) float32 {
	ra, rb := entityRot2(w, j.A), entityRot2(w, j.B)
	j.measure(ra, rb)
	return wrapAngle(rb - ra - j.rel)
}

func (j *RevoluteJoint2) measure(ra, rb float32) {
	if !j.measured {
		j.rel, j.measured = rb-ra, true
	}
}

// entityRot2 is an entity's rotation, or zero for the world.
func entityRot2(w *ecs.World, e ecs.Entity) float32 {
	if e == ecs.None {
		return 0
	}
	if t, ok := ecs.Get[gfx.Transform2](w, e); ok {
		return t.Rotation
	}
	return 0
}

// SpringJoint2 pulls two anchors toward a rest length with a damped
// spring force. RestLength zero measures it on the first step; zero
// Stiffness means 10 and Damping is the velocity coefficient.
type SpringJoint2 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec2
	RestLength       float32
	Stiffness        float32
	Damping          float32

	measured bool
}

// FixedJoint2 welds two bodies together at an anchor, keeping the angle
// between them as it was on the first step.
type FixedJoint2 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec2

	rel      float32
	measured bool
}

// jointSide2 is one end of a joint: the transform and body, or the
// world when the entity is ecs.None.
type jointSide2 struct {
	e       ecs.Entity
	t       *gfx.Transform2
	b       *Body2
	pos     lin.Vec2
	rot     float32
	invMass float32
	invI    float32
}

func sideOf2(w *ecs.World, e ecs.Entity) (jointSide2, bool) {
	s := jointSide2{e: e}
	if e == ecs.None {
		return s, true
	}
	t, ok := ecs.Get[gfx.Transform2](w, e)
	if !ok {
		return s, false
	}
	s.t, s.pos, s.rot = t, t.Position, t.Rotation
	if b, ok := ecs.Get[Body2](w, e); ok && !b.Sleeping && !b.asleep && !b.Kinematic && b.Mass > 0 {
		s.b = b
		s.invMass, s.invI = b.invMass, b.invInertia
	}
	return s, true
}

func (s *jointSide2) anchor(local lin.Vec2) (p, r lin.Vec2) {
	if s.t == nil {
		return local, lin.Vec2{}
	}
	c, sn := cosSin(s.rot)
	r = rotate2(local, c, sn)
	return s.pos.Add(r), r
}

func (s *jointSide2) vel(r lin.Vec2) lin.Vec2 {
	if s.b == nil {
		return lin.Vec2{}
	}
	return s.b.Vel.Add(crossSV(s.b.AngVel, r))
}

func (s *jointSide2) angVel() float32 {
	if s.b == nil {
		return 0
	}
	return s.b.AngVel
}

func (s *jointSide2) impulse(p, r lin.Vec2, sign float32) {
	if s.b == nil {
		return
	}
	s.b.Vel = s.b.Vel.Add(p.Mul(sign * s.invMass))
	s.b.AngVel += sign * s.invI * cross2(r, p)
}

func (s *jointSide2) angularImpulse(l, sign float32) {
	if s.b == nil {
		return
	}
	s.b.AngVel += sign * s.invI * l
}

type jointSolver2 interface {
	prepare(h float32)
	solve()
	sides() (ecs.Entity, ecs.Entity)
}

// gatherJoints2 collects every joint in a stable order (by entity id).
func gatherJoints2(w *ecs.World, s *state2) []jointSolver2 {
	type item struct {
		id uint64
		j  jointSolver2
	}
	var items []item
	add := func(e ecs.Entity, ea, eb ecs.Entity, mk func(a, b jointSide2) jointSolver2) {
		a, oka := sideOf2(w, ea)
		b, okb := sideOf2(w, eb)
		if oka && okb && (a.b != nil || b.b != nil) {
			items = append(items, item{e.ID(), mk(a, b)})
		}
	}
	s.distance.Each(func(e ecs.Entity, j *DistanceJoint2) {
		add(e, j.A, j.B, func(a, b jointSide2) jointSolver2 { return &distanceSolver2{j: j, a: a, b: b} })
	})
	s.revolute.Each(func(e ecs.Entity, j *RevoluteJoint2) {
		add(e, j.A, j.B, func(a, b jointSide2) jointSolver2 { return &revoluteSolver2{j: j, a: a, b: b} })
	})
	s.spring.Each(func(e ecs.Entity, j *SpringJoint2) {
		add(e, j.A, j.B, func(a, b jointSide2) jointSolver2 { return &springSolver2{j: j, a: a, b: b} })
	})
	s.fixed.Each(func(e ecs.Entity, j *FixedJoint2) {
		add(e, j.A, j.B, func(a, b jointSide2) jointSolver2 { return &fixedSolver2{j: j, a: a, b: b} })
	})
	sort.Slice(items, func(i, k int) bool { return items[i].id < items[k].id })
	out := make([]jointSolver2, len(items))
	for i, it := range items {
		out[i] = it.j
	}
	return out
}

type distanceSolver2 struct {
	j       *DistanceJoint2
	a, b    jointSide2
	rA, rB  lin.Vec2
	n       lin.Vec2
	mass    float32
	bias    float32
	impulse float32
	mode    int
}

func (s *distanceSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *distanceSolver2) prepare(h float32) {
	pA, rA := s.a.anchor(s.j.AnchorA)
	pB, rB := s.b.anchor(s.j.AnchorB)
	s.rA, s.rB = rA, rB
	d := pB.Sub(pA)
	dist := d.Len()
	s.n = lin.V2(0, 1)
	if dist > 1e-6 {
		s.n = d.Mul(1 / dist)
	}
	j := s.j
	if j.Length == 0 && j.Max == 0 && !j.measured {
		j.Length, j.measured = dist, true
	}
	var c float32
	switch {
	case j.Max > 0 && dist > j.Max:
		c, s.mode = dist-j.Max, 1
	case j.Max > 0 && dist < j.Min:
		c, s.mode = dist-j.Min, -1
	case j.Max > 0:
		s.mode = 2
	default:
		c, s.mode = dist-j.Length, 0
	}
	rnA, rnB := cross2(rA, s.n), cross2(rB, s.n)
	k := s.a.invMass + s.b.invMass + s.a.invI*rnA*rnA + s.b.invI*rnB*rnB
	if k > 0 {
		s.mass = 1 / k
	}
	s.bias = jointBaumgarte / h * c
	s.impulse = 0
}

func (s *distanceSolver2) solve() {
	if s.mode == 2 || s.mass == 0 {
		return
	}
	vn := s.n.Dot(s.b.vel(s.rB).Sub(s.a.vel(s.rA)))
	lambda := -s.mass * (vn + s.bias)
	old := s.impulse
	s.impulse += lambda
	if s.mode == 1 {
		s.impulse = min(s.impulse, 0)
	} else if s.mode == -1 {
		s.impulse = max(s.impulse, 0)
	}
	p := s.n.Mul(s.impulse - old)
	s.a.impulse(p, s.rA, -1)
	s.b.impulse(p, s.rB, 1)
}

// pointSolver2 keeps two anchors together.
type pointSolver2 struct {
	rA, rB lin.Vec2
	kinv   [4]float32
	bias   lin.Vec2
	ok     bool
}

func (p *pointSolver2) prepare(a, b *jointSide2, anchorA, anchorB lin.Vec2, h float32) {
	pA, rA := a.anchor(anchorA)
	pB, rB := b.anchor(anchorB)
	p.rA, p.rB = rA, rB
	m := a.invMass + b.invMass
	k := [4]float32{
		m + a.invI*rA.Y*rA.Y + b.invI*rB.Y*rB.Y, -a.invI*rA.X*rA.Y - b.invI*rB.X*rB.Y,
		-a.invI*rA.X*rA.Y - b.invI*rB.X*rB.Y, m + a.invI*rA.X*rA.X + b.invI*rB.X*rB.X,
	}
	det := k[0]*k[3] - k[1]*k[2]
	p.ok = abs32(det) > 1e-12
	if p.ok {
		p.kinv = [4]float32{k[3] / det, -k[1] / det, -k[2] / det, k[0] / det}
	}
	p.bias = pB.Sub(pA).Mul(jointBaumgarte / h)
}

func (p *pointSolver2) solve(a, b *jointSide2) {
	if !p.ok {
		return
	}
	c := b.vel(p.rB).Sub(a.vel(p.rA)).Add(p.bias)
	lambda := lin.V2(-(p.kinv[0]*c.X + p.kinv[1]*c.Y), -(p.kinv[2]*c.X + p.kinv[3]*c.Y))
	a.impulse(lambda, p.rA, -1)
	b.impulse(lambda, p.rB, 1)
}

// angularLimit2 is a one-sided angular constraint. With sign +1 the
// impulse may only turn B anticlockwise relative to A, with -1
// clockwise, and with 0 either way.
type angularLimit2 struct {
	mass    float32
	bias    float32
	impulse float32
	sign    float32
	active  bool
}

// prepare sets the limit up with position error c (positive past an
// upper limit, negative past a lower one).
func (l *angularLimit2) prepare(a, b *jointSide2, c, sign, h float32) {
	k := a.invI + b.invI
	l.active = k > 1e-12
	if !l.active {
		return
	}
	l.mass, l.bias, l.sign, l.impulse = 1/k, jointBaumgarte/h*c, sign, 0
}

func (l *angularLimit2) solve(a, b *jointSide2) {
	if !l.active {
		return
	}
	cdot := b.angVel() - a.angVel()
	old := l.impulse
	l.impulse -= l.mass * (cdot + l.bias)
	if l.sign > 0 {
		l.impulse = max(l.impulse, 0)
	} else if l.sign < 0 {
		l.impulse = min(l.impulse, 0)
	}
	a.angularImpulse(l.impulse-old, -1)
	b.angularImpulse(l.impulse-old, 1)
}

// motor2 drives the relative angular speed toward a target with a
// bounded accumulated impulse.
type motor2 struct {
	mass       float32
	speed      float32
	maxImpulse float32
	impulse    float32
	active     bool
}

func (m *motor2) prepare(a, b *jointSide2, speed, maxImpulse float32) {
	k := a.invI + b.invI
	m.active = maxImpulse > 0 && k > 1e-12
	if !m.active {
		return
	}
	m.mass, m.speed, m.maxImpulse, m.impulse = 1/k, speed, maxImpulse, 0
}

func (m *motor2) solve(a, b *jointSide2) {
	if !m.active {
		return
	}
	cdot := b.angVel() - a.angVel()
	old := m.impulse
	m.impulse = lin.Clamp(old+m.mass*(m.speed-cdot), -m.maxImpulse, m.maxImpulse)
	a.angularImpulse(m.impulse-old, -1)
	b.angularImpulse(m.impulse-old, 1)
}

type revoluteSolver2 struct {
	j            *RevoluteJoint2
	a, b         jointSide2
	point        pointSolver2
	motor        motor2
	lower, upper angularLimit2
}

func (s *revoluteSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *revoluteSolver2) prepare(h float32) {
	j := s.j
	s.point.prepare(&s.a, &s.b, j.AnchorA, j.AnchorB, h)
	j.measure(s.a.rot, s.b.rot)
	angle := wrapAngle(s.b.rot - s.a.rot - j.rel)
	s.motor.prepare(&s.a, &s.b, j.MotorSpeed, j.MaxMotorTorque*h)
	s.lower.active, s.upper.active = false, false
	if j.MinAngle != 0 || j.MaxAngle != 0 {
		if angle <= j.MinAngle {
			s.lower.prepare(&s.a, &s.b, angle-j.MinAngle, 1, h)
		}
		if angle >= j.MaxAngle {
			s.upper.prepare(&s.a, &s.b, angle-j.MaxAngle, -1, h)
		}
	}
}

func (s *revoluteSolver2) solve() {
	s.motor.solve(&s.a, &s.b)
	s.lower.solve(&s.a, &s.b)
	s.upper.solve(&s.a, &s.b)
	s.point.solve(&s.a, &s.b)
}

type fixedSolver2 struct {
	j     *FixedJoint2
	a, b  jointSide2
	point pointSolver2
	mass  float32
	bias  float32
}

func (s *fixedSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *fixedSolver2) prepare(h float32) {
	s.point.prepare(&s.a, &s.b, s.j.AnchorA, s.j.AnchorB, h)
	if !s.j.measured {
		s.j.rel, s.j.measured = s.b.rot-s.a.rot, true
	}
	err := wrapAngle(s.b.rot - s.a.rot - s.j.rel)
	k := s.a.invI + s.b.invI
	s.mass = 0
	if k > 0 {
		s.mass = 1 / k
	}
	s.bias = jointBaumgarte / h * err
}

func (s *fixedSolver2) solve() {
	if s.mass > 0 {
		wrel := s.b.angVel() - s.a.angVel()
		l := -s.mass * (wrel + s.bias)
		s.a.angularImpulse(l, -1)
		s.b.angularImpulse(l, 1)
	}
	s.point.solve(&s.a, &s.b)
}

// wrapAngle brings an angle into (-π, π].
func wrapAngle(a float32) float32 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

type springSolver2 struct {
	j    *SpringJoint2
	a, b jointSide2
}

func (s *springSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *springSolver2) prepare(h float32) {
	pA, rA := s.a.anchor(s.j.AnchorA)
	pB, rB := s.b.anchor(s.j.AnchorB)
	d := pB.Sub(pA)
	dist := d.Len()
	if dist < 1e-6 {
		return
	}
	n := d.Mul(1 / dist)
	j := s.j
	if j.RestLength == 0 && !j.measured {
		j.RestLength, j.measured = dist, true
	}
	k := j.Stiffness
	if k == 0 {
		k = 10
	}
	vn := n.Dot(s.b.vel(rB).Sub(s.a.vel(rA)))
	force := -k*(dist-j.RestLength) - j.Damping*vn
	p := n.Mul(force * h)
	s.a.impulse(p, rA, -1)
	s.b.impulse(p, rB, 1)
}

func (s *springSolver2) solve() {}
