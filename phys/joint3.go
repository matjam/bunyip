package phys

import (
	"cmp"
	"math"
	"slices"

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
// axis, given in each body's frame; a zero axis means local Y. A side
// set to ecs.None fixes that anchor and axis in the world.
//
// The angle is how far B has turned about the axis relative to A since
// the first step, by the right-hand rule, in (-π, π]. MinAngle and
// MaxAngle limit it; both zero means unlimited. A motor drives the
// angle at MotorSpeed radians per second with up to MaxMotorTorque;
// zero torque means no motor.
type HingeJoint3 struct {
	A, B               ecs.Entity
	AnchorA, AnchorB   lin.Vec3
	AxisA, AxisB       lin.Vec3
	MinAngle, MaxAngle float32
	MotorSpeed         float32
	MaxMotorTorque     float32

	rel      lin.Quat // B's rotation in A's frame on the first step
	measured bool
}

// Angle is the hinge angle: how far B has turned about the axis
// relative to A since the first step, in radians.
func (j *HingeJoint3) Angle(w *ecs.World) float32 {
	qa, qb := entityQuat(w, j.A), entityQuat(w, j.B)
	j.measure(qa, qb)
	return hingeAngle(mat3FromQuat(qa), mat3FromQuat(qb), hingeAxis(j.AxisA), j.rel)
}

func (j *HingeJoint3) measure(qa, qb lin.Quat) {
	if !j.measured {
		j.rel, j.measured = conj(qa).Mul(qb), true
	}
}

// BallJoint3 pins two bodies at an anchor and lets them turn freely
// about it: a shoulder, a hip, a neck. AxisB is the limb's axis in B's
// frame (zero means local Y) and AxisA the centre of its cone in A's
// frame (zero means where AxisB pointed on the first step). ConeAngle
// limits how far AxisB may swing from AxisA and TwistAngle how far B
// may turn about AxisB either way, both in radians; zero means
// unlimited. A side set to ecs.None fixes that anchor and axis in the
// world.
type BallJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3
	AxisA, AxisB     lin.Vec3
	ConeAngle        float32
	TwistAngle       float32

	rel      lin.Quat // B's rotation in A's frame on the first step
	measured bool
}

// Angles returns the swing of AxisB away from AxisA and the twist of B
// about AxisB, in radians.
func (j *BallJoint3) Angles(w *ecs.World) (cone, twist float32) {
	qa, qb := entityQuat(w, j.A), entityQuat(w, j.B)
	j.measure(qa, qb)
	ra, rb := mat3FromQuat(qa), mat3FromQuat(qb)
	wa := ra.mulVec(j.AxisA).Norm()
	wb := rb.mulVec(hingeAxis(j.AxisB)).Norm()
	return coneAngle(wa, wb), twistAngle(qa, qb, hingeAxis(j.AxisB), j.rel)
}

func (j *BallJoint3) measure(qa, qb lin.Quat) {
	if !j.measured {
		j.rel, j.measured = conj(qa).Mul(qb), true
		if j.AxisA == (lin.Vec3{}) {
			j.AxisA = j.rel.Rotate(hingeAxis(j.AxisB)).Norm()
		}
	}
}

// entityQuat is an entity's rotation, or the identity for the world.
func entityQuat(w *ecs.World, e ecs.Entity) lin.Quat {
	if e == ecs.None {
		return lin.QuatIdentity()
	}
	t, _ := ecs.Get[gfx.Transform](w, e)
	return quatOf(t)
}

// hingeAxis applies the local Y default.
func hingeAxis(axis lin.Vec3) lin.Vec3 {
	if axis == (lin.Vec3{}) {
		return lin.V3(0, 1, 0)
	}
	return axis
}

// hingeAngle measures B's turn about A's axis relative to the rest
// rotation rel: the angle between a reference perpendicular to the
// axis carried by A and the same reference carried by B.
func hingeAngle(ra, rb mat3, axisA lin.Vec3, rel lin.Quat) float32 {
	n := ra.mulVec(axisA).Norm()
	pa := perpendicular(axisA)
	u := ra.mulVec(pa)
	v := rb.mulVec(conj(rel).Rotate(pa))
	return float32(math.Atan2(float64(n.Dot(u.Cross(v))), float64(u.Dot(v))))
}

