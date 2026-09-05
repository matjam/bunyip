package phys

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestSignedDistance3(t *testing.T) {
	quarter := lin.AxisAngle(lin.V3(0, 1, 0), math.Pi/2)
	cases := []struct {
		name   string
		shape  Shape3
		pos    lin.Vec3
		rot    lin.Quat
		point  lin.Vec3
		dist   float32
		normal lin.Vec3
	}{
		{"sphere outside", Sphere{Radius: 1}, lin.Vec3{}, lin.Quat{}, lin.V3(0, 3, 0), 2, lin.V3(0, 1, 0)},
		{"sphere inside", Sphere{Radius: 1}, lin.Vec3{}, lin.Quat{}, lin.V3(0, 0.25, 0), -0.75, lin.V3(0, 1, 0)},
		{"sphere placed", Sphere{Radius: 1}, lin.V3(5, 0, 0), lin.Quat{}, lin.V3(7, 0, 0), 1, lin.V3(1, 0, 0)},
		{"box above face", Box3{Half: lin.V3(1, 1, 1)}, lin.Vec3{}, lin.Quat{}, lin.V3(0, 2.5, 0), 1.5, lin.V3(0, 1, 0)},
		{"box inside", Box3{Half: lin.V3(1, 2, 1)}, lin.Vec3{}, lin.Quat{}, lin.V3(0.75, 0, 0), -0.25, lin.V3(1, 0, 0)},
		{"box corner", Box3{Half: lin.V3(1, 1, 1)}, lin.Vec3{}, lin.Quat{}, lin.V3(2, 1, 1), 1, lin.V3(1, 0, 0)},
		{"box rotated", Box3{Half: lin.V3(2, 1, 1)}, lin.Vec3{}, quarter, lin.V3(0, 0, 3), 1, lin.V3(0, 0, 1)},
		{"capsule side", Capsule{Radius: 0.5, HalfHeight: 1}, lin.Vec3{}, lin.Quat{}, lin.V3(2, 0.5, 0), 1.5, lin.V3(1, 0, 0)},
		{"capsule cap", Capsule{Radius: 0.5, HalfHeight: 1}, lin.Vec3{}, lin.Quat{}, lin.V3(0, 2, 0), 0.5, lin.V3(0, 1, 0)},
		{"capsule inside", Capsule{Radius: 0.5, HalfHeight: 1}, lin.Vec3{}, lin.Quat{}, lin.V3(0.25, 0, 0), -0.25, lin.V3(1, 0, 0)},
		{"compound", Compound3{Parts: []Part3{{Shape: Sphere{Radius: 1}, Offset: lin.V3(3, 0, 0)}, {Shape: Sphere{Radius: 1}}}},
			lin.Vec3{}, lin.Quat{}, lin.V3(0, 2, 0), 1, lin.V3(0, 1, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, n, ok := SignedDistance3(c.shape, c.pos, c.rot, c.point)
			if !ok {
				t.Fatal("shape not understood")
			}
			if math.Abs(float64(d-c.dist)) > 1e-5 {
				t.Errorf("distance %v, want %v", d, c.dist)
			}
			if n.Sub(c.normal).Len() > 1e-5 {
				t.Errorf("normal %v, want %v", n, c.normal)
			}
		})
	}
	if _, _, ok := SignedDistance3(ConvexHull{Points: []lin.Vec3{{}}}, lin.Vec3{}, lin.Quat{}, lin.Vec3{}); ok {
		t.Error("a convex hull has no signed distance yet, but one was reported")
	}
	if _, _, ok := SignedDistance3(nil, lin.Vec3{}, lin.Quat{}, lin.Vec3{}); ok {
		t.Error("a nil shape reported a distance")
	}
}

