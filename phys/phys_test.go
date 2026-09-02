package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

const step = 1.0 / 60

func run(w *ecs.World, seconds float64) {
	for t := 0.0; t < seconds; t += step {
		w.Update(step)
	}
}

func TestFallAndRest2D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}}) // floor top at y=0.5
	ball := w.SpawnWith(gfx.At2(0, 5), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.5}})
	box := w.SpawnWith(gfx.At2(3, 4), Dynamic2(2), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	run(w, 3)
	bt, _ := ecs.Get[gfx.Transform2](w, ball)
	bb, _ := ecs.Get[Body2](w, ball)
	if math.Abs(float64(bt.Position.Y-1)) > 0.05 || bb.Vel.Len() > 0.05 {
		t.Fatalf("ball should rest on the floor at y=1: pos %v vel %v", bt.Position, bb.Vel)
	}
	xt, _ := ecs.Get[gfx.Transform2](w, box)
	if math.Abs(float64(xt.Position.Y-1)) > 0.05 || math.Abs(float64(xt.Rotation)) > 0.05 {
		t.Fatalf("box should rest flat at y=1: pos %v rot %v", xt.Position, xt.Rotation)
	}
	if len(ecs.Events[Collision2](w)) == 0 {
		t.Fatal("resting contact should report collisions")
	}
}

func TestBounceAndFriction2D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 50, HalfH: 0.5}})
	bouncy := Dynamic2(1)
	bouncy.Restitution = 0.8
	ball := w.SpawnWith(gfx.At2(0, 10), bouncy, Collider2{Shape: Circle{Radius: 0.5}})
	peak := float32(0)
	landed := false
	for i := 0; i < 240; i++ {
		w.Update(step)
		bt, _ := ecs.Get[gfx.Transform2](w, ball)
		bb, _ := ecs.Get[Body2](w, ball)
		if bb.Vel.Y > 0 {
			landed = true
		}
		if landed {
			peak = max(peak, bt.Position.Y)
		}
	}
	if !landed || peak < 4 || peak > 9 {
		t.Fatalf("a 0.8 restitution ball dropped from 10 should bounce to roughly 6: peak %v", peak)
	}
	// A sliding box with friction stops; without friction it keeps going.
	for _, friction := range []float32{0.5, 0} {
		w := ecs.NewWorld()
		ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
		w.AddSystem("phys", System2)
		w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 100, HalfH: 0.5}})
		body := Dynamic2(1)
		body.Friction, body.Restitution = friction, 0
		body.Vel = lin.V2(5, 0)
		slider := w.SpawnWith(gfx.At2(0, 1.0), body, Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
		run(w, 2)
		b, _ := ecs.Get[Body2](w, slider)
		if friction > 0 && math.Abs(float64(b.Vel.X)) > 0.1 {
			t.Fatalf("friction %v: still sliding at %v", friction, b.Vel)
		}
		if friction == 0 && b.Vel.X < 4.5 {
			t.Fatalf("no friction: slowed to %v", b.Vel)
		}
	}
}

func TestStackTriggerRaycast2D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10), Substeps: 8, Iterations: 12})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}})
	var boxes []ecs.Entity
	for i := range 4 {
		boxes = append(boxes, w.SpawnWith(gfx.At2(0, 1+float32(i)*1.0), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}}))
	}
	trigger := w.SpawnWith(gfx.At2(5, 2), Collider2{Shape: Circle{Radius: 1}, Trigger: true})
	visitor := w.SpawnWith(gfx.At2(5, 3), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.3}})
	run(w, 3)
	for i, b := range boxes {
		bt, _ := ecs.Get[gfx.Transform2](w, b)
		if math.Abs(float64(bt.Position.Y-float32(1+i))) > 0.1 || math.Abs(float64(bt.Position.X)) > 0.1 {
			t.Fatalf("stack box %d drifted to %v", i, bt.Position)
		}
	}
	trig := false
	for _, ev := range ecs.Events[Trigger2](w) {
		if ev.Trigger == trigger && ev.Other == visitor {
			trig = true
		}
	}
	if !trig {
		t.Fatal("trigger did not report the visitor")
	}
	vt, _ := ecs.Get[gfx.Transform2](w, visitor)
	if vt.Position.Y > 1.5 {
		t.Fatal("trigger blocked the visitor instead of letting it fall through")
	}
	hit, ok := Raycast2(w, Ray2{Origin: lin.V2(-5, 2.5), Dir: lin.V2(10, 0)}, 0)
	if !ok || hit.Entity != boxes[2] || math.Abs(float64(hit.Point.X+0.5)) > 0.05 || hit.Normal.X > -0.9 {
		t.Fatalf("raycast %+v ok=%v", hit, ok)
	}
	_, ok = Raycast2(w, Ray2{Origin: lin.V2(-5, 20), Dir: lin.V2(10, 0)}, 0)
	if ok {
		t.Fatal("raycast above everything should miss")
	}
}

