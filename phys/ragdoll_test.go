package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestHingeMotor3D spins a wheel up to its target speed on a motorised
// world hinge, then shows a weak motor cannot exceed its torque.
func TestHingeMotor3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{})
	w.AddSystem("phys", System3)
	wheel := w.SpawnWith(gfx.At(0, 2, 0), Dynamic3(2), Collider3{Shape: Sphere{0.5}})
	hinge := w.SpawnWith(HingeJoint3{A: ecs.None, AnchorA: lin.V3(0, 2, 0), B: wheel, AxisA: lin.V3(1, 0, 0), AxisB: lin.V3(1, 0, 0), MotorSpeed: 10, MaxMotorTorque: 50})
	run(w, 2)
	b, _ := w.Get[Body3](wheel)
	if !near(b.AngVel.X, 10, 0.1) || abs32(b.AngVel.Y) > 0.01 || abs32(b.AngVel.Z) > 0.01 {
		t.Errorf("motorised wheel spins at %v, want (10, 0, 0)", b.AngVel)
	}
	j, _ := w.Get[HingeJoint3](hinge)
	if a := j.Angle(w); abs32(a) > math.Pi {
		t.Errorf("hinge angle %.2f out of range", a)
	}
	// A heavy wheel on a weak motor: inertia 0.4·10·1 = 4, torque 1, so
	// after a second it turns at 0.25 radians per second.
	w = ecs.NewWorld()
	w.SetResource(Settings3{})
	w.AddSystem("phys", System3)
	heavy := w.SpawnWith(gfx.At(0, 2, 0), Dynamic3(10), Collider3{Shape: Sphere{1}})
	w.SpawnWith(HingeJoint3{A: ecs.None, AnchorA: lin.V3(0, 2, 0), B: heavy, AxisA: lin.V3(1, 0, 0), AxisB: lin.V3(1, 0, 0), MotorSpeed: 10, MaxMotorTorque: 1})
	run(w, 1)
	b, _ = w.Get[Body3](heavy)
	if !near(b.AngVel.X, 0.25, 0.03) {
		t.Errorf("heavy wheel spins at %v, want 0.25 with 1 unit of torque", b.AngVel)
	}
}

// TestHingeLimit3D hangs a bar from a limited hinge: gravity swings it
// down until the lower limit catches it.
func TestHingeLimit3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	body := Dynamic3(1)
	body.AngularDamping = 1
	bar := w.SpawnWith(gfx.At(0.5, 5, 0), body, Collider3{Shape: Box3{Half: lin.V3(0.5, 0.05, 0.05)}})
	hinge := w.SpawnWith(HingeJoint3{A: ecs.None, AnchorA: lin.V3(0, 5, 0), B: bar, AnchorB: lin.V3(-0.5, 0, 0),
		AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1), MinAngle: -0.5, MaxAngle: 0.5})
	j, _ := w.Get[HingeJoint3](hinge)
	lowest := float32(0)
	for range 240 {
		w.Update(step)
		lowest = min(lowest, j.Angle(w))
	}
	if lowest < -0.6 {
		t.Errorf("bar swung past the limit to %.2f", lowest)
	}
	if a := j.Angle(w); !near(a, -0.5, 0.05) {
		t.Errorf("bar rests at %.2f, want -0.5", a)
	}
	bt, _ := w.Get[gfx.Transform](bar)
	if wantY := 5 + 0.5*float32(math.Sin(-0.5)); !near(bt.Position.Y, wantY, 0.05) {
		t.Errorf("bar centre at %v, want y %.2f", bt.Position, wantY)
	}
	// Both limits zero means unlimited: the same bar swings well past.
	w = ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	free := w.SpawnWith(gfx.At(0.5, 5, 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.05, 0.05)}})
	fh := w.SpawnWith(HingeJoint3{A: ecs.None, AnchorA: lin.V3(0, 5, 0), B: free, AnchorB: lin.V3(-0.5, 0, 0), AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1)})
	fj, _ := w.Get[HingeJoint3](fh)
	lowest = 0
	for range 60 {
		w.Update(step)
		lowest = min(lowest, fj.Angle(w))
	}
	if lowest > -1 {
		t.Errorf("unlimited bar only reached %.2f", lowest)
	}
}

