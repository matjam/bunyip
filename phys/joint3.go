package phys

import (
	"sort"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// jointBaumgarte is the fraction of a joint's position error corrected
// per step.
const jointBaumgarte = 0.2

// DistanceJoint3 keeps two anchors a fixed distance apart, like a rod,
// or within a range, like a rope. AnchorA and AnchorB are in each
// body's frame; with B set to ecs.None, AnchorB is a point in the
// world. Length zero measures the distance on the first step. When Max
// is above zero the joint only acts outside [Min, Max].
type DistanceJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3
	Length           float32
	Min, Max         float32

	measured bool
}

// HingeJoint3 pins two bodies at an anchor and lets them turn about one
// axis, given in each body's frame; a zero axis means local Y. With B
// set to ecs.None the hinge is fixed to the world at AnchorB.
type HingeJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3
	AxisA, AxisB     lin.Vec3
}

// SpringJoint3 pulls two anchors toward a rest length with a damped
// spring force. RestLength zero measures it on the first step; zero
// Stiffness means 10 and Damping is the velocity coefficient.
type SpringJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3
	RestLength       float32
	Stiffness        float32
	Damping          float32

	measured bool
}

// FixedJoint3 welds two bodies together at an anchor, keeping the
// rotation between them as it was on the first step.
type FixedJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3

	rel      lin.Quat
	measured bool
}

// jointSide3 is one end of a joint: the transform and body, or the
// world when the entity is ecs.None.
type jointSide3 struct {
	e   ecs.Entity
	t   *gfx.Transform
	b   *Body3
	rot mat3
	// Position, rotation, inverse mass and inertia in the world.
	pos     lin.Vec3
	invMass float32
	invI    mat3
}

func sideOf3(w *ecs.World, e ecs.Entity) (jointSide3, bool) {
	s := jointSide3{e: e, rot: mat3FromQuat(lin.QuatIdentity())}
	if e == ecs.None {
		return s, true
	}
	t, ok := ecs.Get[gfx.Transform](w, e)
	if !ok {
		return s, false
	}
	s.t = t
	s.pos = t.Position
	s.rot = mat3FromQuat(t.Rotation)
	if b, ok := ecs.Get[Body3](w, e); ok && !b.Sleeping && !b.asleep && !b.Kinematic && b.Mass > 0 {
		s.b = b
		s.invMass, s.invI = b.invMass, b.invInertia
	}
	return s, true
}

// anchor places a local anchor in the world and returns the lever arm.
func (s *jointSide3) anchor(local lin.Vec3) (p, r lin.Vec3) {
	if s.t == nil {
		return local, lin.Vec3{}
	}
	r = s.rot.mulVec(local)
	return s.pos.Add(r), r
}

func (s *jointSide3) vel(r lin.Vec3) lin.Vec3 {
	if s.b == nil {
		return lin.Vec3{}
	}
	return s.b.Vel.Add(s.b.AngVel.Cross(r))
}

func (s *jointSide3) angVel() lin.Vec3 {
	if s.b == nil {
		return lin.Vec3{}
	}
	return s.b.AngVel
}

// impulse applies p at lever arm r; sign -1 for the A side.
func (s *jointSide3) impulse(p, r lin.Vec3, sign float32) {
	if s.b == nil {
		return
	}
	s.b.Vel = s.b.Vel.Add(p.Mul(sign * s.invMass))
	s.b.AngVel = s.b.AngVel.Add(s.invI.mulVec(r.Cross(p)).Mul(sign))
}

func (s *jointSide3) angularImpulse(l lin.Vec3, sign float32) {
	if s.b == nil {
		return
	}
	s.b.AngVel = s.b.AngVel.Add(s.invI.mulVec(l).Mul(sign))
}

// jointSolver3 is one joint prepared for a step.
type jointSolver3 interface {
	prepare(h float32)
	solve()
	sides() (ecs.Entity, ecs.Entity)
}

