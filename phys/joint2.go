package phys

import (
	"cmp"
	"math"
	"slices"

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
// step or first Angle call, positive from +X toward +Y (clockwise on
// screen), in (-π, π]. MinAngle and MaxAngle
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
// the reference pose, in radians. The first call captures that pose if
// the solver has not measured it yet.
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

// PrismaticJoint2 is a slider: two bodies may only move along one axis
// relative to each other, and keep the angle they had on the first step.
// Axis is the slide direction in A's frame and a zero axis means local
// X. AnchorA and AnchorB are in each body's frame; a side set to
// ecs.None fixes that anchor and the axis in the world.
//
// The translation is how far B's anchor sits from A's along the axis, so
// it is zero when the anchors meet. Min and Max limit it; both zero
// means unlimited. A motor drives the translation at MotorSpeed units
// per second with up to MaxMotorForce; zero force means no motor. A
// spring pulls the translation back toward zero with Stiffness as the
// force per unit of travel and Damping as the force per unit of speed;
// zero stiffness means no spring.
type PrismaticJoint2 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec2
	Axis             lin.Vec2
	Min, Max         float32
	MotorSpeed       float32
	MaxMotorForce    float32
	Stiffness        float32
	Damping          float32

	rel      float32 // B's rotation less A's on the first step
	measured bool
}

// Translation is how far B's anchor has slid from A's along the axis, in
// world units.
func (j *PrismaticJoint2) Translation(w *ecs.World) float32 {
	return slideTranslation2(w, j.A, j.B, j.AnchorA, j.AnchorB, j.Axis, lin.V2(1, 0))
}

// WheelJoint2 is a wheel on a suspension: B may spin freely and slide
// along one axis of A, and is held on that line in every other
// direction. A is the chassis and B the wheel. Axis is the suspension
// direction in A's frame and a zero axis means local Y. AnchorA and
// AnchorB are in each body's frame, so AnchorA is where the wheel sits
// when the suspension is at rest; a side set to ecs.None fixes that
// anchor and the axis in the world.
//
// The spring along the axis is tuned by Frequency in hertz and
// DampingRatio, where 1 is critically damped; zero frequency means no
// spring and zero DampingRatio means 0.7. Min and Max limit the travel
// along the axis; both zero means unlimited. A motor drives B's spin at
// MotorSpeed radians per second with up to MaxMotorTorque; zero torque
// means no motor.
type WheelJoint2 struct {
	A, B             ecs.Entity
	AnchorA, AnchorB lin.Vec2
	Axis             lin.Vec2
	Frequency        float32
	DampingRatio     float32
	Min, Max         float32
	MotorSpeed       float32
	MaxMotorTorque   float32
}

// Translation is how far the suspension has moved from its rest
// position, in world units. It is negative while the spring is
// compressed toward A.
func (j *WheelJoint2) Translation(w *ecs.World) float32 {
	return slideTranslation2(w, j.A, j.B, j.AnchorA, j.AnchorB, j.Axis, lin.V2(0, 1))
}

// slideTranslation2 measures the gap between two anchors along an axis
// carried by A.
func slideTranslation2(w *ecs.World, ea, eb ecs.Entity, anchorA, anchorB, axis, def lin.Vec2) float32 {
	a, _ := sideOf2(w, ea)
	b, _ := sideOf2(w, eb)
	pA, _ := a.anchor(anchorA)
	pB, _ := b.anchor(anchorB)
	c, s := cosSin(a.rot)
	return rotate2(unitAxis2(axis, def), c, s).Dot(pB.Sub(pA))
}

// unitAxis2 normalises an axis, falling back to def for a zero one.
func unitAxis2(axis, def lin.Vec2) lin.Vec2 {
	if n := axis.Norm(); n != (lin.Vec2{}) {
		return n
	}
	return def
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
	b       *Body2 // the body when it takes part in the solve; nil when static or asleep
	body    *Body2 // the body whether or not it takes part, to wake it
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
	if b, ok := ecs.Get[Body2](w, e); ok {
		s.body = b
		if !b.Sleeping && !b.asleep && !b.Kinematic && b.Mass > 0 {
			s.b = b
			s.invMass, s.invI = b.invMass, b.invInertia
		}
	}
	return s, true
}

