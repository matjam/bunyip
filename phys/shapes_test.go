package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// flatMesh is a square of ground at height y made of triangles.
func flatMesh(size, y float32, cells int) MeshShape {
	var verts []lin.Vec3
	var idx []uint32
	for j := 0; j <= cells; j++ {
		for i := 0; i <= cells; i++ {
			verts = append(verts, lin.V3(-size+2*size*float32(i)/float32(cells), y, -size+2*size*float32(j)/float32(cells)))
		}
	}
	for j := 0; j < cells; j++ {
		for i := 0; i < cells; i++ {
			a := uint32(j*(cells+1) + i)
			b := a + uint32(cells) + 1
			idx = append(idx, a, b, a+1, a+1, b, b+1)
		}
	}
	return NewMeshShape(verts, idx)
}

func octahedron(r float32) ConvexHull {
	return ConvexHull{Points: []lin.Vec3{{X: r}, {X: -r}, {Y: r}, {Y: -r}, {Z: r}, {Z: -r}}}
}

func near(a, b, tol float32) bool { return math.Abs(float64(a-b)) <= float64(tol) }

// TestPenetration3D checks the support-function collision on known
// placements: distances, normals and depths.
func TestPenetration3D(t *testing.T) {
	id := mat3FromQuat(lin.QuatIdentity())
	cases := []struct {
		name   string
		a      Shape3
		pa     lin.Vec3
		b      Shape3
		pb     lin.Vec3
		depth  float32
		normal lin.Vec3
		hit    bool
	}{
		{"sphere above capsule", Sphere{0.5}, lin.V3(0, 1.9, 0), Capsule{0.5, 1}, lin.Vec3{}, 0.1, lin.V3(0, -1, 0), true},
		{"sphere beside capsule", Sphere{0.5}, lin.V3(0.9, 0.3, 0), Capsule{0.5, 1}, lin.Vec3{}, 0.1, lin.V3(-1, 0, 0), true},
		{"sphere clear of capsule", Sphere{0.5}, lin.V3(1.1, 0, 0), Capsule{0.5, 1}, lin.Vec3{}, 0, lin.Vec3{}, false},
		{"capsule on box", Capsule{0.5, 1}, lin.V3(0, 1.95, 0), Box3{Half: lin.V3(5, 0.5, 5)}, lin.Vec3{}, 0.05, lin.V3(0, -1, 0), true},
		{"hull on box", octahedron(1), lin.V3(0, 1.4, 0), Box3{Half: lin.V3(5, 0.5, 5)}, lin.Vec3{}, 0.1, lin.V3(0, -1, 0), true},
		{"hull clear of box", octahedron(1), lin.V3(0, 1.6, 0), Box3{Half: lin.V3(5, 0.5, 5)}, lin.Vec3{}, 0, lin.Vec3{}, false},
		{"box on hull", Box3{Half: lin.V3(0.5, 0.5, 0.5)}, lin.V3(0, 1.4, 0), octahedron(1), lin.Vec3{}, 0.1, lin.V3(0, -1, 0), true},
		// Tip to tip, the shortest way out is along a face normal: 0.1/√3.
		{"hull hull", octahedron(1), lin.V3(1.9, 0, 0), octahedron(1), lin.Vec3{}, 0.0577, lin.V3(-1, -1, 1).Norm(), true},
		{"sphere on hull", Sphere{0.5}, lin.V3(0, 1.4, 0), octahedron(1), lin.Vec3{}, 0.1, lin.V3(0, -1, 0), true},
	}
	for _, c := range cases {
		cs := collide3(new(scratch3), nil, c.a, c.pa, id, c.b, c.pb, id)
		if !c.hit {
			if len(cs) != 0 {
				t.Errorf("%s: got %d contacts, want none: %+v", c.name, len(cs), cs)
			}
			continue
		}
		if len(cs) == 0 {
			t.Errorf("%s: no contacts", c.name)
			continue
		}
		deepest := cs[0]
		for _, k := range cs {
			if k.depth > deepest.depth {
				deepest = k
			}
		}
		if !near(deepest.depth, c.depth, 0.02) || deepest.normal.Dot(c.normal) < 0.5 {
			t.Errorf("%s: depth %.3f normal %v, want %.3f along %v (%d contacts)", c.name, deepest.depth, deepest.normal, c.depth, c.normal, len(cs))
		}
	}
}

