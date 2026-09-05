package phys

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestPrismaticSlides3D checks that a slider runs freely along its axis
// and holds every other degree of freedom while gravity pulls on it.
func TestPrismaticSlides3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	rail := w.SpawnWith(gfx.At(0, 5, 0))
	body := Dynamic3(1)
	body.Vel = lin.V3(2, 0, 0)
	e := w.SpawnWith(gfx.At(0, 5, 0), body, Collider3{Shape: Box3{Half: lin.V3(0.2, 0.2, 0.2)}})
	w.SpawnWith(PrismaticJoint3{A: rail, B: e, Axis: lin.V3(1, 0, 0)})
	for range 120 {
		w.Update(step)
		tr, _ := w.Get[gfx.Transform](e)
		if !near(tr.Position.Y, 5, 0.01) || !near(tr.Position.Z, 0, 0.01) {
			t.Fatalf("slider left the rail: %v", tr.Position)
		}
		// A zero quaternion is the identity throughout the engine.
		if q := tr.Rotation; q != (lin.Quat{}) && !near(abs32(q.W), 1, 0.01) {
			t.Fatalf("slider turned on the rail: %v", q)
		}
	}
	tr, _ := w.Get[gfx.Transform](e)
	if tr.Position.X < 3.5 {
		t.Errorf("slider should have run freely along the rail: x %.3f, want about 4", tr.Position.X)
	}
	j := &PrismaticJoint3{A: rail, B: e, Axis: lin.V3(1, 0, 0)}
	if !near(j.Translation(w), tr.Position.X, 1e-4) {
		t.Errorf("Translation %v does not match the position %v", j.Translation(w), tr.Position.X)
	}
}

// TestPrismaticLimits3D pushes a slider into each limit and checks it
// stops there.
func TestPrismaticLimits3D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	rail := w.SpawnWith(gfx.At(0, 5, 0))
	e := w.SpawnWith(gfx.At(0, 5, 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.2, 0.2, 0.2)}})
	w.SpawnWith(PrismaticJoint3{A: rail, B: e, Axis: lin.V3(1, 0, 0), Min: -1, Max: 1})
	push := func(fx float32, want float32) {
		for range 180 {
			if b, ok := w.Get[Body3](e); ok {
				b.AddForce(lin.V3(fx, 0, 0))
			}
			w.Update(step)
			tr, _ := w.Get[gfx.Transform](e)
			if tr.Position.X > 1.05 || tr.Position.X < -1.05 {
				t.Fatalf("the limit let the slider past: x %.3f", tr.Position.X)
			}
		}
		tr, _ := w.Get[gfx.Transform](e)
		if !near(tr.Position.X, want, 0.05) {
			t.Errorf("pushed at %.0f the slider should rest at %.0f: x %.3f", fx, want, tr.Position.X)
		}
	}
	push(200, 1)
	push(-200, -1)
}

// TestPrismaticMotor2D drives a slider with its motor and checks it
// reaches the target speed while gravity does not move it off the rail.
func TestPrismaticMotor2D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	rail := w.SpawnWith(gfx.At2(0, 5))
	e := w.SpawnWith(gfx.At2(0, 5), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.2, HalfH: 0.2}})
	w.SpawnWith(PrismaticJoint2{A: rail, B: e, Axis: lin.V2(1, 0), MotorSpeed: 3, MaxMotorForce: 50})
	run(w, 1)
	b, _ := w.Get[Body2](e)
	if !near(b.Vel.X, 3, 0.05) {
		t.Errorf("the motor should reach 3 units per second: %v", b.Vel)
	}
	tr, _ := w.Get[gfx.Transform2](e)
	if !near(tr.Position.Y, 5, 0.01) {
		t.Errorf("the slider drifted off the rail: %v", tr.Position)
	}
	// A motor whose force is too small for the load stalls instead.
	w2 := ecs.NewWorld()
	w2.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w2.AddSystem("phys", System2)
	up := w2.SpawnWith(gfx.At2(0, 5))
	e2 := w2.SpawnWith(gfx.At2(0, 5), Dynamic2(10), Collider2{Shape: Box2{HalfW: 0.2, HalfH: 0.2}})
	w2.SpawnWith(PrismaticJoint2{A: up, B: e2, Axis: lin.V2(0, 1), MotorSpeed: 1, MaxMotorForce: 20})
	run(w2, 1)
	b2, _ := w2.Get[Body2](e2)
	if b2.Vel.Y > 0 {
		t.Errorf("a motor weaker than the load should not lift it: %v", b2.Vel)
	}
}

