package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
)

// TestLedgeLanding3D drops tumbling cubes onto the top edge of a long
// thin wall, as the physics3d example does. A cube must never move faster
// than it hit, and must end up resting on the wall or the floor: an
// edge-edge contact placed at a far support vertex, or a face manifold
// that accepted corners hanging past the ledge, used to catapult them.
func TestLedgeLanding3D(t *testing.T) {
	random := rng.New(7)
	for trial := range 40 {
		w := ecs.NewWorld()
		ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -9.8, 0), Substeps: 4, Iterations: 8})
		w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(30, 0.5, 30)}})
		w.SpawnWith(gfx.Transform{Position: lin.V3(12, 1.5, 0)}, Collider3{Shape: Box3{Half: lin.V3(0.5, 1.5, 12)}})
		w.AddSystem("physics", System3)
		body := Dynamic3(1)
		body.Friction, body.Restitution = 0.6, 0.05
		x := 12 + random.Between(-0.9, 0.9)
		height := 4 + random.Between(0, 6)
		rot := lin.AxisAngle(lin.V3(random.Float(), random.Float(), random.Float()).Norm(), random.Float()*3)
		e := w.SpawnWith(gfx.Transform{Position: lin.V3(x, height, random.Between(-2, 2)), Rotation: rot}, body, Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
		impact := float32(math.Sqrt(float64(2 * 9.8 * height)))
		var maxSpeed float32
		for range 600 {
			w.Update(1.0 / 60)
			b, _ := ecs.Get[Body3](w, e)
			maxSpeed = max(maxSpeed, b.Vel.Len())
		}
		tr, _ := ecs.Get[gfx.Transform](w, e)
		b, _ := ecs.Get[Body3](w, e)
		if maxSpeed > impact*1.1 {
			t.Errorf("trial %d: cube reached %.1f, faster than its impact speed %.1f", trial, maxSpeed, impact)
		}
		if tr.Position.Y < 0.9 || math.Abs(float64(tr.Position.X)) > 20 || math.Abs(float64(tr.Position.Z)) > 20 {
			t.Errorf("trial %d: cube ended at %v, not resting near the wall", trial, tr.Position)
		}
		if b.Vel.Len() > 0.5 {
			t.Errorf("trial %d: cube still moving at %.2f after ten seconds", trial, b.Vel.Len())
		}
	}
}

// TestLedgeManifold3D checks the face manifold of a cube hanging over the
// edge of a ledge: only the part of the cube above the ledge's top face
// may produce contacts, so no contact point lies past the ledge's side.
func TestLedgeManifold3D(t *testing.T) {
	ledge := Box3{Half: lin.V3(0.5, 1.5, 12)}
	cube := Box3{Half: lin.V3(0.5, 0.5, 0.5)}
	identity := mat3FromQuat(lin.Quat{W: 1})
	// A flat cube sunk 0.05 into the top, overhanging the side by 0.9:
	// the top face is the reference and only the strip over the ledge
	// (x from 0.4 to 0.5) may contribute points.
	cs := collide3(ledge, lin.V3(0, 0, 0), identity, cube, lin.V3(0.9, 1.95, 0), identity)
	if len(cs) != 4 {
		t.Fatalf("flat overhang: got %d contacts, want 4", len(cs))
	}
	for _, c := range cs {
		if c.point.X > 0.5+1e-3 {
			t.Errorf("contact at %v lies past the ledge's side", c.point)
		}
		if math.Abs(float64(c.depth-0.05)) > 1e-3 || c.normal.Y < 0.99 {
			t.Errorf("contact at %v: depth %.3f normal %v, want 0.05 along +y", c.point, c.depth, c.normal)
		}
	}
	// The same cube tilted so its bottom face rests on the ledge's corner
	// edge. The contacts must sit on that corner beside the cube (the
	// ledge's edge clipped to the cube's width gives its two ends), not at
	// the far end of the 24-unit ledge.
	rot := mat3FromQuat(lin.AxisAngle(lin.V3(0, 0, 1), -0.1))
	cs = collide3(ledge, lin.V3(0, 0, 0), identity, cube, lin.V3(0.9, 1.95, 0), rot)
	if len(cs) < 1 || len(cs) > 2 {
		t.Fatalf("tilted on corner: got %d contacts, want 1 or 2: %+v", len(cs), cs)
	}
	for _, c := range cs {
		if math.Abs(float64(c.point.X-0.5)) > 0.05 || math.Abs(float64(c.point.Y-1.5)) > 0.05 || math.Abs(float64(c.point.Z)) > 0.5+1e-3 {
			t.Errorf("corner contact at %v, want the ledge corner beside the cube", c.point)
		}
		if c.depth < 0 || c.depth > 0.05 {
			t.Errorf("corner contact depth %.3f, want shallow", c.depth)
		}
	}
}

// TestClosestOnSegments checks the segment-segment closest points used by
// edge-edge box contacts, including the clamped ends.
func TestClosestOnSegments(t *testing.T) {
	// Crossed perpendicular segments at height 0 and 1.
	a, b := closestOnSegments(lin.V3(0, 0, 0), lin.V3(1, 0, 0), 5, lin.V3(2, 1, 0), lin.V3(0, 0, 1), 5)
	if a.Sub(lin.V3(2, 0, 0)).Len() > 1e-5 || b.Sub(lin.V3(2, 1, 0)).Len() > 1e-5 {
		t.Errorf("crossed: got %v and %v", a, b)
	}
	// The second segment is short and off to the side, so its end clamps.
	a, b = closestOnSegments(lin.V3(0, 0, 0), lin.V3(1, 0, 0), 5, lin.V3(8, 1, 0), lin.V3(0, 0, 1), 1)
	if a.Sub(lin.V3(5, 0, 0)).Len() > 1e-5 || b.Sub(lin.V3(8, 1, 0)).Len() > 1e-5 {
		t.Errorf("clamped: got %v and %v", a, b)
	}
}