// TestRestOnShapes3D drops each kind of body onto each kind of ground and
// checks it comes to rest at the expected height.
func TestRestOnShapes3D(t *testing.T) {
	grounds := []struct {
		name  string
		shape Shape3
		top   float32
	}{
		{"box", Box3{Half: lin.V3(10, 0.5, 10)}, 0.5},
		{"mesh", flatMesh(10, 0.5, 4), 0.5},
		{"compound", Compound3{Parts: []Part3{{Shape: Box3{Half: lin.V3(5, 0.5, 10)}, Offset: lin.V3(-5, 0, 0)}, {Shape: Box3{Half: lin.V3(5, 0.5, 10)}, Offset: lin.V3(5, 0, 0)}}}, 0.5},
	}
	bodies := []struct {
		name   string
		shape  Shape3
		rot    lin.Quat
		height float32 // of the centre when resting
	}{
		{"sphere", Sphere{0.5}, lin.Quat{}, 0.5},
		{"box", Box3{Half: lin.V3(0.5, 0.5, 0.5)}, lin.Quat{}, 0.5},
		{"standing capsule", Capsule{0.3, 0.5}, lin.Quat{}, 0.8},
		{"lying capsule", Capsule{0.3, 0.5}, lin.AxisAngle(lin.V3(0, 0, 1), math.Pi/2), 0.3},
		{"hull", ConvexHull{Points: []lin.Vec3{{X: -0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: -0.5, Z: 0.5}, {X: -0.5, Y: -0.5, Z: 0.5}, {X: 0, Y: 0.5, Z: 0}}}, lin.Quat{}, 0.5},
		{"compound", Compound3{Parts: []Part3{{Shape: Box3{Half: lin.V3(0.5, 0.25, 0.5)}}, {Shape: Sphere{0.25}, Offset: lin.V3(0, 0.5, 0)}}}, lin.Quat{}, 0.25},
	}
	for _, g := range grounds {
		for _, b := range bodies {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0), Substeps: 4, Iterations: 8})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: g.shape})
			body := Dynamic3(1)
			body.Restitution = 0
			e := w.SpawnWith(gfx.Transform{Position: lin.V3(1, 3, 1), Rotation: b.rot}, body, Collider3{Shape: b.shape})
			run(w, 4)
			tr, _ := w.Get[gfx.Transform](e)
			bd, _ := w.Get[Body3](e)
			want := g.top + b.height
			if !near(tr.Position.Y, want, 0.08) || bd.Vel.Len() > 0.1 {
				t.Errorf("%s on %s: rests at %v (vel %v), want y %.2f", b.name, g.name, tr.Position, bd.Vel, want)
			}
		}
	}
}

// TestRaycastShapes3D casts rays at the new shapes.
func TestRaycastShapes3D(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("phys", System3)
	capsule := w.SpawnWith(gfx.At(0, 0, 0), Collider3{Shape: Capsule{0.5, 1}})
	hull := w.SpawnWith(gfx.At(5, 0, 0), Collider3{Shape: octahedron(1)})
	mesh := w.SpawnWith(gfx.At(0, -2, 0), Collider3{Shape: flatMesh(20, 0, 2)})
	compound := w.SpawnWith(gfx.At(-5, 0, 0), Collider3{Shape: Compound3{Parts: []Part3{{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}, Offset: lin.V3(0, 1, 0)}}}})
	w.Update(step)
	cases := []struct {
		name   string
		ray    Ray3
		entity ecs.Entity
		point  lin.Vec3
	}{
		{"capsule side", Ray3{Origin: lin.V3(-3, 0.5, 0), Dir: lin.V3(6, 0, 0)}, capsule, lin.V3(-0.5, 0.5, 0)},
		{"capsule cap", Ray3{Origin: lin.V3(0, 5, 0), Dir: lin.V3(0, -4, 0)}, capsule, lin.V3(0, 1.5, 0)},
		{"hull tip", Ray3{Origin: lin.V3(5, 5, 0), Dir: lin.V3(0, -4, 0)}, hull, lin.V3(5, 1, 0)},
		{"mesh", Ray3{Origin: lin.V3(3, 5, 3), Dir: lin.V3(0, -10, 0)}, mesh, lin.V3(3, -2, 3)},
		{"compound", Ray3{Origin: lin.V3(-5, 5, 0), Dir: lin.V3(0, -4, 0)}, compound, lin.V3(-5, 1.5, 0)},
	}
	for _, c := range cases {
		hit, ok := Raycast3(w, c.ray, 0)
		if !ok || hit.Entity != c.entity || hit.Point.Sub(c.point).Len() > 0.03 {
			t.Errorf("%s: %+v ok=%v, want %v on %v", c.name, hit, ok, c.point, c.entity)
		}
	}
}