// coneAngle is the angle between two world axes.
func coneAngle(wa, wb lin.Vec3) float32 {
	return float32(math.Acos(float64(lin.Clamp(wa.Dot(wb), -1, 1))))
}

// twistAngle is B's turn about its own axis relative to the rest
// rotation rel: the twist part of the swing-twist split of the extra
// rotation B has picked up, in B's frame.
func twistAngle(qa, qb lin.Quat, axisB lin.Vec3, rel lin.Quat) float32 {
	d := conj(rel).Mul(conj(qa).Mul(qb))
	if d.W < 0 {
		d = lin.Quat{X: -d.X, Y: -d.Y, Z: -d.Z, W: -d.W}
	}
	v := axisB.Norm()
	return 2 * float32(math.Atan2(float64(lin.V3(d.X, d.Y, d.Z).Dot(v)), float64(d.W)))
}

// PrismaticJoint3 is a slider: two bodies may only move along one axis
// relative to each other, and keep the rotation they had on the first
// step. Axis is the slide direction in A's frame and a zero axis means
// local X. AnchorA and AnchorB are in each body's frame; a side set to
// ecs.None fixes that anchor and the axis in the world.
//
// The translation is how far B's anchor sits from A's along the axis, so
// it is zero when the anchors meet. Min and Max limit it; both zero
// means unlimited. A motor drives the translation at MotorSpeed units
// per second with up to MaxMotorForce; zero force means no motor. A
// spring pulls the translation back toward zero with Stiffness as the
// force per unit of travel and Damping as the force per unit of speed;
// zero stiffness means no spring.
type PrismaticJoint3 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec3
	Axis             lin.Vec3
	Min, Max         float32
	MotorSpeed       float32
	MaxMotorForce    float32
	Stiffness        float32
	Damping          float32

	rel      lin.Quat // B's rotation in A's frame on the first step
	measured bool
}

// Translation is how far B's anchor has slid from A's along the axis, in
// world units.
func (j *PrismaticJoint3) Translation(w *ecs.World) float32 {
	a, _ := sideOf3(w, j.A)
	b, _ := sideOf3(w, j.B)
	pA, _ := a.anchor(j.AnchorA)
	pB, _ := b.anchor(j.AnchorB)
	return a.rot.mulVec(slideAxis3(j.Axis)).Norm().Dot(pB.Sub(pA))
}

func (j *PrismaticJoint3) measure(qa, qb lin.Quat) {
	if !j.measured {
		j.rel, j.measured = conj(qa).Mul(qb), true
	}
}

// slideAxis3 applies the local X default.
func slideAxis3(axis lin.Vec3) lin.Vec3 {
	if axis == (lin.Vec3{}) {
		return lin.V3(1, 0, 0)
	}
	return axis
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
	e    ecs.Entity
	t    *gfx.Transform
	b    *Body3 // the body when it takes part in the solve; nil when static or asleep
	body *Body3 // the body whether or not it takes part, to wake it
	rot  mat3
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
	if b, ok := ecs.Get[Body3](w, e); ok {
		s.body = b
		if !b.Sleeping && !b.asleep && !b.Kinematic && b.Mass > 0 {
			s.b = b
			s.invMass, s.invI = b.invMass, b.invInertia
		}
	}
	return s, true
}

