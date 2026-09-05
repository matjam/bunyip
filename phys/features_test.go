package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestQueries3D checks overlaps, shape casts, nearest and all-hit rays.
func TestQueries3D(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("phys", System3)
	near1 := w.SpawnWith(gfx.At(1, 0, 0), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	near2 := w.SpawnWith(gfx.At(-1, 0, 0), Collider3{Shape: Sphere{0.5}})
	far := w.SpawnWith(gfx.At(6, 0, 0), Collider3{Shape: Capsule{0.5, 0.5}})
	trigger := w.SpawnWith(gfx.At(0, 1, 0), Collider3{Shape: Sphere{0.5}, Trigger: true})
	w.Update(step)
	hits := OverlapSphere3(w, lin.Vec3{}, 1.2, 0)
	got := map[ecs.Entity]bool{}
	for _, h := range hits {
		got[h.Entity] = true
	}
	if len(hits) != 3 || !got[near1] || !got[near2] || !got[trigger] {
		t.Errorf("overlap sphere: %+v", hits)
	}
	if hits := OverlapBox3(w, lin.V3(6, 0, 0), lin.V3(0.2, 0.2, 0.2), lin.Quat{}, 0); len(hits) != 1 || hits[0].Entity != far {
		t.Errorf("overlap box: %+v", hits)
	}
	// A sphere swept toward the far capsule touches it after 6-1-0.5-0.5 = 4 units of 8.
	hit, ok := ShapeCast3(w, Sphere{0.5}, lin.V3(2.5, 0, 3), lin.Quat{}, lin.V3(0, 0, -3), 0)
	if ok {
		t.Errorf("shape cast should miss: %+v", hit)
	}
	hit, ok = ShapeCast3(w, Sphere{0.5}, lin.V3(2, 0, 0), lin.Quat{}, lin.V3(8, 0, 0), 0)
	if !ok || hit.Entity != far || !near(hit.Distance, 3.0/8, 0.02) || hit.Normal.X > -0.9 || !near(hit.Point.X, 5.5, 0.05) {
		t.Errorf("shape cast: %+v ok=%v", hit, ok)
	}
	hit, ok = Nearest3(w, lin.V3(0, 0, 0), 10, 0)
	if !ok || (hit.Entity != near1 && hit.Entity != near2) || !near(hit.Distance, 0.5, 0.02) {
		t.Errorf("nearest: %+v ok=%v", hit, ok)
	}
	if _, ok := Nearest3(w, lin.V3(0, 0, 20), 5, 0); ok {
		t.Error("nearest should find nothing within 5 of a far point")
	}
	all := RaycastAll3(w, Ray3{Origin: lin.V3(-5, 0, 0), Dir: lin.V3(20, 0, 0)}, 0)
	if len(all) != 3 || all[0].Entity != near2 || all[1].Entity != near1 || all[2].Entity != far {
		t.Errorf("raycast all: %+v", all)
	}
}

// TestQueries2D checks the 2D overlaps, shape casts, nearest and rays.
func TestQueries2D(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("phys", System2)
	box := w.SpawnWith(gfx.At2(1, 0), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	circle := w.SpawnWith(gfx.At2(-1, 0), Collider2{Shape: Circle{0.5}})
	far := w.SpawnWith(gfx.At2(6, 0), Collider2{Shape: Capsule2{0.5, 0.5}})
	w.Update(step)
	hits := OverlapCircle2(w, lin.Vec2{}, 1.2, 0)
	if len(hits) != 2 {
		t.Errorf("overlap circle: %+v", hits)
	}
	if hits := OverlapBox2(w, lin.V2(6, 0), 0.2, 0.2, 0, 0); len(hits) != 1 || hits[0].Entity != far {
		t.Errorf("overlap box: %+v", hits)
	}
	hit, ok := ShapeCast2(w, Circle{0.5}, lin.V2(2, 0), 0, lin.V2(8, 0), 0)
	if !ok || hit.Entity != far || !near(hit.Distance, 3.0/8, 0.03) || hit.Normal.X > -0.9 {
		t.Errorf("shape cast: %+v ok=%v", hit, ok)
	}
	hit, ok = Nearest2(w, lin.V2(0, 0), 10, 0)
	if !ok || (hit.Entity != box && hit.Entity != circle) || !near(hit.Distance, 0.5, 0.02) {
		t.Errorf("nearest: %+v ok=%v", hit, ok)
	}
	all := RaycastAll2(w, Ray2{Origin: lin.V2(-5, 0), Dir: lin.V2(20, 0)}, 0)
	if len(all) != 3 || all[0].Entity != circle || all[1].Entity != box || all[2].Entity != far {
		t.Errorf("raycast all: %+v", all)
	}
}

// TestCCD fires a small fast sphere at a thin wall: with CCD it stops,
// without it tunnels through.
func TestCCD(t *testing.T) {
	for _, ccd := range []bool{true, false} {
		w := ecs.NewWorld()
		ecs.SetResource(w, Settings3{})
		w.AddSystem("phys", System3)
		w.SpawnWith(gfx.At(5, 0, 0), Collider3{Shape: Box3{Half: lin.V3(0.05, 2, 2)}})
		body := Dynamic3(1)
		body.Vel = lin.V3(200, 0, 0)
		body.CCD = ccd
		bullet := w.SpawnWith(gfx.At(0, 0, 0), body, Collider3{Shape: Sphere{0.1}})
		run(w, 1)
		tr, _ := ecs.Get[gfx.Transform](w, bullet)
		if ccd && tr.Position.X > 5 {
			t.Errorf("3D: bullet with CCD passed the wall: %v", tr.Position)
		}
		if !ccd && tr.Position.X < 5 {
			t.Errorf("3D: bullet without CCD should tunnel (the test needs a thinner wall): %v", tr.Position)
		}
		w2 := ecs.NewWorld()
		w2.AddSystem("phys", System2)
		w2.SpawnWith(gfx.At2(5, 0), Collider2{Shape: Box2{HalfW: 0.05, HalfH: 2}})
		body2 := Dynamic2(1)
		body2.Vel = lin.V2(200, 0)
		body2.CCD = ccd
		bullet2 := w2.SpawnWith(gfx.At2(0, 0), body2, Collider2{Shape: Circle{0.1}})
		run(w2, 1)
		tr2, _ := ecs.Get[gfx.Transform2](w2, bullet2)
		if ccd && tr2.Position.X > 5 {
			t.Errorf("2D: bullet with CCD passed the wall: %v", tr2.Position)
		}
		if !ccd && tr2.Position.X < 5 {
			t.Errorf("2D: bullet without CCD should tunnel: %v", tr2.Position)
		}
	}
}

// TestSleep lets a stack settle and sleep, then drops a box on it.
func TestSleep(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), SleepTime: 0.5, Substeps: 8, Iterations: 12})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	var stack []ecs.Entity
	for i := range 3 {
		stack = append(stack, w.SpawnWith(gfx.At(0, 1+float32(i), 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}}))
	}
	run(w, 3)
	for i, e := range stack {
		b, _ := ecs.Get[Body3](w, e)
		if !b.Asleep() || b.Vel != (lin.Vec3{}) {
			t.Errorf("stack box %d should sleep: asleep=%v vel %v", i, b.Asleep(), b.Vel)
		}
	}
	dropped := w.SpawnWith(gfx.At(0, 6, 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	woke := false
	for range 60 {
		w.Update(step)
		b, _ := ecs.Get[Body3](w, stack[2])
		if !b.Asleep() {
			woke = true
		}
	}
	if !woke {
		t.Error("the dropped box did not wake the top of the stack")
	}
	run(w, 3)
	tr, _ := ecs.Get[gfx.Transform](w, dropped)
	b, _ := ecs.Get[Body3](w, dropped)
	if !near(tr.Position.Y, 4, 0.1) || !b.Asleep() {
		t.Errorf("dropped box should rest asleep on the stack at y 4: %v asleep=%v", tr.Position, b.Asleep())
	}
	b.AddImpulse(lin.V3(3, 0, 0))
	if b.Asleep() {
		t.Error("an impulse should wake a body")
	}
	// 2D: a resting box sleeps and a kinematic platform pushing it wakes it.
	w2 := ecs.NewWorld()
	ecs.SetResource(w2, Settings2{Gravity: lin.V2(0, -10), SleepTime: 0.5})
	w2.AddSystem("phys", System2)
	w2.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}})
	box := w2.SpawnWith(gfx.At2(0, 1), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	run(w2, 3)
	b2, _ := ecs.Get[Body2](w2, box)
	if !b2.Asleep() {
		t.Error("2D box should sleep")
	}
	pusher := Kinematic2()
	pusher.Vel = lin.V2(1, 0)
	w2.SpawnWith(gfx.At2(-2, 1), pusher, Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	run(w2, 2)
	bt, _ := ecs.Get[gfx.Transform2](w2, box)
	if bt.Position.X < 0.5 {
		t.Errorf("the platform should have pushed the sleeping box: %v", bt.Position)
	}
}

// stackSleepTime builds a stack of n boxes on a static floor at the
// default solver quality and returns when they were all asleep, or -1.
func stackSleepTime(n int, seconds float64) float64 {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), SleepTime: 0.5})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	var stack []ecs.Entity
	for i := range n {
		stack = append(stack, w.SpawnWith(gfx.At(0, 1+float32(i)*1.01, 0), Dynamic3(1),
			Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}}))
	}
	for t := 0.0; t < seconds; t += step {
		w.Update(step)
		all := true
		for _, e := range stack {
			if b, _ := ecs.Get[Body3](w, e); !b.Asleep() {
				all = false
			}
		}
		if all {
			return t
		}
	}
	return -1
}