// gatherJoints3 collects every joint in a stable order (by entity id).
func gatherJoints3(w *ecs.World, s *state3) []jointSolver3 {
	type item struct {
		id uint64
		j  jointSolver3
	}
	var items []item
	s.distance.Each(func(e ecs.Entity, j *DistanceJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		if oka && okb && (a.b != nil || b.b != nil) {
			items = append(items, item{e.ID(), &distanceSolver3{j: j, a: a, b: b}})
		}
	})
	s.hinge.Each(func(e ecs.Entity, j *HingeJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		if oka && okb && (a.b != nil || b.b != nil) {
			items = append(items, item{e.ID(), &hingeSolver3{j: j, a: a, b: b}})
		}
	})
	s.spring.Each(func(e ecs.Entity, j *SpringJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		if oka && okb && (a.b != nil || b.b != nil) {
			items = append(items, item{e.ID(), &springSolver3{j: j, a: a, b: b}})
		}
	})
	s.fixed.Each(func(e ecs.Entity, j *FixedJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		if oka && okb && (a.b != nil || b.b != nil) {
			items = append(items, item{e.ID(), &fixedSolver3{j: j, a: a, b: b}})
		}
	})
	sort.Slice(items, func(i, k int) bool { return items[i].id < items[k].id })
	out := make([]jointSolver3, len(items))
	for i, it := range items {
		out[i] = it.j
	}
	return out
}

type distanceSolver3 struct {
	j       *DistanceJoint3
	a, b    jointSide3
	rA, rB  lin.Vec3
	n       lin.Vec3
	mass    float32
	bias    float32
	impulse float32
	mode    int // 0 rod, 1 may only pull together, -1 may only push apart, 2 inactive
}

func (s *distanceSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *distanceSolver3) prepare(h float32) {
	pA, rA := s.a.anchor(s.j.AnchorA)
	pB, rB := s.b.anchor(s.j.AnchorB)
	s.rA, s.rB = rA, rB
	d := pB.Sub(pA)
	dist := d.Len()
	s.n = lin.V3(0, 1, 0)
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
	s.mass = effectiveMass(s.n, rA, rB, s.a.invMass, s.a.invI, s.b.invMass, s.b.invI)
	s.bias = jointBaumgarte / h * c
	s.impulse = 0
}