// TestWheelSpring2D releases a wheel below its rest position and checks
// the suspension brings it back with a damped response.
func TestWheelSpring2D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	chassis := w.SpawnWith(gfx.At2(0, 5))
	wheel := w.SpawnWith(gfx.At2(0, 3.5), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.3}})
	w.SpawnWith(WheelJoint2{A: chassis, B: wheel, AnchorA: lin.V2(0, -1), Axis: lin.V2(0, 1),
		Frequency: 4, DampingRatio: 0.7})
	// The spring holds the weight at k = m·ω², so it hangs a little low.
	omega := 2 * 3.14159265 * 4.0
	rest := float32(4 - 10/(omega*omega))
	var overshoot float32
	for range 240 {
		w.Update(step)
		tr, _ := w.Get[gfx.Transform2](wheel)
		overshoot = max(overshoot, tr.Position.Y-rest)
		if !near(tr.Position.X, 0, 1e-3) {
			t.Fatalf("the wheel left the suspension line: %v", tr.Position)
		}
	}
	tr, _ := w.Get[gfx.Transform2](wheel)
	if !near(tr.Position.Y, rest, 0.02) {
		t.Errorf("the suspension should settle at %.4f: %.4f", rest, tr.Position.Y)
	}
	// A damping ratio of 0.7 overshoots a step of 0.5 by about five per
	// cent, so anything much larger means the spring is not damped.
	if overshoot > 0.1 {
		t.Errorf("the suspension overshot its rest position by %.3f", overshoot)
	}
	j := &WheelJoint2{A: chassis, B: wheel, AnchorA: lin.V2(0, -1), Axis: lin.V2(0, 1)}
	if !near(j.Translation(w), tr.Position.Y-4, 1e-4) {
		t.Errorf("Translation %v does not match the compression %v", j.Translation(w), tr.Position.Y-4)
	}
}

// TestWheelCar2D drives a two-wheeled car along flat ground with the
// motors on its wheel joints.
func TestWheelCar2D(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	const (
		ground  = 1
		frame   = 2
		rolling = 4
	)
	w.SpawnWith(gfx.At2(0, -0.5), Collider2{Shape: Box2{HalfW: 60, HalfH: 0.5},
		Layers: Layers{Layer: ground, Mask: frame | rolling}})
	body := Dynamic2(20)
	body.Friction = 0.6
	car := w.SpawnWith(gfx.At2(0, 1), body, Collider2{Shape: Box2{HalfW: 1, HalfH: 0.25},
		Layers: Layers{Layer: frame, Mask: ground}})
	wheelAt := func(x float32) ecs.Entity {
		b := Dynamic2(1)
		b.Friction = 0.9
		e := w.SpawnWith(gfx.At2(x, 0.35), b, Collider2{Shape: Circle{Radius: 0.35},
			Layers: Layers{Layer: rolling, Mask: ground}})
		w.SpawnWith(WheelJoint2{A: car, B: e, AnchorA: lin.V2(x, -0.65), Axis: lin.V2(0, 1),
			Frequency: 4, DampingRatio: 0.7, MotorSpeed: -20, MaxMotorTorque: 20})
		return e
	}
	wheelAt(-0.8)
	wheelAt(0.8)
	run(w, 3)
	tr, _ := w.Get[gfx.Transform2](car)
	// The wheels spin at 20 radians per second on a radius of 0.35, so
	// the car tops out near 7 units per second.
	if tr.Position.X < 8 {
		t.Errorf("the car should have driven forward: x %.3f, want at least 8", tr.Position.X)
	}
	// It rides a little lower than it started as the springs take the
	// chassis weight, and it stays the right way up.
	if tr.Position.Y < 0.6 || tr.Position.Y > 1.05 || !near(tr.Rotation, 0, 0.3) {
		t.Errorf("the car should still be upright on its wheels: %v rot %.3f", tr.Position, tr.Rotation)
	}
}