// wakeAcross3 wakes a sleeping body joined to one that is awake, so a
// pushed body drags its joint partners with it as a contact would; the
// woken body joins the solve from the next step.
func wakeAcross3(a, b *jointSide3) {
	if a.b != nil && b.body != nil && b.body.asleep {
		b.body.Wake()
	}
	if b.b != nil && a.body != nil && a.body.asleep {
		a.body.Wake()
	}
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

// jointItem3 is one prepared joint waiting to be put in entity order.
// kind and at name the solver slice and the row in it, because the
// slices may still grow while the joints are being gathered.
type jointItem3 struct {
	id   uint64
	kind uint8
	at   int32
}

const (
	jointDistance3 = iota
	jointHinge3
	jointBall3
	jointPrismatic3
	jointSpring3
	jointFixed3
)

// gatherJoints3 collects every joint in a stable order (by entity id).
// The solvers are stored by value in the state's slices and the returned
// interfaces point into them, so a step allocates nothing once the
// slices have grown to fit.
func gatherJoints3(w *ecs.World, s *state3) []jointSolver3 {
	s.items = s.items[:0]
	s.distanceSolvers = s.distanceSolvers[:0]
	s.hingeSolvers = s.hingeSolvers[:0]
	s.ballSolvers = s.ballSolvers[:0]
	s.prismaticSolvers = s.prismaticSolvers[:0]
	s.springSolvers = s.springSolvers[:0]
	s.fixedSolvers = s.fixedSolvers[:0]
	s.distance.Each(func(e ecs.Entity, j *DistanceJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointDistance3, int32(len(s.distanceSolvers))})
			s.distanceSolvers = append(s.distanceSolvers, distanceSolver3{j: j, a: a, b: b})
		}
	})
	s.hinge.Each(func(e ecs.Entity, j *HingeJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointHinge3, int32(len(s.hingeSolvers))})
			s.hingeSolvers = append(s.hingeSolvers, hingeSolver3{j: j, a: a, b: b})
		}
	})
	s.ball.Each(func(e ecs.Entity, j *BallJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointBall3, int32(len(s.ballSolvers))})
			s.ballSolvers = append(s.ballSolvers, ballSolver3{j: j, a: a, b: b})
		}
	})
	s.prismatic.Each(func(e ecs.Entity, j *PrismaticJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointPrismatic3, int32(len(s.prismaticSolvers))})
			s.prismaticSolvers = append(s.prismaticSolvers, prismaticSolver3{j: j, a: a, b: b})
		}
	})
	s.spring.Each(func(e ecs.Entity, j *SpringJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointSpring3, int32(len(s.springSolvers))})
			s.springSolvers = append(s.springSolvers, springSolver3{j: j, a: a, b: b})
		}
	})
	s.fixed.Each(func(e ecs.Entity, j *FixedJoint3) {
		a, oka := sideOf3(w, j.A)
		b, okb := sideOf3(w, j.B)
		wakeAcross3(&a, &b)
		if oka && okb && (a.b != nil || b.b != nil) {
			s.items = append(s.items, jointItem3{e.ID(), jointFixed3, int32(len(s.fixedSolvers))})
			s.fixedSolvers = append(s.fixedSolvers, fixedSolver3{j: j, a: a, b: b})
		}
	})
	slices.SortFunc(s.items, func(a, b jointItem3) int { return cmp.Compare(a.id, b.id) })
	s.joints = s.joints[:0]
	for _, it := range s.items {
		switch it.kind {
		case jointDistance3:
			s.joints = append(s.joints, &s.distanceSolvers[it.at])
		case jointHinge3:
			s.joints = append(s.joints, &s.hingeSolvers[it.at])
		case jointBall3:
			s.joints = append(s.joints, &s.ballSolvers[it.at])
		case jointPrismatic3:
			s.joints = append(s.joints, &s.prismaticSolvers[it.at])
		case jointSpring3:
			s.joints = append(s.joints, &s.springSolvers[it.at])
		default:
			s.joints = append(s.joints, &s.fixedSolvers[it.at])
		}
	}
	return s.joints
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

// angularLimit3 is a one-sided angular constraint about a world axis,
// shared by hinge limits and a ball joint's cone and twist. With sign
// +1 the impulse may only turn B toward positive angles, with -1 toward
// negative, and with 0 either way (a locked joint).
type angularLimit3 struct {
	axis    lin.Vec3
	mass    float32
	bias    float32
	impulse float32
	sign    float32
	active  bool
}

// prepare sets the limit up with position error c (positive past an
// upper limit, negative past a lower one).
func (l *angularLimit3) prepare(a, b *jointSide3, axis lin.Vec3, c, sign, h float32) {
	k := axis.Dot(a.invI.add(b.invI).mulVec(axis))
	l.active = k > 1e-12
	if !l.active {
		return
	}
	l.axis, l.mass, l.bias, l.sign, l.impulse = axis, 1/k, jointBaumgarte/h*c, sign, 0
}

func (l *angularLimit3) solve(a, b *jointSide3) {
	if !l.active {
		return
	}
	cdot := l.axis.Dot(b.angVel().Sub(a.angVel()))
	old := l.impulse
	l.impulse -= l.mass * (cdot + l.bias)
	if l.sign > 0 {
		l.impulse = max(l.impulse, 0)
	} else if l.sign < 0 {
		l.impulse = min(l.impulse, 0)
	}
	p := l.axis.Mul(l.impulse - old)
	a.angularImpulse(p, -1)
	b.angularImpulse(p, 1)
}