func (s *distanceSolver3) solve() {
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

// pointSolver3 is the shared part of hinges and welds: the anchors must
// coincide.
type pointSolver3 struct {
	rA, rB lin.Vec3
	kinv   mat3
	bias   lin.Vec3
	ok     bool
}

func (p *pointSolver3) prepare(a, b *jointSide3, anchorA, anchorB lin.Vec3, h float32) {
	pA, rA := a.anchor(anchorA)
	pB, rB := b.anchor(anchorB)
	p.rA, p.rB = rA, rB
	k := diag3(a.invMass+b.invMass, a.invMass+b.invMass, a.invMass+b.invMass)
	sa, sb := skew(rA), skew(rB)
	k = k.sub(sa.mul(a.invI).mul(sa)).sub(sb.mul(b.invI).mul(sb))
	p.kinv, p.ok = k.inverse()
	p.bias = pB.Sub(pA).Mul(jointBaumgarte / h)
}

func (p *pointSolver3) solve(a, b *jointSide3) {
	if !p.ok {
		return
	}
	cdot := b.vel(p.rB).Sub(a.vel(p.rA))
	lambda := p.kinv.mulVec(cdot.Add(p.bias)).Neg()
	a.impulse(lambda, p.rA, -1)
	b.impulse(lambda, p.rB, 1)
}

type hingeSolver3 struct {
	j      *HingeJoint3
	a, b   jointSide3
	point  pointSolver3
	b1, b2 lin.Vec3
	k2inv  [4]float32
	bias   [2]float32
	ok     bool
}

func (s *hingeSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *hingeSolver3) prepare(h float32) {
	s.point.prepare(&s.a, &s.b, s.j.AnchorA, s.j.AnchorB, h)
	axisA, axisB := s.j.AxisA, s.j.AxisB
	if axisA == (lin.Vec3{}) {
		axisA = lin.V3(0, 1, 0)
	}
	if axisB == (lin.Vec3{}) {
		axisB = lin.V3(0, 1, 0)
	}
	wa := s.a.rot.mulVec(axisA).Norm()
	wb := s.b.rot.mulVec(axisB).Norm()
	s.b1 = perpendicular(wa)
	s.b2 = wa.Cross(s.b1)
	sum := s.a.invI.add(s.b.invI)
	k := [4]float32{
		s.b1.Dot(sum.mulVec(s.b1)), s.b1.Dot(sum.mulVec(s.b2)),
		s.b2.Dot(sum.mulVec(s.b1)), s.b2.Dot(sum.mulVec(s.b2)),
	}
	det := k[0]*k[3] - k[1]*k[2]
	s.ok = abs32(det) > 1e-12
	if s.ok {
		s.k2inv = [4]float32{k[3] / det, -k[1] / det, -k[2] / det, k[0] / det}
	}
	e := wa.Cross(wb)
	s.bias = [2]float32{jointBaumgarte / h * s.b1.Dot(e), jointBaumgarte / h * s.b2.Dot(e)}
}

func (s *hingeSolver3) solve() {
	if s.ok {
		wrel := s.b.angVel().Sub(s.a.angVel())
		c0, c1 := s.b1.Dot(wrel)+s.bias[0], s.b2.Dot(wrel)+s.bias[1]
		l0 := -(s.k2inv[0]*c0 + s.k2inv[1]*c1)
		l1 := -(s.k2inv[2]*c0 + s.k2inv[3]*c1)
		l := s.b1.Mul(l0).Add(s.b2.Mul(l1))
		s.a.angularImpulse(l, -1)
		s.b.angularImpulse(l, 1)
	}
	s.point.solve(&s.a, &s.b)
}

type fixedSolver3 struct {
	j     *FixedJoint3
	a, b  jointSide3
	point pointSolver3
	kinv  mat3
	bias  lin.Vec3
	ok    bool
}

func (s *fixedSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func quatOf(t *gfx.Transform) lin.Quat {
	if t == nil || t.Rotation == (lin.Quat{}) {
		return lin.QuatIdentity()
	}
	return t.Rotation
}

func conj(q lin.Quat) lin.Quat { return lin.Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W} }

func (s *fixedSolver3) prepare(h float32) {
	s.point.prepare(&s.a, &s.b, s.j.AnchorA, s.j.AnchorB, h)
	qa, qb := quatOf(s.a.t), quatOf(s.b.t)
	if !s.j.measured {
		s.j.rel, s.j.measured = conj(qa).Mul(qb), true
	}
	// The rotation taking B's target orientation to its actual one.
	target := qa.Mul(s.j.rel)
	err := qb.Mul(conj(target))
	if err.W < 0 {
		err = lin.Quat{X: -err.X, Y: -err.Y, Z: -err.Z, W: -err.W}
	}
	e := lin.V3(err.X, err.Y, err.Z).Mul(2)
	s.kinv, s.ok = s.a.invI.add(s.b.invI).inverse()
	s.bias = e.Mul(jointBaumgarte / h)
}

func (s *fixedSolver3) solve() {
	if s.ok {
		wrel := s.b.angVel().Sub(s.a.angVel())
		l := s.kinv.mulVec(wrel.Add(s.bias)).Neg()
		s.a.angularImpulse(l, -1)
		s.b.angularImpulse(l, 1)
	}
	s.point.solve(&s.a, &s.b)
}

type springSolver3 struct {
	j    *SpringJoint3
	a, b jointSide3
}

func (s *springSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *springSolver3) prepare(h float32) {
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

func (s *springSolver3) solve() {}

// skew returns the matrix S with S·x = v × x.
func skew(v lin.Vec3) mat3 {
	return mat3{0, -v.Z, v.Y, v.Z, 0, -v.X, -v.Y, v.X, 0}
}

func (m mat3) add(n mat3) mat3 {
	var o mat3
	for i := range m {
		o[i] = m[i] + n[i]
	}
	return o
}

func (m mat3) sub(n mat3) mat3 {
	var o mat3
	for i := range m {
		o[i] = m[i] - n[i]
	}
	return o
}

// inverse returns the inverse, or false for a singular matrix.
func (m mat3) inverse() (mat3, bool) {
	a, b, c := m[0], m[1], m[2]
	d, e, f := m[3], m[4], m[5]
	g, h, i := m[6], m[7], m[8]
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if abs32(det) < 1e-12 {
		return mat3{}, false
	}
	inv := 1 / det
	return mat3{
		(e*i - f*h) * inv, (c*h - b*i) * inv, (b*f - c*e) * inv,
		(f*g - d*i) * inv, (a*i - c*g) * inv, (c*d - a*f) * inv,
		(d*h - e*g) * inv, (b*g - a*h) * inv, (a*e - b*d) * inv,
	}, true
}
