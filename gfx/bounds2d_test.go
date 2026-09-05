package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestTransform2ApplyCentresSprite(t *testing.T) {
	for _, rotation := range []float32{0, math.Pi / 3} {
		tr := Transform2{Position: lin.V2(100, 100), Rotation: rotation, Scale: lin.V2(2, 3)}
		s := tr.Apply(Sprite{Size: lin.V2(20, 10)})
		corners := s.Corners()
		var centre lin.Vec2
		for _, p := range corners {
			centre = centre.Add(p.Mul(0.25))
		}
		if centre.Sub(tr.Position).Len() > 0.0001 {
			t.Errorf("rotation %g: centre %v, want %v", rotation, centre, tr.Position)
		}
	}
}

func TestSpriteBoundsAndCorners(t *testing.T) {
	s := Sprite{Pos: lin.V2(10, 20), Size: lin.V2(-8, 4), Origin: lin.V2(0.5, 0.5), Rotation: math.Pi / 2}
	want := lin.R(8, 16, 4, 8)
	if b := s.Bounds(); math.Abs(float64(b.X-want.X))+math.Abs(float64(b.Y-want.Y))+math.Abs(float64(b.W-want.W))+math.Abs(float64(b.H-want.H)) > 0.0001 {
		t.Fatalf("bounds %v, want %v", b, want)
	}
	p := s.Corners()
	s.FlipX, s.FlipY = true, true
	if s.Corners() != p {
		t.Fatal("texture flips changed geometry")
	}
}

func TestPathBoundsCurveExtrema(t *testing.T) {
	for _, tc := range []struct {
		name string
		path *Path
		want lin.Rect
	}{
		{"empty", &Path{}, lin.Rect{}},
		{"point", new(Path).MoveTo(4, -2), lin.R(4, -2, 0, 0)},
		{"quadratic", new(Path).MoveTo(0, 0).QuadTo(10, 20, 20, 0), lin.R(0, 0, 20, 10)},
		{"cubic", new(Path).MoveTo(0, 0).CubicTo(0, 20, 20, 20, 20, 0), lin.R(0, 0, 20, 15)},
		{"cubic inflection", new(Path).MoveTo(0, 0).CubicTo(0, 30, 30, -30, 30, 0), lin.R(0, -float32(5*math.Sqrt(3)), 30, float32(10*math.Sqrt(3)))},
		{"closed", new(Path).MoveTo(-5, 7).LineTo(3, -2).Close().MoveTo(10, 20), lin.R(-5, -2, 15, 22)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.path.Bounds()
			if math.Abs(float64(b.X-tc.want.X))+math.Abs(float64(b.Y-tc.want.Y))+math.Abs(float64(b.W-tc.want.W))+math.Abs(float64(b.H-tc.want.H)) > 0.0001 {
				t.Fatalf("bounds %v, want %v", b, tc.want)
			}
		})
	}
}
