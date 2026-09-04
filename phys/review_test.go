package phys_test

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
)

// Pushing one body of a sleeping jointed pair wakes the other through
// the joint, as a contact would.
func TestJointWakesPartner(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0), SleepTime: 0.3})
	w.AddSystem("phys", phys.System3)
	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(20, 0.5, 20)}})
	a := w.SpawnWith(gfx.At(0, 1.0, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	b := w.SpawnWith(gfx.At(3, 1.0, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	w.SpawnWith(phys.DistanceJoint3{A: a, B: b, Length: 3})
	for range 240 {
		w.Update(1.0 / 60)
	}
	ba, _ := ecs.Get[phys.Body3](w, a)
	bb, _ := ecs.Get[phys.Body3](w, b)
	if !ba.Asleep() || !bb.Asleep() {
		t.Fatalf("expected both asleep: a=%v b=%v", ba.Asleep(), bb.Asleep())
	}
	ba.AddImpulse(lin.V3(0, 0, 8))
	for range 30 {
		w.Update(1.0 / 60)
	}
	bb, _ = ecs.Get[phys.Body3](w, b)
	if bb.Asleep() {
		t.Error("the joint partner of a pushed body stayed asleep")
	}
}

// A sphere that clips the edge of a mesh triangle is not thrown upward
// by a contact measured from the face plane.
func TestMeshEdgeDoesNotLaunch(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
	w.AddSystem("phys", phys.System3)
	verts := []lin.Vec3{lin.V3(-5, 0, -5), lin.V3(0, 0, -5), lin.V3(0, 0, 5), lin.V3(-5, 0, 5)}
	idx := []uint32{0, 1, 2, 0, 2, 3}
	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.NewMeshShape(verts, idx)})
	body := phys.Dynamic3(1)
	body.Restitution = 0
	e := w.SpawnWith(gfx.At(0.3, 3, 0), body, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	var maxUp float32
	for range 240 {
		w.Update(1.0 / 60)
		b, _ := ecs.Get[phys.Body3](w, e)
		maxUp = max(maxUp, b.Vel.Y)
	}
	if maxUp > 1 {
		t.Errorf("sphere launched upward at %.2f m/s off a mesh edge", maxUp)
	}
}