func TestLayersAndKinematic2D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}, Layers: Layers{Layer: 1, Mask: 1}})
	ghost := w.SpawnWith(gfx.At2(0, 3), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.5}, Layers: Layers{Layer: 2, Mask: 2}})
	run(w, 1)
	gt, _ := ecs.Get[gfx.Transform2](w, ghost)
	if gt.Position.Y > 0 {
		t.Fatal("layers should let the ghost fall through the floor")
	}
	// A kinematic platform carries a box upward.
	platform := Kinematic2()
	platform.Vel = lin.V2(0, 1)
	plat := w.SpawnWith(gfx.At2(5, 0), platform, Collider2{Shape: Box2{HalfW: 2, HalfH: 0.25}})
	rider := w.SpawnWith(gfx.At2(5, 1), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.4, HalfH: 0.4}})
	run(w, 2)
	pt, _ := ecs.Get[gfx.Transform2](w, plat)
	rt, _ := ecs.Get[gfx.Transform2](w, rider)
	if pt.Position.Y < 1.9 || rt.Position.Y < pt.Position.Y+0.5 {
		t.Fatalf("platform %v should carry the rider %v", pt.Position, rt.Position)
	}
}

func TestFallRestAndStack3D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), Substeps: 8, Iterations: 12})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	ball := w.SpawnWith(gfx.At(0, 4, 0), Dynamic3(1), Collider3{Shape: Sphere{Radius: 0.5}})
	var boxes []ecs.Entity
	for i := range 3 {
		boxes = append(boxes, w.SpawnWith(gfx.At(3, 1+float32(i), 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}}))
	}
	tilted := w.SpawnWith(gfx.Transform{Position: lin.V3(-3, 3, 0), Rotation: lin.AxisAngle(lin.V3(0, 0, 1), 0.3)}, Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	run(w, 4)
	bt, _ := ecs.Get[gfx.Transform](w, ball)
	bb, _ := ecs.Get[Body3](w, ball)
	if math.Abs(float64(bt.Position.Y-1)) > 0.05 || bb.Vel.Len() > 0.05 {
		t.Fatalf("ball should rest at y=1: %v vel %v", bt.Position, bb.Vel)
	}
	for i, b := range boxes {
		xt, _ := ecs.Get[gfx.Transform](w, b)
		if math.Abs(float64(xt.Position.Y-float32(1+i))) > 0.1 || xt.Position.Sub(lin.V3(3, xt.Position.Y, 0)).Len() > 0.1 {
			t.Fatalf("stacked box %d at %v", i, xt.Position)
		}
	}
	tt, _ := ecs.Get[gfx.Transform](w, tilted)
	up := tt.Rotation.Rotate(lin.V3(0, 1, 0))
	if math.Abs(float64(tt.Position.Y-1)) > 0.05 || math.Abs(float64(up.Y)) < 0.98 {
		t.Fatalf("tilted box should settle flat: pos %v up %v", tt.Position, up)
	}
	if len(ecs.Events[Collision3](w)) == 0 {
		t.Fatal("resting contacts should report collisions")
	}
}

func TestBounceRollRaycast3D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(50, 0.5, 50)}})
	bouncy := Dynamic3(1)
	bouncy.Restitution = 0.8
	ball := w.SpawnWith(gfx.At(0, 10, 0), bouncy, Collider3{Shape: Sphere{Radius: 0.5}})
	peak, landed := float32(0), false
	for range 240 {
		w.Update(step)
		bt, _ := ecs.Get[gfx.Transform](w, ball)
		bb, _ := ecs.Get[Body3](w, ball)
		if bb.Vel.Y > 0 {
			landed = true
		}
		if landed {
			peak = max(peak, bt.Position.Y)
		}
	}
	if !landed || peak < 4 || peak > 9 {
		t.Fatalf("bounce peak %v", peak)
	}
	// A pushed ball rolls: friction converts sliding into spin.
	roller := Dynamic3(1)
	roller.Vel = lin.V3(3, 0, 0)
	r := w.SpawnWith(gfx.At(0, 1, 5), roller, Collider3{Shape: Sphere{Radius: 0.5}})
	run(w, 1)
	rb, _ := ecs.Get[Body3](w, r)
	if math.Abs(float64(rb.AngVel.Z)) < 1 {
		t.Fatalf("rolling ball should spin: angvel %v", rb.AngVel)
	}
	hit, ok := Raycast3(w, Ray3{Origin: lin.V3(0, 20, 5), Dir: lin.V3(0, -40, 0)}, 0)
	if !ok || hit.Normal.Y < 0.9 {
		t.Fatalf("downward raycast %+v ok=%v", hit, ok)
	}
	if hit.Entity != r && math.Abs(float64(hit.Point.Y-0.5)) > 0.05 {
		t.Fatalf("expected the floor top (y=0.5) or the ball: %+v", hit)
	}
	trigger := w.SpawnWith(gfx.At(0, 2, -5), Collider3{Shape: Sphere{Radius: 1}, Trigger: true})
	visitor := w.SpawnWith(gfx.At(0, 3, -5), Dynamic3(1), Collider3{Shape: Sphere{Radius: 0.3}})
	run(w, 0.5)
	found := false
	for _, ev := range ecs.Events[Trigger3](w) {
		found = found || (ev.Trigger == trigger && ev.Other == visitor)
	}
	if !found {
		t.Fatal("3D trigger did not report")
	}
}

func BenchmarkBoxes3D(b *testing.B) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(50, 0.5, 50)}})
	for i := range 500 {
		x, z := float32(i%25)*1.2-15, float32(i/25)*1.2-12
		w.SpawnWith(gfx.At(x, 2+float32(i%3), z), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	}
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}