// motor3 drives the relative angular speed about a world axis toward a
// target with a bounded accumulated impulse.
type motor3 struct {
	axis       lin.Vec3
	mass       float32
	speed      float32
	maxImpulse float32
	impulse    float32
	active     bool
}

func (m *motor3) prepare(a, b *jointSide3, axis lin.Vec3, speed, maxImpulse float32) {
	k := axis.Dot(a.invI.add(b.invI).mulVec(axis))
	m.active = maxImpulse > 0 && k > 1e-12
	if !m.active {
		return
	}
	m.axis, m.mass, m.speed, m.maxImpulse, m.impulse = axis, 1/k, speed, maxImpulse, 0
}

func (m *motor3) solve(a, b *jointSide3) {
	if !m.active {
		return
	}
	cdot := m.axis.Dot(b.angVel().Sub(a.angVel()))
	old := m.impulse
	m.impulse = lin.Clamp(old+m.mass*(m.speed-cdot), -m.maxImpulse, m.maxImpulse)
	p := m.axis.Mul(m.impulse - old)
	a.angularImpulse(p, -1)
	b.angularImpulse(p, 1)
}

// angularLock3 holds two bodies at the relative rotation rel, which is
// every rotation a weld or a slider forbids.
type angularLock3 struct {
	kinv mat3
	bias lin.Vec3
	ok   bool
}

func (l *angularLock3) prepare(a, b *jointSide3, rel lin.Quat, h float32) {
	qa, qb := quatOf(a.t), quatOf(b.t)
	// The rotation taking B's target orientation to its actual one.
	err := qb.Mul(conj(qa.Mul(rel)))
	if err.W < 0 {
		err = lin.Quat{X: -err.X, Y: -err.Y, Z: -err.Z, W: -err.W}
	}
	l.kinv, l.ok = a.invI.add(b.invI).inverse()
	l.bias = lin.V3(err.X, err.Y, err.Z).Mul(2 * jointBaumgarte / h)
}

func (l *angularLock3) solve(a, b *jointSide3) {
	if !l.ok {
		return
	}
	wrel := b.angVel().Sub(a.angVel())
	p := l.kinv.mulVec(wrel.Add(l.bias)).Neg()
	a.angularImpulse(p, -1)
	b.angularImpulse(p, 1)
}

// axialLimit3 is a one-sided constraint on the speed along a world axis,
// with the lever arm from each body to the axis. With sign +1 the
// impulse may only push the translation up, with -1 only down.
type axialLimit3 struct {
	mass    float32
	bias    float32
	impulse float32
	sign    float32
	active  bool
}

// prepare sets the limit up with position error c (positive past an
// upper limit, negative past a lower one) and the effective mass along
// the axis.
func (l *axialLimit3) prepare(mass, c, sign, h float32) {
	l.active = mass > 0
	if !l.active {
		return
	}
	l.mass, l.bias, l.sign, l.impulse = mass, jointBaumgarte/h*c, sign, 0
}

func (l *axialLimit3) solve(a, b *jointSide3, axis, rA, rB lin.Vec3) {
	if !l.active {
		return
	}
	cdot := axis.Dot(b.vel(rB).Sub(a.vel(rA)))
	old := l.impulse
	l.impulse -= l.mass * (cdot + l.bias)
	if l.sign > 0 {
		l.impulse = max(l.impulse, 0)
	} else if l.sign < 0 {
		l.impulse = min(l.impulse, 0)
	}
	p := axis.Mul(l.impulse - old)
	a.impulse(p, rA, -1)
	b.impulse(p, rB, 1)
}

// axialMotor3 drives the speed along a world axis toward a target with a
// bounded accumulated impulse.
type axialMotor3 struct {
	mass       float32
	speed      float32
	maxImpulse float32
	impulse    float32
	active     bool
}

func (m *axialMotor3) prepare(mass, speed, maxImpulse float32) {
	m.active = maxImpulse > 0 && mass > 0
	if !m.active {
		return
	}
	m.mass, m.speed, m.maxImpulse, m.impulse = mass, speed, maxImpulse, 0
}