// TestStackSleepsAtDefaults settles stacks of boxes at Substeps 4 and
// Iterations 8. The position correction adds a separating speed of the
// same order as the sleep threshold, so without the relax pass that
// takes it back out a stack never rests below the threshold.
func TestStackSleepsAtDefaults(t *testing.T) {
	if at := stackSleepTime(4, 5); at < 0 || at > 2 {
		t.Errorf("four boxes at the defaults slept at %.2fs, want within 2s", at)
	}
	if at := stackSleepTime(8, 5); at < 0 || at > 3 {
		t.Errorf("eight boxes at the defaults slept at %.2fs, want within 3s", at)
	}
	// 2D at the defaults settles as well.
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10), SleepTime: 0.5})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}})
	var stack []ecs.Entity
	for i := range 4 {
		stack = append(stack, w.SpawnWith(gfx.At2(0, 1+float32(i)*1.01), Dynamic2(1),
			Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}}))
	}
	run(w, 2)
	for i, e := range stack {
		if b, _ := ecs.Get[Body2](w, e); !b.Asleep() {
			t.Errorf("2D stack box %d did not sleep within 2s", i)
		}
	}
}

// TestSlowSliderStaysAwake keeps a body moving at 0.2 units per second
// out of the sleep state: the relax pass must not stop a body that is
// genuinely moving.
func TestSlowSliderStaysAwake(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), SleepTime: 0.5})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(50, 0.5, 10)}})
	slider := Dynamic3(1)
	slider.Friction = 0 // ice: nothing slows it down
	slider.Vel = lin.V3(0.2, 0, 0)
	e := w.SpawnWith(gfx.At(-8, 1, 0), slider, Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	run(w, 5)
	b, _ := ecs.Get[Body3](w, e)
	if b.Asleep() {
		t.Error("a body sliding at 0.2 units per second went to sleep")
	}
	if !near(b.Vel.X, 0.2, 0.02) {
		t.Errorf("the slider should keep its speed: %v", b.Vel)
	}
}

