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

// TestRagdollsSleep drops fifty ragdolls on a floor at the default
// settings with sleeping on: every part is asleep within eight seconds.
// Their limbs used to roll on forever at a few millimetres a second,
// driven by a capsule contact that wandered along the limb, so a pile of
// ragdolls cost a full step every frame.
func TestRagdollsSleep(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0), SleepTime: 0.5})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(100, 0.5, 100)}})
	for i := range 50 {
		NewRagdoll3(w, RagdollSpec{Position: lin.V3(float32(i%10)*3-15, 1, float32(i/10)*3-7),
			Rotation: lin.AxisAngle(lin.V3(1, 0, 0), 1.2)})
	}
	awake := 0
	for range 8 * 60 {
		w.Update(step)
		awake = 0
		w.Each(func(_ ecs.Entity, b *Body3) {
			if !b.Asleep() {
				awake++
			}
		})
		if awake == 0 {
			return
		}
	}
	t.Errorf("%d of 550 ragdoll parts still awake after eight seconds", awake)
}

// TestCapsuleBoxMatchesSupportPath checks the flat capsule contact
// against the general support-function path it replaces: where it
// applies, the deepest contact has the same depth and normal.
func TestCapsuleBoxMatchesSupportPath(t *testing.T) {
	r := indexRand(11)
	var sc scratch3
	used, outliers := 0, 0
	for range 4000 {
		c := Capsule{Radius: r.in(0.03, 0.3), HalfHeight: r.in(0.05, 0.6)}
		box := Box3{Half: lin.V3(r.in(0.5, 3), r.in(0.2, 1), r.in(0.5, 3))}
		brot := r.quat()
		if r.intn(2) == 0 {
			brot = lin.Quat{}
		}
		bm := mat3FromQuat(brot)
		// Lay the capsule roughly flat on the box's top face, tilted a
		// little and sunk or lifted by a little.
		up := bm.axis(1)
		lie := lin.AxisAngle(lin.V3(0, 0, 1), math.Pi/2+r.in(-0.02, 0.02)).Mul(lin.AxisAngle(lin.V3(1, 0, 0), r.in(-3, 3)))
		crot := brot.Mul(lie)
		if brot == (lin.Quat{}) {
			crot = lie
		}
		cpos := bm.mulVec(lin.V3(r.in(-0.4, 0.4), 0, r.in(-0.4, 0.4))).Add(up.Mul(box.Half.Y + c.Radius - r.in(-0.002, 0.02)))
		cm := mat3FromQuat(crot)
		flat, ok := capsuleBox(nil, c, cpos, cm, obb{lin.Vec3{}, bm, box.Half})
		if !ok {
			continue
		}
		used++
		general := convexPair(&sc, nil, c, cpos, cm, box, lin.Vec3{}, bm)
		if len(general) == 0 {
			t.Fatalf("the support path finds no contact where the flat path finds %v", flat)
		}
		deepest := func(cs []contact3) contact3 {
			d := cs[0]
			for _, x := range cs[1:] {
				if x.depth > d.depth {
					d = x
				}
			}
			return d
		}
		a, b := deepest(flat), deepest(general)
		// The exact depth: the capsule's radius less the axis's distance
		// from the box, found along the axis finely.
		ca, cb := c.segment(cpos, cm)
		exact := float32(math.Inf(-1))
		for k := range 1001 {
			p := ca.Add(cb.Sub(ca).Mul(float32(k) / 1000))
			if d, _, ok := SignedDistance3(box, lin.Vec3{}, brot, p); ok {
				exact = max(exact, c.Radius-d)
			}
		}
		if !near(a.depth, exact, 1e-4) {
			t.Fatalf("flat contact depth %v, exact %v", a.depth, exact)
		}
		// The support path stops refining to within about a hundredth of
		// a radian, so its normal is compared that loosely, and a few of
		// its answers are wrong outright, which the exact depth shows.
		if !near(b.depth, exact, 1e-3) {
			outliers++
			continue
		}
		if !near(a.depth, b.depth, 1e-3) || a.normal.Sub(b.normal).Len() > 5e-2 {
			t.Fatalf("flat contact depth %v normal %v, support path depth %v normal %v", a.depth, a.normal, b.depth, b.normal)
		}
	}
	if used < 1000 {
		t.Fatalf("the flat path applied in only %d of 4000 placements", used)
	}
	if outliers > used/100 {
		t.Fatalf("the support path strayed from the exact depth in %d of %d placements", outliers, used)
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