// wakeAcross2 wakes a sleeping body joined to one that is awake, so a
// pushed body drags its joint partners with it as a contact would; the
// woken body joins the solve from the next step.
func wakeAcross2(a, b *jointSide2) {
	if a.b != nil && b.body != nil && b.body.asleep {
		b.body.Wake()
	}
	if b.b != nil && a.body != nil && a.body.asleep {
		a.body.Wake()
	}
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

// jointItem2 is one prepared joint waiting to be put in entity order.
// kind and at name the solver slice and the row in it, because the
// slices may still grow while the joints are being gathered.
type jointItem2 struct {
	id   uint64
	kind uint8
	at   int32
}

const (
	jointDistance2 = iota
	jointRevolute2
	jointPrismatic2
	jointWheel2
	jointSpring2
	jointFixed2
)

// gatherJoints2 collects every joint in a stable order (by entity id).
// The solvers are stored by value in the state's slices and the returned
// interfaces point into them, so a step allocates nothing once the
// slices have grown to fit.
func gatherJoints2(w *ecs.World, s *state2) []jointSolver2 {
	s.items = s.items[:0]
	s.distanceSolvers = s.distanceSolvers[:0]
	s.revoluteSolvers = s.revoluteSolvers[:0]
	s.prismaticSolvers = s.prismaticSolvers[:0]
	s.wheelSolvers = s.wheelSolvers[:0]
	s.springSolvers = s.springSolvers[:0]
	s.fixedSolvers = s.fixedSolvers[:0]
	sides := func(ea, eb ecs.Entity) (jointSide2, jointSide2, bool) {
		a, oka := sideOf2(w, ea)
		b, okb := sideOf2(w, eb)
		wakeAcross2(&a, &b)
		return a, b, oka && okb && (a.b != nil || b.b != nil)
	}
	s.distance.Each(func(e ecs.Entity, j *DistanceJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointDistance2, int32(len(s.distanceSolvers))})
			s.distanceSolvers = append(s.distanceSolvers, distanceSolver2{j: j, a: a, b: b})
		}
	})
	s.revolute.Each(func(e ecs.Entity, j *RevoluteJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointRevolute2, int32(len(s.revoluteSolvers))})
			s.revoluteSolvers = append(s.revoluteSolvers, revoluteSolver2{j: j, a: a, b: b})
		}
	})
	s.prismatic.Each(func(e ecs.Entity, j *PrismaticJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointPrismatic2, int32(len(s.prismaticSolvers))})
			s.prismaticSolvers = append(s.prismaticSolvers, prismaticSolver2{j: j, a: a, b: b})
		}
	})
	s.wheel.Each(func(e ecs.Entity, j *WheelJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointWheel2, int32(len(s.wheelSolvers))})
			s.wheelSolvers = append(s.wheelSolvers, wheelSolver2{j: j, a: a, b: b})
		}
	})
	s.spring.Each(func(e ecs.Entity, j *SpringJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointSpring2, int32(len(s.springSolvers))})
			s.springSolvers = append(s.springSolvers, springSolver2{j: j, a: a, b: b})
		}
	})
	s.fixed.Each(func(e ecs.Entity, j *FixedJoint2) {
		if a, b, ok := sides(j.A, j.B); ok {
			s.items = append(s.items, jointItem2{e.ID(), jointFixed2, int32(len(s.fixedSolvers))})
			s.fixedSolvers = append(s.fixedSolvers, fixedSolver2{j: j, a: a, b: b})
		}
	})
	slices.SortFunc(s.items, func(a, b jointItem2) int { return cmp.Compare(a.id, b.id) })
	s.joints = s.joints[:0]
	for _, it := range s.items {
		switch it.kind {
		case jointDistance2:
			s.joints = append(s.joints, &s.distanceSolvers[it.at])
		case jointRevolute2:
			s.joints = append(s.joints, &s.revoluteSolvers[it.at])
		case jointPrismatic2:
			s.joints = append(s.joints, &s.prismaticSolvers[it.at])
		case jointWheel2:
			s.joints = append(s.joints, &s.wheelSolvers[it.at])
		case jointSpring2:
			s.joints = append(s.joints, &s.springSolvers[it.at])
		default:
			s.joints = append(s.joints, &s.fixedSolvers[it.at])
		}
	}
	return s.joints
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

// axialLimit2 is a one-sided constraint on the speed along a world axis,
// with the lever arm from each body to the axis. With sign +1 the
// impulse may only push the translation up, with -1 only down.
type axialLimit2 struct {
	mass    float32
	bias    float32
	impulse float32
	sign    float32
	active  bool
}