// TestSignedDistance3Gradient checks that stepping along the normal by
// the distance lands on the surface, which is what a solver relies on.
func TestSignedDistance3Gradient(t *testing.T) {
	shapes := []Shape3{Sphere{Radius: 0.8}, Box3{Half: lin.V3(1, 0.5, 1.5)}, Capsule{Radius: 0.4, HalfHeight: 0.9}}
	rot := lin.AxisAngle(lin.V3(1, 1, 0).Norm(), 0.7)
	for _, s := range shapes {
		for _, p := range []lin.Vec3{{X: 2, Y: 1, Z: 0.5}, {X: -0.2, Y: 0.1, Z: 0.05}, {X: 0, Y: -3, Z: 0}, {X: 1.4, Y: 1.4, Z: 1.4}} {
			d, n, ok := SignedDistance3(s, lin.V3(0.5, 0, -0.5), rot, p)
			if !ok {
				t.Fatalf("%T not understood", s)
			}
			q := p.Sub(n.Mul(d))
			d2, _, _ := SignedDistance3(s, lin.V3(0.5, 0, -0.5), rot, q)
			if math.Abs(float64(d2)) > 1e-4 {
				t.Errorf("%T from %v: stepping %v along %v left distance %v", s, p, d, n, d2)
			}
		}
	}
}

func TestSignedDistance2(t *testing.T) {
	tri := Polygon2{Points: []lin.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 0, Y: 1}}}
	cases := []struct {
		name   string
		shape  Shape2
		pos    lin.Vec2
		rot    float32
		point  lin.Vec2
		dist   float32
		normal lin.Vec2
	}{
		{"circle outside", Circle{Radius: 2}, lin.Vec2{}, 0, lin.V2(5, 0), 3, lin.V2(1, 0)},
		{"circle inside", Circle{Radius: 2}, lin.Vec2{}, 0, lin.V2(0, 1), -1, lin.V2(0, 1)},
		{"box face", Box2{HalfW: 1, HalfH: 2}, lin.Vec2{}, 0, lin.V2(0, 5), 3, lin.V2(0, 1)},
		{"box inside", Box2{HalfW: 1, HalfH: 2}, lin.Vec2{}, 0, lin.V2(0.5, 0), -0.5, lin.V2(1, 0)},
		{"box corner", Box2{HalfW: 1, HalfH: 1}, lin.Vec2{}, 0, lin.V2(2, 2), float32(math.Sqrt2), lin.V2(0.70710678, 0.70710678)},
		{"box placed", Box2{HalfW: 1, HalfH: 1}, lin.V2(10, 10), 0, lin.V2(10, 12), 1, lin.V2(0, 1)},
		{"triangle inside", tri, lin.Vec2{}, 0, lin.V2(0, -0.5), -0.5, lin.V2(0, -1)},
		{"capsule side", Capsule2{Radius: 0.5, HalfHeight: 1}, lin.Vec2{}, 0, lin.V2(2, 0), 1.5, lin.V2(1, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, n, ok := SignedDistance2(c.shape, c.pos, c.rot, c.point)
			if !ok {
				t.Fatal("shape not understood")
			}
			if math.Abs(float64(d-c.dist)) > 1e-5 {
				t.Errorf("distance %v, want %v", d, c.dist)
			}
			if n.Sub(c.normal).Len() > 1e-5 {
				t.Errorf("normal %v, want %v", n, c.normal)
			}
		})
	}
	if _, _, ok := SignedDistance2(Edge2{A: lin.V2(-1, 0), B: lin.V2(1, 0)}, lin.Vec2{}, 0, lin.Vec2{}); ok {
		t.Error("an edge has no inside, but a signed distance was reported")
	}
}

func TestSignedDistance2Gradient(t *testing.T) {
	shapes := []Shape2{Circle{Radius: 0.8}, Box2{HalfW: 1, HalfH: 0.5},
		Polygon2{Points: []lin.Vec2{{X: -1, Y: -0.5}, {X: 1, Y: -0.5}, {X: 1.5, Y: 0.5}, {X: -0.5, Y: 0.8}}},
		Capsule2{Radius: 0.3, HalfHeight: 0.7}}
	for _, s := range shapes {
		for _, p := range []lin.Vec2{{X: 3, Y: 1}, {X: 0.1, Y: 0.1}, {X: -2, Y: -2}, {X: 0, Y: 4}} {
			d, n, ok := SignedDistance2(s, lin.V2(1, -1), 0.4, p)
			if !ok {
				t.Fatalf("%T not understood", s)
			}
			q := p.Sub(n.Mul(d))
			d2, _, _ := SignedDistance2(s, lin.V2(1, -1), 0.4, q)
			if math.Abs(float64(d2)) > 1e-4 {
				t.Errorf("%T from %v: stepping %v along %v left distance %v", s, p, d, n, d2)
			}
		}
	}
}
