package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

func cacheCompound3() Compound3 {
	return Compound3{Parts: []Part3{
		{Shape: Sphere{Radius: 1}, Offset: lin.V3(-2, 0, 0)},
		{Shape: Sphere{Radius: 1}, Offset: lin.V3(2, 0, 0)},
	}}
}

func TestShapeCache3Replacement(t *testing.T) {
	w := ecs.NewWorld()
	s := cacheCompound3()
	e := w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
	point := lin.V3(0, 3, 0)
	h, ok := Nearest3(w, point, 10, 0)
	if !ok || math.Abs(float64(h.Distance)-(math.Sqrt(13)-1)) > 1e-5 {
		t.Fatalf("initial nearest = %+v, %v", h, ok)
	}
	s.Parts = append(s.Parts, Part3{Shape: Sphere{Radius: 1}})
	ecs.Add(w, e, Collider3{Shape: s})
	h, ok = Nearest3(w, point, 10, 0)
	if !ok || math.Abs(float64(h.Distance-2)) > 1e-5 {
		t.Errorf("replacement nearest = %+v, %v; want distance 2", h, ok)
	}
	h, ok = ShapeCast3(w, Sphere{Radius: 0.25}, point, lin.Quat{}, lin.V3(0, -3, 0), 0)
	if !ok || math.Abs(float64(h.Distance-1.75/3)) > 1e-4 {
		t.Errorf("replacement cast = %+v, %v; want fraction %v", h, ok, 1.75/3)
	}
}

func TestShapeCache3NestedEdits(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"small_hull", 6}, {"large_hull", convexBuf + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			// The axial extremes fix the bounds; the diagonal vertex changes
			// the surface nearest the query without changing those bounds.
			points := make([]lin.Vec3, tc.size)
			copy(points, []lin.Vec3{lin.V3(-1, 0, 0), lin.V3(1, 0, 0), lin.V3(0, -1, 0), lin.V3(0, 1, 0), lin.V3(0, 0, -1), lin.V3(0, 0, 1)})
			points = append(points, lin.Vec3{})
			inner := Compound3{Parts: []Part3{{Shape: ConvexHull{Points: points}}}}
			outer := Compound3{Parts: []Part3{{Shape: inner}}}
			w := ecs.NewWorld()
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: outer})
			point := lin.V3(3, 3, 0)
			before, _ := Nearest3(w, point, 10, 0)
			points[len(points)-1] = lin.V3(1, 1, 0)
			after, ok := Nearest3(w, point, 10, 0)
			if !ok || math.Abs(float64(after.Distance)-math.Sqrt(8)) > 1e-4 || after.Distance >= before.Distance {
				t.Errorf("nested hull edit: before=%+v after=%+v, %v; want sqrt(8)", before, after, ok)
			}
			// Editing a nested child shape must also be seen without reassignment.
			inner.Parts[0].Shape = Sphere{Radius: 1}
			after, ok = Nearest3(w, point, 10, 0)
			if !ok || math.Abs(float64(after.Distance)-(math.Sqrt(18)-1)) > 1e-4 {
				t.Errorf("nested child replacement = %+v, %v", after, ok)
			}
		})
	}
}

func TestShapeCache3EntityReuse(t *testing.T) {
	w := ecs.NewWorld()
	s := cacheCompound3()
	e := w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
	Nearest3(w, lin.V3(0, 3, 0), 10, 0)
	w.Despawn(e)
	s.Parts = append(s.Parts, Part3{Shape: Sphere{Radius: 1}})
	reused := w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
	if slot(reused) != slot(e) || reused == e {
		t.Fatalf("expected generational slot reuse: old=%v new=%v", e, reused)
	}
	h, ok := Nearest3(w, lin.V3(0, 3, 0), 10, 0)
	if !ok || h.Entity != reused || math.Abs(float64(h.Distance-2)) > 1e-5 {
		t.Errorf("reused entity nearest = %+v, %v; want new entity distance 2", h, ok)
	}
}

func TestShapeCache3PartTransforms(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shape  Shape3
		mutate func(*Part3)
		want   float64
	}{
		{"offset", Sphere{Radius: 0.25}, func(p *Part3) { p.Offset.Y = 0.5 }, 2.25},
		{"rotation", Capsule{Radius: 0.25, HalfHeight: 0.5}, func(p *Part3) {
			p.Rotation = lin.AxisAngle(lin.V3(0, 0, 1), math.Pi/2)
		}, math.Sqrt(13) - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := Compound3{Parts: []Part3{{Shape: tc.shape}}}
			s := cacheCompound3()
			s.Parts = append(s.Parts, Part3{Shape: inner})
			w := ecs.NewWorld()
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
			pos, rot := lin.Vec3{}, mat3FromQuat(lin.Quat{})
			lo, hi := s.bounds(pos, rot)
			before, _ := Nearest3(w, lin.V3(0, 3, 0), 10, 0)
			tc.mutate(&inner.Parts[0])
			if afterLo, afterHi := s.bounds(pos, rot); afterLo != lo || afterHi != hi {
				t.Fatal("test edit changed the outer bounds")
			}
			after, ok := Nearest3(w, lin.V3(0, 3, 0), 10, 0)
			if !ok || math.Abs(float64(after.Distance)-tc.want) > 1e-4 || before.Distance == after.Distance {
				t.Errorf("nested transform edit: before=%+v after=%+v, %v; want %v", before, after, ok, tc.want)
			}
		})
	}
}

func TestShapeCache3ControllerReplacement(t *testing.T) {
	w := ecs.NewWorld()
	s := cacheCompound3()
	e := w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
	Nearest3(w, lin.V3(0, 3, 0), 10, 0)
	s.Parts = append(s.Parts, Part3{Shape: Sphere{Radius: 1}})
	ecs.Add(w, e, Collider3{Shape: s})
	character := w.SpawnWith(gfx.At(0, 3, 0))
	c := CharacterController3{Radius: 0.25, HalfHeight: 0.25}
	c.Move(w, character, lin.V3(0, -3, 0), 1)
	tr, _ := ecs.Get[gfx.Transform](w, character)
	if !c.Grounded || math.Abs(float64(tr.Position.Y-1.52)) > 1e-3 {
		t.Errorf("controller after replacement: position=%v grounded=%v; want y=1.52 grounded", tr.Position, c.Grounded)
	}
}

func TestShapeCache3UnchangedAllocNothing(t *testing.T) {
	points := make([]lin.Vec3, convexBuf+10)
	for i := range points {
		a := 2 * math.Pi * float64(i) / float64(len(points))
		points[i] = lin.V3(float32(math.Cos(a)), float32(math.Sin(a)), 0)
	}
	s := Compound3{Parts: []Part3{{Shape: ConvexHull{Points: points}}, {Shape: cacheCompound3()}}}
	w := ecs.NewWorld()
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: s})
	run := func() {
		Nearest3(w, lin.V3(0, 3, 0), 10, 0)
		ShapeCast3(w, Sphere{Radius: 0.25}, lin.V3(0, 3, 0), lin.Quat{}, lin.V3(0, -3, 0), 0)
	}
	run()
	if n := testing.AllocsPerRun(50, run); n != 0 {
		t.Errorf("unchanged nested geometry allocates %v times per query pair", n)
	}
}