// TestShapes2D rests capsules, boxes and circles on the 2D terrain shapes.
func TestShapes2D(t *testing.T) {
	grounds := []struct {
		name  string
		shape Shape2
		top   float32
	}{
		{"box", Box2{HalfW: 10, HalfH: 0.5}, 0.5},
		{"edge", Edge2{A: lin.V2(-10, 0.5), B: lin.V2(10, 0.5)}, 0.5},
		{"chain", Chain2{Points: []lin.Vec2{{X: -10, Y: 3}, {X: -10, Y: 0.5}, {X: 0, Y: 0.5}, {X: 10, Y: 0.5}, {X: 10, Y: 3}}}, 0.5},
	}
	bodies := []struct {
		name   string
		shape  Shape2
		rot    float32
		height float32
	}{
		{"circle", Circle{0.5}, 0, 0.5},
		{"box", Box2{HalfW: 0.5, HalfH: 0.5}, 0, 0.5},
		{"standing capsule", Capsule2{0.3, 0.5}, 0, 0.8},
		{"lying capsule", Capsule2{0.3, 0.5}, math.Pi / 2, 0.3},
	}
	for _, g := range grounds {
		for _, b := range bodies {
			w := ecs.NewWorld()
			w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
			w.AddSystem("phys", System2)
			w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: g.shape})
			body := Dynamic2(1)
			body.Restitution = 0
			e := w.SpawnWith(gfx.Transform2{Position: lin.V2(1, 3), Rotation: b.rot}, body, Collider2{Shape: b.shape})
			run(w, 4)
			tr, _ := w.Get[gfx.Transform2](e)
			bd, _ := w.Get[Body2](e)
			want := g.top + b.height
			if !near(tr.Position.Y, want, 0.08) || bd.Vel.Len() > 0.1 {
				t.Errorf("%s on %s: rests at %v (vel %v), want y %.2f", b.name, g.name, tr.Position, bd.Vel, want)
			}
		}
	}
	// A ray finds the chain and the capsule.
	w := ecs.NewWorld()
	w.AddSystem("phys", System2)
	chain := w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Chain2{Points: []lin.Vec2{{X: -10, Y: 0}, {X: 0, Y: 2}, {X: 10, Y: 0}}}})
	capsule := w.SpawnWith(gfx.At2(0, 5), Collider2{Shape: Capsule2{0.5, 1}})
	w.Update(step)
	hit, ok := Raycast2(w, Ray2{Origin: lin.V2(5, 10), Dir: lin.V2(0, -20)}, 0)
	if !ok || hit.Entity != chain || !near(hit.Point.Y, 1, 0.01) {
		t.Errorf("chain ray: %+v ok=%v", hit, ok)
	}
	hit, ok = Raycast2(w, Ray2{Origin: lin.V2(-5, 5.5), Dir: lin.V2(10, 0)}, 0)
	if !ok || hit.Entity != capsule || !near(hit.Point.X, -0.5, 0.01) {
		t.Errorf("capsule ray: %+v ok=%v", hit, ok)
	}
}
