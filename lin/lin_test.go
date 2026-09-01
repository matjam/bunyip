package lin

import (
	"math"
	"testing"
)

func near(a, b Vec3) bool {
	const eps = 1e-4
	return math.Abs(float64(a.X-b.X)) < eps && math.Abs(float64(a.Y-b.Y)) < eps && math.Abs(float64(a.Z-b.Z)) < eps
}

func TestTransforms(t *testing.T) {
	cases := []struct {
		name string
		m    Mat4
		in   Vec3
		want Vec3
	}{
		{"translate", Translate(V3(1, 2, 3)), V3(0, 0, 0), V3(1, 2, 3)},
		{"scale", Scale(V3(2, 3, 4)), V3(1, 1, 1), V3(2, 3, 4)},
		{"rotate z 90", Rotate(Radians(90), V3(0, 0, 1)), V3(1, 0, 0), V3(0, 1, 0)},
		{"rotate y 90", Rotate(Radians(90), V3(0, 1, 0)), V3(1, 0, 0), V3(0, 0, -1)},
		{"trs order", TRS(V3(10, 0, 0), AxisAngle(V3(0, 0, 1), Radians(90)), V3(2, 2, 2)), V3(1, 0, 0), V3(10, 2, 0)},
		{"quat rotate", AxisAngle(V3(0, 0, 1), Radians(90)).Mat4(), V3(1, 0, 0), V3(0, 1, 0)},
		{"lookat origin", LookAt(V3(0, 0, 5), V3(0, 0, 0), V3(0, 1, 0)), V3(0, 0, 0), V3(0, 0, -5)},
		{"inverse", Translate(V3(1, 2, 3)).Mul(Rotate(1, V3(1, 1, 0))).Inverse().Mul(Translate(V3(1, 2, 3)).Mul(Rotate(1, V3(1, 1, 0)))), V3(4, 5, 6), V3(4, 5, 6)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.m.MulPoint(c.in); !near(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestOrthoTopLeft(t *testing.T) {
	// Screen space 800x600 with +Y down maps the top-left corner to (-1,-1)
	// and the bottom-right to (1,1) in Vulkan clip space.
	m := Ortho2D(800, 600)
	if got := m.MulPoint(V3(0, 0, 0)); !near(got, V3(-1, -1, 0.5)) {
		t.Errorf("top-left -> %v", got)
	}
	if got := m.MulPoint(V3(800, 600, 0)); !near(got, V3(1, 1, 0.5)) {
		t.Errorf("bottom-right -> %v", got)
	}
}

func TestOrthoDepth(t *testing.T) {
	// A right-handed view looks down -Z: the near plane maps to 0, far to 1.
	m := Ortho(-1, 1, -1, 1, 2, 10)
	if z := m.MulPoint(V3(0, 0, -2)).Z; math.Abs(float64(z)) > 1e-5 {
		t.Errorf("near plane -> %v, want 0", z)
	}
	if z := m.MulPoint(V3(0, 0, -10)).Z; math.Abs(float64(z-1)) > 1e-5 {
		t.Errorf("far plane -> %v, want 1", z)
	}
}

func TestPerspectiveDepth(t *testing.T) {
	p := Perspective(Radians(60), 4.0/3, 0.1, 100)
	nearPt := p.MulVec4(V4(0, 0, -0.1, 1))
	farPt := p.MulVec4(V4(0, 0, -100, 1))
	if z := nearPt.Z / nearPt.W; math.Abs(float64(z)) > 1e-4 {
		t.Errorf("near plane depth %v, want 0", z)
	}
	if z := farPt.Z / farPt.W; math.Abs(float64(z-1)) > 1e-4 {
		t.Errorf("far plane depth %v, want 1", z)
	}
	// A point above the camera's centre line ends up with negative clip Y (screen up).
	up := p.MulVec4(V4(0, 1, -5, 1))
	if up.Y/up.W >= 0 {
		t.Errorf("up in view space should be -Y in clip space, got %v", up.Y/up.W)
	}
}