// TestJoints3D swings a pendulum on a distance joint, hangs a chain of
// hinges, welds two boxes and bounces a spring.
func TestJoints3D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), Substeps: 4, Iterations: 8})
	w.AddSystem("phys", System3)
	pivot := w.SpawnWith(gfx.At(0, 5, 0))
	bob := w.SpawnWith(gfx.At(2, 5, 0), Dynamic3(1), Collider3{Shape: Sphere{0.2}})
	w.SpawnWith(DistanceJoint3{A: pivot, B: bob})
	swung := false
	for i := range 240 {
		w.Update(step)
		bt, _ := ecs.Get[gfx.Transform](w, bob)
		if d := bt.Position.Sub(lin.V3(0, 5, 0)).Len(); !near(d, 2, 0.05) {
			t.Fatalf("frame %d: pendulum length %.3f, want 2", i, d)
		}
		if bt.Position.X < -1 {
			swung = true
		}
	}
	if !swung {
		t.Error("the pendulum did not swing across")
	}
	// A rope lets the bob fall until taut.
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	bob = w.SpawnWith(gfx.At(0, 5, 0), Dynamic3(1), Collider3{Shape: Sphere{0.2}})
	w.SpawnWith(DistanceJoint3{A: ecs.None, AnchorA: lin.V3(0, 5, 0), B: bob, Max: 3})
	run(w, 2)
	bt, _ := ecs.Get[gfx.Transform](w, bob)
	if !near(bt.Position.Y, 2, 0.1) {
		t.Errorf("rope bob should hang 3 below the anchor: %v", bt.Position)
	}
	// A chain of hinged links hangs from a world anchor.
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0), Substeps: 4, Iterations: 12})
	w.AddSystem("phys", System3)
	var links []ecs.Entity
	prev := ecs.None
	anchorA := lin.V3(0, 8, 0)
	for i := range 5 {
		body := Dynamic3(1)
		body.LinearDamping, body.AngularDamping = 2, 2
		link := w.SpawnWith(gfx.At(0.5+float32(i), 8, 0), body, Collider3{Shape: Box3{Half: lin.V3(0.5, 0.1, 0.1)}, Layers: Layers{Layer: 2, Mask: 1}})
		w.SpawnWith(HingeJoint3{A: prev, AnchorA: anchorA, B: link, AnchorB: lin.V3(-0.5, 0, 0), AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1)})
		anchorA = lin.V3(0.5, 0, 0)
		links = append(links, link)
		prev = link
	}
	run(w, 8)
	for i, link := range links {
		lt, _ := ecs.Get[gfx.Transform](w, link)
		wantY := 8 - 0.5 - float32(i)
		if !near(lt.Position.Y, wantY, 0.15) || abs32(lt.Position.X) > 0.15 {
			t.Errorf("link %d hangs at %v, want (0, %.1f, 0)", i, lt.Position, wantY)
		}
		if i > 0 {
			pt, _ := ecs.Get[gfx.Transform](w, links[i-1])
			end := pt.Position.Add(pt.Rotation.Rotate(lin.V3(0.5, 0, 0)))
			start := lt.Position.Add(lt.Rotation.Rotate(lin.V3(-0.5, 0, 0)))
			if end.Sub(start).Len() > 0.05 {
				t.Errorf("hinge %d separated by %.3f", i, end.Sub(start).Len())
			}
		}
	}
	// A weld keeps two boxes together as they fall and land.
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	a := w.SpawnWith(gfx.At(0, 4, 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	b := w.SpawnWith(gfx.At(1, 4, 0), Dynamic3(1), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	w.SpawnWith(FixedJoint3{A: a, B: b, AnchorA: lin.V3(0.5, 0, 0), AnchorB: lin.V3(-0.5, 0, 0)})
	run(w, 3)
	at, _ := ecs.Get[gfx.Transform](w, a)
	btr, _ := ecs.Get[gfx.Transform](w, b)
	if !near(btr.Position.Sub(at.Position).Len(), 1, 0.05) || !near(at.Position.Y, 1, 0.1) {
		t.Errorf("welded boxes at %v and %v", at.Position, btr.Position)
	}
	// A spring hangs a mass and settles to the stretched length.
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	mass := Dynamic3(1)
	m := w.SpawnWith(gfx.At(0, 3, 0), mass, Collider3{Shape: Sphere{0.1}})
	w.SpawnWith(SpringJoint3{A: ecs.None, AnchorA: lin.V3(0, 5, 0), B: m, Stiffness: 50, Damping: 5})
	run(w, 6)
	mt, _ := ecs.Get[gfx.Transform](w, m)
	if !near(mt.Position.Y, 3-0.2, 0.05) {
		t.Errorf("spring mass settled at %v, want y 2.8", mt.Position)
	}
}

// TestJoints2D swings a pendulum and hangs a pinned chain in 2D.
func TestJoints2D(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	bob := w.SpawnWith(gfx.At2(2, 5), Dynamic2(1), Collider2{Shape: Circle{0.2}})
	w.SpawnWith(DistanceJoint2{A: ecs.None, AnchorA: lin.V2(0, 5), B: bob})
	for i := range 240 {
		w.Update(step)
		bt, _ := ecs.Get[gfx.Transform2](w, bob)
		if d := bt.Position.Sub(lin.V2(0, 5)).Len(); !near(d, 2, 0.05) {
			t.Fatalf("frame %d: pendulum length %.3f, want 2", i, d)
		}
	}
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10), Iterations: 12})
	w.AddSystem("phys", System2)
	var links []ecs.Entity
	prev := ecs.None
	anchorA := lin.V2(0, 8)
	for i := range 5 {
		body := Dynamic2(1)
		body.LinearDamping, body.AngularDamping = 2, 2
		link := w.SpawnWith(gfx.At2(0.5+float32(i), 8), body, Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.1}, Layers: Layers{Layer: 2, Mask: 1}})
		w.SpawnWith(RevoluteJoint2{A: prev, AnchorA: anchorA, B: link, AnchorB: lin.V2(-0.5, 0)})
		anchorA = lin.V2(0.5, 0)
		links = append(links, link)
		prev = link
	}
	run(w, 8)
	for i, link := range links {
		lt, _ := ecs.Get[gfx.Transform2](w, link)
		if !near(lt.Position.Y, 8-0.5-float32(i), 0.15) || abs32(lt.Position.X) > 0.15 {
			t.Errorf("link %d hangs at %v", i, lt.Position)
		}
	}
	// Weld and spring.
	w = ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 10, HalfH: 0.5}})
	a := w.SpawnWith(gfx.At2(0, 4), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	b := w.SpawnWith(gfx.At2(1, 4), Dynamic2(1), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	w.SpawnWith(FixedJoint2{A: a, B: b, AnchorA: lin.V2(0.5, 0), AnchorB: lin.V2(-0.5, 0)})
	m := w.SpawnWith(gfx.At2(5, 3), Dynamic2(1), Collider2{Shape: Circle{0.1}})
	w.SpawnWith(SpringJoint2{A: ecs.None, AnchorA: lin.V2(5, 5), B: m, Stiffness: 50, Damping: 5})
	run(w, 6)
	at, _ := ecs.Get[gfx.Transform2](w, a)
	bt, _ := ecs.Get[gfx.Transform2](w, b)
	mt, _ := ecs.Get[gfx.Transform2](w, m)
	if !near(bt.Position.Sub(at.Position).Len(), 1, 0.05) || !near(at.Position.Y, 1, 0.1) {
		t.Errorf("welded boxes at %v and %v", at.Position, bt.Position)
	}
	if !near(mt.Position.Y, 2.8, 0.05) {
		t.Errorf("spring mass settled at %v, want y 2.8", mt.Position)
	}
}

// TestCharacter3D walks a controller over a low step and into a wall.
func TestCharacter3D(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(20, 0.5, 20)}})
	w.SpawnWith(gfx.At(1, 0.65, 0), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.15, 2)}}) // 0.3 tall step
	w.SpawnWith(gfx.At(5, 2, 0), Collider3{Shape: Box3{Half: lin.V3(0.25, 1.5, 2)}})    // 3 tall wall
	w.SpawnWith(gfx.At(-4, 2, 0), Collider3{Shape: Box3{Half: lin.V3(0.25, 1.5, 2)}})   // a wall behind
	ctrl := CharacterController3{Radius: 0.4, HalfHeight: 0.5, StepHeight: 0.5}
	e := w.SpawnWith(gfx.At(-2, 1.5, 0))
	w.Update(step)
	climbed := false
	for range 240 {
		ctrl.Move(w, e, lin.V3(2, -5, 0), step)
		tr, _ := ecs.Get[gfx.Transform](w, e)
		if tr.Position.X > 0.7 && tr.Position.X < 1.3 && tr.Position.Y > 1.65 {
			climbed = true
		}
	}
	tr, _ := ecs.Get[gfx.Transform](w, e)
	if !climbed {
		t.Errorf("the character did not climb the step: %v", tr.Position)
	}
	if tr.Position.X < 4 || tr.Position.X > 4.4 || !near(tr.Position.Y, 1.4, 0.05) || !ctrl.Grounded {
		t.Errorf("the character should stand against the wall at x 4.3, y 1.4: %v grounded=%v", tr.Position, ctrl.Grounded)
	}
	// Moving into the wall at an angle slides along it.
	ctrl.Move(w, e, lin.V3(2, 0, 2), 0.5)
	tr2, _ := ecs.Get[gfx.Transform](w, e)
	if tr2.Position.Z < 0.9 || tr2.Position.X > 4.4 {
		t.Errorf("the character should slide along the wall: %v", tr2.Position)
	}
}