// TestRevoluteMotorAndLimit2D is the wheel and the hanging bar in 2D.
func TestRevoluteMotorAndLimit2D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings2{})
	w.AddSystem("phys", System2)
	wheel := w.SpawnWith(gfx.At2(0, 2), Dynamic2(2), Collider2{Shape: Circle{0.5}})
	w.SpawnWith(RevoluteJoint2{A: ecs.None, AnchorA: lin.V2(0, 2), B: wheel, MotorSpeed: 10, MaxMotorTorque: 50})
	heavy := w.SpawnWith(gfx.At2(5, 2), Dynamic2(10), Collider2{Shape: Circle{1}})
	w.SpawnWith(RevoluteJoint2{A: ecs.None, AnchorA: lin.V2(5, 2), B: heavy, MotorSpeed: 10, MaxMotorTorque: 1})
	run(w, 1)
	b, _ := w.Get[Body2](wheel)
	if !near(b.AngVel, 10, 0.1) {
		t.Errorf("motorised wheel spins at %.2f, want 10", b.AngVel)
	}
	// A disc of mass 10 and radius 1 has inertia 5: torque 1 for a second gives 0.2.
	hb, _ := w.Get[Body2](heavy)
	if !near(hb.AngVel, 0.2, 0.03) {
		t.Errorf("heavy wheel spins at %.2f, want 0.2", hb.AngVel)
	}
	w = ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	body := Dynamic2(1)
	body.AngularDamping = 1
	bar := w.SpawnWith(gfx.At2(0.5, 5), body, Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.05}})
	pin := w.SpawnWith(RevoluteJoint2{A: ecs.None, AnchorA: lin.V2(0, 5), B: bar, AnchorB: lin.V2(-0.5, 0), MinAngle: -0.5, MaxAngle: 0.5})
	j, _ := w.Get[RevoluteJoint2](pin)
	lowest := float32(0)
	for range 240 {
		w.Update(step)
		lowest = min(lowest, j.Angle(w))
	}
	if lowest < -0.6 {
		t.Errorf("bar swung past the limit to %.2f", lowest)
	}
	if a := j.Angle(w); !near(a, -0.5, 0.05) {
		t.Errorf("bar rests at %.2f, want -0.5", a)
	}
}

// TestBallJoint3D hangs a limb from a ball joint with a cone and a
// twist limit and checks it stays inside both.
func TestBallJoint3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	body := Dynamic3(1)
	body.AngularDamping = 1
	// The limb starts pointing sideways along +X, which is the centre of
	// its cone; gravity swings it down until the cone stops it.
	limb := w.SpawnWith(gfx.Transform{Position: lin.V3(0.5, 5, 0), Rotation: lin.AxisAngle(lin.V3(0, 0, 1), -math.Pi/2)}, body, Collider3{Shape: Capsule{Radius: 0.05, HalfHeight: 0.45}})
	je := w.SpawnWith(BallJoint3{A: ecs.None, AnchorA: lin.V3(0, 5, 0), B: limb, AnchorB: lin.V3(0, 0.5, 0), AxisA: lin.V3(1, 0, 0), ConeAngle: 0.6, TwistAngle: 0.3})
	j, _ := w.Get[BallJoint3](je)
	var worstCone, worstTwist float32
	for range 240 {
		w.Update(step)
		c, tw := j.Angles(w)
		worstCone, worstTwist = max(worstCone, c), max(worstTwist, abs32(tw))
	}
	if worstCone > 0.75 {
		t.Errorf("limb swung %.2f from the cone axis, limit 0.6", worstCone)
	}
	if worstTwist > 0.45 {
		t.Errorf("limb twisted %.2f, limit 0.3", worstTwist)
	}
	c, _ := j.Angles(w)
	if !near(c, 0.6, 0.06) {
		t.Errorf("limb rests %.2f from the cone axis, want 0.6", c)
	}
	lt, _ := w.Get[gfx.Transform](limb)
	if d := lt.Position.Add(lt.Rotation.Rotate(lin.V3(0, 0.5, 0))).Sub(lin.V3(0, 5, 0)).Len(); d > 0.03 {
		t.Errorf("ball joint separated by %.3f", d)
	}
	// Spin the limb about its axis: the twist limit holds it.
	b, _ := w.Get[Body3](limb)
	b.AngVel = lt.Rotation.Rotate(lin.V3(0, 20, 0))
	worstTwist = 0
	for range 120 {
		w.Update(step)
		_, tw := j.Angles(w)
		worstTwist = max(worstTwist, abs32(tw))
	}
	if worstTwist > 0.5 {
		t.Errorf("spun limb twisted %.2f, limit 0.3", worstTwist)
	}
}