func (m *axialMotor3) solve(a, b *jointSide3, axis, rA, rB lin.Vec3) {
	if !m.active {
		return
	}
	cdot := axis.Dot(b.vel(rB).Sub(a.vel(rA)))
	old := m.impulse
	m.impulse = lin.Clamp(old+m.mass*(m.speed-cdot), -m.maxImpulse, m.maxImpulse)
	p := axis.Mul(m.impulse - old)
	a.impulse(p, rA, -1)
	b.impulse(p, rB, 1)
}

// prismaticSolver3 locks the relative rotation, holds the two directions
// across the axis with a coupled two by two system, and leaves the axis
// to the motor, the limits and the spring.
type prismaticSolver3 struct {
	j    *PrismaticJoint3
	a, b jointSide3
	// The lever arms: A's runs from its centre to B's anchor so the axis
	// is carried by A, B's to its own anchor.
	dA, rB       lin.Vec3
	axis, u1, u2 lin.Vec3
	kinv         [4]float32
	bias         [2]float32
	ok           bool
	ang          angularLock3
	motor        axialMotor3
	lower, upper axialLimit3
}

func (s *prismaticSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *prismaticSolver3) prepare(h float32) {
	j := s.j
	pA, rA := s.a.anchor(j.AnchorA)
	pB, rB := s.b.anchor(j.AnchorB)
	d := pB.Sub(pA)
	s.dA, s.rB = d.Add(rA), rB
	s.axis = s.a.rot.mulVec(slideAxis3(j.Axis)).Norm()
	s.u1 = perpendicular(s.axis)
	s.u2 = s.axis.Cross(s.u1)
	j.measure(quatOf(s.a.t), quatOf(s.b.t))
	s.ang.prepare(&s.a, &s.b, j.rel, h)
	// The two directions across the axis, coupled through the inertias.
	m := s.a.invMass + s.b.invMass
	a1, b1 := s.dA.Cross(s.u1), s.rB.Cross(s.u1)
	a2, b2 := s.dA.Cross(s.u2), s.rB.Cross(s.u2)
	ia, ib := s.a.invI, s.b.invI
	k := [4]float32{
		m + a1.Dot(ia.mulVec(a1)) + b1.Dot(ib.mulVec(b1)), a1.Dot(ia.mulVec(a2)) + b1.Dot(ib.mulVec(b2)),
		a2.Dot(ia.mulVec(a1)) + b2.Dot(ib.mulVec(b1)), m + a2.Dot(ia.mulVec(a2)) + b2.Dot(ib.mulVec(b2)),
	}
	det := k[0]*k[3] - k[1]*k[2]
	s.ok = abs32(det) > 1e-12
	if s.ok {
		s.kinv = [4]float32{k[3] / det, -k[1] / det, -k[2] / det, k[0] / det}
	}
	s.bias = [2]float32{jointBaumgarte / h * s.u1.Dot(d), jointBaumgarte / h * s.u2.Dot(d)}
	// The motor, the limits and the spring act along the axis.
	aa, ba := s.dA.Cross(s.axis), s.rB.Cross(s.axis)
	var axialMass float32
	if ka := m + aa.Dot(ia.mulVec(aa)) + ba.Dot(ib.mulVec(ba)); ka > 0 {
		axialMass = 1 / ka
	}
	translation := s.axis.Dot(d)
	s.motor.prepare(axialMass, j.MotorSpeed, j.MaxMotorForce*h)
	s.lower.active, s.upper.active = false, false
	if j.Min != 0 || j.Max != 0 {
		if translation <= j.Min {
			s.lower.prepare(axialMass, translation-j.Min, 1, h)
		}
		if translation >= j.Max {
			s.upper.prepare(axialMass, translation-j.Max, -1, h)
		}
	}
	if j.Stiffness > 0 {
		v := s.axis.Dot(s.b.vel(s.rB).Sub(s.a.vel(s.dA)))
		p := s.axis.Mul((-j.Stiffness*translation - j.Damping*v) * h)
		s.a.impulse(p, s.dA, -1)
		s.b.impulse(p, s.rB, 1)
	}
}