// TestCharacter2D walks the 2D controller over a step and into a wall.
func TestCharacter2D(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Box2{HalfW: 20, HalfH: 0.5}})
	w.SpawnWith(gfx.At2(1, 0.65), Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.15}})
	w.SpawnWith(gfx.At2(5, 2), Collider2{Shape: Box2{HalfW: 0.25, HalfH: 1.5}})
	ctrl := CharacterController2{Radius: 0.4, HalfHeight: 0.5, StepHeight: 0.5}
	e := w.SpawnWith(gfx.At2(-2, 1.5))
	w.Update(step)
	climbed := false
	for range 240 {
		ctrl.Move(w, e, lin.V2(2, -5), step)
		tr, _ := ecs.Get[gfx.Transform2](w, e)
		if tr.Position.X > 0.7 && tr.Position.X < 1.3 && tr.Position.Y > 1.65 {
			climbed = true
		}
	}
	tr, _ := ecs.Get[gfx.Transform2](w, e)
	if !climbed {
		t.Errorf("the character did not climb the step: %v", tr.Position)
	}
	if tr.Position.X < 4 || tr.Position.X > 4.4 || !near(tr.Position.Y, 1.4, 0.05) || !ctrl.Grounded {
		t.Errorf("the character should stand against the wall at x 4.3, y 1.4: %v grounded=%v", tr.Position, ctrl.Grounded)
	}
}

// TestCollisionImpulse checks that a landing reports a normal impulse.
func TestCollisionImpulse(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(10, 0.5, 10)}})
	w.SpawnWith(gfx.At(0, 3, 0), Dynamic3(2), Collider3{Shape: Sphere{0.5}})
	var best float32
	for range 120 {
		w.Update(step)
		for _, ev := range ecs.Events[Collision3](w) {
			best = max(best, ev.Impulse)
		}
	}
	// Falling 2 units gives speed √40 ≈ 6.3; mass 2 gives an impulse near 12.6.
	if best < 8 || best > 16 {
		t.Errorf("landing impulse %.2f, want about 12.6", best)
	}
	if math.IsNaN(float64(best)) {
		t.Error("impulse is NaN")
	}
}