// prepare sets the limit up with position error c (positive past an
// upper limit, negative past a lower one) and the effective mass along
// the axis.
func (l *axialLimit2) prepare(mass, c, sign, h float32) {
	l.active = mass > 0
	if !l.active {
		return
	}
	l.mass, l.bias, l.sign, l.impulse = mass, jointBaumgarte/h*c, sign, 0
}

func (l *axialLimit2) solve(a, b *jointSide2, axis, rA, rB lin.Vec2) {
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

// axialMotor2 drives the speed along a world axis toward a target with a
// bounded accumulated impulse.
type axialMotor2 struct {
	mass       float32
	speed      float32
	maxImpulse float32
	impulse    float32
	active     bool
}

func (m *axialMotor2) prepare(mass, speed, maxImpulse float32) {
	m.active = maxImpulse > 0 && mass > 0
	if !m.active {
		return
	}
	m.mass, m.speed, m.maxImpulse, m.impulse = mass, speed, maxImpulse, 0
}

func (m *axialMotor2) solve(a, b *jointSide2, axis, rA, rB lin.Vec2) {
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

// axialSpring2 pulls the translation along a world axis back toward zero
// as a soft constraint tuned by frequency and damping ratio, so the
// response is the same whatever the masses are.
type axialSpring2 struct {
	mass    float32
	bias    float32
	gamma   float32
	impulse float32
	active  bool
}

// prepare takes the inverse effective mass along the axis and the
// current translation c.
func (s *axialSpring2) prepare(invMass, c, frequency, ratio, h float32) {
	s.active = false
	if frequency <= 0 || invMass <= 0 {
		return
	}
	if ratio <= 0 {
		ratio = 0.7
	}
	omega := 2 * float32(math.Pi) * frequency
	m := 1 / invMass
	k := m * omega * omega
	d := 2 * m * ratio * omega
	s.gamma = h * (d + h*k)
	if s.gamma > 0 {
		s.gamma = 1 / s.gamma
	}
	s.bias = c * h * k * s.gamma
	if total := invMass + s.gamma; total > 0 {
		s.mass, s.impulse, s.active = 1/total, 0, true
	}
}

func (s *axialSpring2) solve(a, b *jointSide2, axis, rA, rB lin.Vec2) {
	if !s.active {
		return
	}
	cdot := axis.Dot(b.vel(rB).Sub(a.vel(rA)))
	l := -s.mass * (cdot + s.bias + s.gamma*s.impulse)
	s.impulse += l
	p := axis.Mul(l)
	a.impulse(p, rA, -1)
	b.impulse(p, rB, 1)
}

// prismaticSolver2 holds the perpendicular offset and the relative angle
// at zero with a coupled two by two system, and leaves the axis to the
// motor, the limits and the spring.
type prismaticSolver2 struct {
	j    *PrismaticJoint2
	a, b jointSide2
	// The lever arms: A's runs from its centre to B's anchor so the axis
	// is carried by A, B's to its own anchor.
	dA, rB       lin.Vec2
	axis, perp   lin.Vec2
	kinv         [4]float32
	bias         [2]float32
	ok           bool
	motor        axialMotor2
	lower, upper axialLimit2
}

func (s *prismaticSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *prismaticSolver2) prepare(h float32) {
	j := s.j
	pA, rA := s.a.anchor(j.AnchorA)
	pB, rB := s.b.anchor(j.AnchorB)
	d := pB.Sub(pA)
	c, sn := cosSin(s.a.rot)
	s.axis = rotate2(unitAxis2(j.Axis, lin.V2(1, 0)), c, sn)
	s.perp = lin.V2(-s.axis.Y, s.axis.X)
	s.dA, s.rB = d.Add(rA), rB
	if !j.measured {
		j.rel, j.measured = s.b.rot-s.a.rot, true
	}
	mA, mB, iA, iB := s.a.invMass, s.b.invMass, s.a.invI, s.b.invI
	s1, s2 := cross2(s.dA, s.perp), cross2(s.rB, s.perp)
	k11 := mA + mB + iA*s1*s1 + iB*s2*s2
	k12 := iA*s1 + iB*s2
	k22 := iA + iB
	if k22 == 0 {
		// Two bodies that cannot turn still need a solvable system.
		k22 = 1
	}
	det := k11*k22 - k12*k12
	s.ok = abs32(det) > 1e-12
	if s.ok {
		s.kinv = [4]float32{k22 / det, -k12 / det, -k12 / det, k11 / det}
	}
	angle := wrapAngle(s.b.rot - s.a.rot - j.rel)
	s.bias = [2]float32{jointBaumgarte / h * s.perp.Dot(d), jointBaumgarte / h * angle}
	// The motor, the limits and the spring act along the axis.
	a1, a2 := cross2(s.dA, s.axis), cross2(s.rB, s.axis)
	var axialMass float32
	if k := mA + mB + iA*a1*a1 + iB*a2*a2; k > 0 {
		axialMass = 1 / k
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

func (s *prismaticSolver2) solve() {
	s.motor.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.lower.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.upper.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	if !s.ok {
		return
	}
	c0 := s.perp.Dot(s.b.vel(s.rB).Sub(s.a.vel(s.dA))) + s.bias[0]
	c1 := s.b.angVel() - s.a.angVel() + s.bias[1]
	l0 := -(s.kinv[0]*c0 + s.kinv[1]*c1)
	l1 := -(s.kinv[2]*c0 + s.kinv[3]*c1)
	p := s.perp.Mul(l0)
	s.a.impulse(p, s.dA, -1)
	s.b.impulse(p, s.rB, 1)
	s.a.angularImpulse(l1, -1)
	s.b.angularImpulse(l1, 1)
}

// wheelSolver2 holds the wheel on the suspension line, leaves its spin
// free and gives the axis to the spring, the limits and the spin motor.
type wheelSolver2 struct {
	j            *WheelJoint2
	a, b         jointSide2
	dA, rB       lin.Vec2
	axis, perp   lin.Vec2
	perpMass     float32
	perpBias     float32
	spring       axialSpring2
	motor        motor2
	lower, upper axialLimit2
}

func (s *wheelSolver2) sides() (ecs.Entity, ecs.Entity) { return s.j.A, s.j.B }

func (s *wheelSolver2) prepare(h float32) {
	j := s.j
	pA, rA := s.a.anchor(j.AnchorA)
	pB, rB := s.b.anchor(j.AnchorB)
	d := pB.Sub(pA)
	c, sn := cosSin(s.a.rot)
	s.axis = rotate2(unitAxis2(j.Axis, lin.V2(0, 1)), c, sn)
	s.perp = lin.V2(-s.axis.Y, s.axis.X)
	s.dA, s.rB = d.Add(rA), rB
	mA, mB, iA, iB := s.a.invMass, s.b.invMass, s.a.invI, s.b.invI
	s1, s2 := cross2(s.dA, s.perp), cross2(s.rB, s.perp)
	s.perpMass = 0
	if k := mA + mB + iA*s1*s1 + iB*s2*s2; k > 0 {
		s.perpMass = 1 / k
	}
	s.perpBias = jointBaumgarte / h * s.perp.Dot(d)
	a1, a2 := cross2(s.dA, s.axis), cross2(s.rB, s.axis)
	axialInv := mA + mB + iA*a1*a1 + iB*a2*a2
	var axialMass float32
	if axialInv > 0 {
		axialMass = 1 / axialInv
	}
	translation := s.axis.Dot(d)
	s.spring.prepare(axialInv, translation, j.Frequency, j.DampingRatio, h)
	s.motor.prepare(&s.a, &s.b, j.MotorSpeed, j.MaxMotorTorque*h)
	s.lower.active, s.upper.active = false, false
	if j.Min != 0 || j.Max != 0 {
		if translation <= j.Min {
			s.lower.prepare(axialMass, translation-j.Min, 1, h)
		}
		if translation >= j.Max {
			s.upper.prepare(axialMass, translation-j.Max, -1, h)
		}
	}
}

func (s *wheelSolver2) solve() {
	s.spring.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.motor.solve(&s.a, &s.b)
	s.lower.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	s.upper.solve(&s.a, &s.b, s.axis, s.dA, s.rB)
	if s.perpMass > 0 {
		cdot := s.perp.Dot(s.b.vel(s.rB).Sub(s.a.vel(s.dA))) + s.perpBias
		p := s.perp.Mul(-s.perpMass * cdot)
		s.a.impulse(p, s.dA, -1)
		s.b.impulse(p, s.rB, 1)
	}
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
	// One remainder rather than a loop, which a rotation that has wound
	// up over many turns would spin through once per turn.
	a = float32(math.Remainder(float64(a), 2*math.Pi))
	if a <= -math.Pi {
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