func (s *prismaticSolver3) solve() {
	s.motor.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.lower.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.upper.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.ang.solve(&s.a, &s.b)
	if !s.ok {
		return
	}
	rel := s.b.vel(s.rB).Sub(s.a.vel(s.dA))
	c0, c1 := s.u1.Dot(rel)+s.bias[0], s.u2.Dot(rel)+s.bias[1]
	l0 := -(s.kinv[0]*c0 + s.kinv[1]*c1)
	l1 := -(s.kinv[2]*c0 + s.kinv[3]*c1)
	p := s.u1.Mul(l0).Add(s.u2.Mul(l1))
	s.a.impulse(p, s.dA, -1)
	s.b.impulse(p, s.rB, 1)
}

type hingeSolver3 struct {
	j            *HingeJoint3
	a, b         jointSide3
	point        pointSolver3
	b1, b2       lin.Vec3
	k2inv        [4]float32
	bias         [2]float32
	ok           bool
	motor        motor3
	lower, upper angularLimit3
}

func (s *hingeSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *hingeSolver3) prepare(h float32) {
	j := s.j
	s.point.prepare(&s.a, &s.b, j.AnchorA, j.AnchorB, h)
	axisA, axisB := hingeAxis(j.AxisA), hingeAxis(j.AxisB)
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
	// The motor and the limits act about the axis.
	j.measure(quatOf(s.a.t), quatOf(s.b.t))
	angle := hingeAngle(s.a.rot, s.b.rot, axisA, j.rel)
	s.motor.prepare(&s.a, &s.b, wa, j.MotorSpeed, j.MaxMotorTorque*h)
	s.lower.active, s.upper.active = false, false
	if j.MinAngle != 0 || j.MaxAngle != 0 {
		if angle <= j.MinAngle {
			s.lower.prepare(&s.a, &s.b, wa, angle-j.MinAngle, 1, h)
		}
		if angle >= j.MaxAngle {
			s.upper.prepare(&s.a, &s.b, wa, angle-j.MaxAngle, -1, h)
		}
	}
}

func (s *hingeSolver3) solve() {
	s.motor.solve(&s.a, &s.b)
	s.lower.solve(&s.a, &s.b)
	s.upper.solve(&s.a, &s.b)
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

type ballSolver3 struct {
	j           *BallJoint3
	a, b        jointSide3
	point       pointSolver3
	cone, twist angularLimit3
}

func (s *ballSolver3) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *ballSolver3) prepare(h float32) {
	j := s.j
	s.point.prepare(&s.a, &s.b, j.AnchorA, j.AnchorB, h)
	qa, qb := quatOf(s.a.t), quatOf(s.b.t)
	j.measure(qa, qb)
	axisB := hingeAxis(j.AxisB)
	wa := s.a.rot.mulVec(j.AxisA).Norm()
	wb := s.b.rot.mulVec(axisB).Norm()
	s.cone.active, s.twist.active = false, false
	if cone := coneAngle(wa, wb); j.ConeAngle > 0 && cone > j.ConeAngle {
		// Turning B about wa × wb swings it further from the centre.
		n := wa.Cross(wb).Norm()
		if n == (lin.Vec3{}) {
			n = perpendicular(wa)
		}
		s.cone.prepare(&s.a, &s.b, n, cone-j.ConeAngle, -1, h)
	}
	if j.TwistAngle > 0 {
		switch twist := twistAngle(qa, qb, axisB, j.rel); {
		case twist > j.TwistAngle:
			s.twist.prepare(&s.a, &s.b, wb, twist-j.TwistAngle, -1, h)
		case twist < -j.TwistAngle:
			s.twist.prepare(&s.a, &s.b, wb, twist+j.TwistAngle, 1, h)
		}
	}
}

func (s *ballSolver3) solve() {
	s.cone.solve(&s.a, &s.b)
	s.twist.solve(&s.a, &s.b)
	s.point.solve(&s.a, &s.b)
}

type fixedSolver3 struct {
	j     *FixedJoint3
	a, b  jointSide3
	point pointSolver3
	ang   angularLock3
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
	if !s.j.measured {
		s.j.rel, s.j.measured = conj(quatOf(s.a.t)).Mul(quatOf(s.b.t)), true
	}
	s.ang.prepare(&s.a, &s.b, s.j.rel, h)
}

func (s *fixedSolver3) solve() {
	s.ang.solve(&s.a, &s.b)
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