// TestRagdoll3D drops a ragdoll onto the ground: it settles with every
// joint together and every limit respected, and Pose moves it whole.
func TestRagdoll3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	r := NewRagdoll3(w, RagdollSpec{Position: lin.V3(0, 2, 0), Rotation: lin.AxisAngle(lin.V3(1, 0, 0), 0.3)})
	if len(r.Parts) != len(RagdollParts) || len(r.Joints) != len(RagdollParts)-1 {
		t.Fatalf("ragdoll has %d parts and %d joints", len(r.Parts), len(r.Joints))
	}
	head, _ := w.Get[gfx.Transform](r.Parts[RagdollHead])
	if !near(head.Position.Y, 2+1.68, 0.1) {
		t.Errorf("head starts at %v, want y 3.68", head.Position)
	}
	var worstGap, worstOver float32
	check := func() {
		for name, je := range r.Joints {
			var a, b ecs.Entity
			var anchorA, anchorB lin.Vec3
			if h, ok := w.Get[HingeJoint3](je); ok {
				a, b, anchorA, anchorB = h.A, h.B, h.AnchorA, h.AnchorB
				angle := h.Angle(w)
				worstOver = max(worstOver, h.MinAngle-angle, angle-h.MaxAngle)
			} else if bj, ok := w.Get[BallJoint3](je); ok {
				a, b, anchorA, anchorB = bj.A, bj.B, bj.AnchorA, bj.AnchorB
				cone, twist := bj.Angles(w)
				worstOver = max(worstOver, cone-bj.ConeAngle, abs32(twist)-bj.TwistAngle)
			} else {
				t.Fatalf("joint %s has no joint component", name)
			}
			ta, _ := w.Get[gfx.Transform](a)
			tb, _ := w.Get[gfx.Transform](b)
			pa := ta.Position.Add(ta.Rotation.Rotate(anchorA))
			pb := tb.Position.Add(tb.Rotation.Rotate(anchorB))
			worstGap = max(worstGap, pa.Sub(pb).Len())
		}
	}
	for range 240 {
		w.Update(step)
		check()
	}
	if worstGap > 0.05 {
		t.Errorf("a joint separated by %.3f", worstGap)
	}
	if worstOver > 0.15 {
		t.Errorf("a joint went %.2f past its limit", worstOver)
	}
	for _, name := range RagdollParts {
		tr, _ := w.Get[gfx.Transform](r.Parts[name])
		b, _ := w.Get[Body3](r.Parts[name])
		bone := r.Bones[name]
		if tr.Position.Y < 0.5+bone.Radius-0.05 {
			t.Errorf("%s sank to %v", name, tr.Position)
		}
		if b.Vel.Len() > 0.3 {
			t.Errorf("%s is still moving at %v", name, b.Vel)
		}
	}
	// Pose lifts the pelvis and spine back up; the rest follow next update.
	pos := map[string]lin.Vec3{RagdollPelvis: lin.V3(3, 5, 0)}
	rot := map[string]lin.Quat{RagdollPelvis: lin.QuatIdentity()}
	r.Pose(w, pos, rot)
	pt, _ := w.Get[gfx.Transform](r.Parts[RagdollPelvis])
	pb, _ := w.Get[Body3](r.Parts[RagdollPelvis])
	if pt.Position != lin.V3(3, 5, 0) || pb.Vel != (lin.Vec3{}) || pb.Asleep() {
		t.Errorf("posed pelvis at %v vel %v", pt.Position, pb.Vel)
	}
	if n := len(r.Entities()); n != 2*len(RagdollParts)-1 {
		t.Errorf("ragdoll lists %d entities", n)
	}
	r.Despawn(w)
	if _, ok := w.Get[gfx.Transform](r.Parts[RagdollHead]); ok {
		t.Error("despawned ragdoll still has a head")
	}
}

// TestDynamicCCD fires two fast spheres at each other: with CCD they
// meet, without it they pass through.
func TestDynamicCCD(t *testing.T) {
	for _, ccd := range []bool{true, false} {
		w := ecs.NewWorld()
		w.SetResource(Settings3{})
		w.AddSystem("phys", System3)
		left, right := Dynamic3(1), Dynamic3(1)
		left.Vel, right.Vel = lin.V3(100, 0, 0), lin.V3(-100, 0, 0)
		left.CCD, right.CCD = ccd, ccd
		left.Restitution, right.Restitution = 1, 1
		a := w.SpawnWith(gfx.At(-5, 0, 0), left, Collider3{Shape: Sphere{0.1}})
		b := w.SpawnWith(gfx.At(5, 0, 0), right, Collider3{Shape: Sphere{0.1}})
		run(w, 0.5)
		ta, _ := w.Get[gfx.Transform](a)
		tb, _ := w.Get[gfx.Transform](b)
		if ccd && ta.Position.X > tb.Position.X {
			t.Errorf("3D: CCD spheres passed through each other: %v %v", ta.Position, tb.Position)
		}
		if !ccd && ta.Position.X < tb.Position.X {
			t.Errorf("3D: spheres without CCD should tunnel: %v %v", ta.Position, tb.Position)
		}
		w2 := ecs.NewWorld()
		w2.AddSystem("phys", System2)
		l2, r2 := Dynamic2(1), Dynamic2(1)
		l2.Vel, r2.Vel = lin.V2(100, 0), lin.V2(-100, 0)
		l2.CCD, r2.CCD = ccd, ccd
		a2 := w2.SpawnWith(gfx.At2(-5, 0), l2, Collider2{Shape: Circle{0.1}})
		b2 := w2.SpawnWith(gfx.At2(5, 0), r2, Collider2{Shape: Circle{0.1}})
		run(w2, 0.5)
		t2a, _ := w2.Get[gfx.Transform2](a2)
		t2b, _ := w2.Get[gfx.Transform2](b2)
		if ccd && t2a.Position.X > t2b.Position.X {
			t.Errorf("2D: CCD circles passed through each other: %v %v", t2a.Position, t2b.Position)
		}
		if !ccd && t2a.Position.X < t2b.Position.X {
			t.Errorf("2D: circles without CCD should tunnel: %v %v", t2a.Position, t2b.Position)
		}
	}
}
