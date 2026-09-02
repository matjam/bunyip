package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// triangleArea sums the signed-magnitude area of a vertex stream's
// opaque triangles, which for a fill is the area covered.
func triangleArea(verts []vertex2D, opaqueOnly bool) float64 {
	var area float64
	for i := 0; i+2 < len(verts); i += 3 {
		a, b, c := verts[i], verts[i+1], verts[i+2]
		if opaqueOnly && (a.color[3] < 1 || b.color[3] < 1 || c.color[3] < 1) {
			continue
		}
		area += math.Abs(float64((b.pos.X-a.pos.X)*(c.pos.Y-a.pos.Y)-(c.pos.X-a.pos.X)*(b.pos.Y-a.pos.Y))) / 2
	}
	return area
}

func fillArea(p *Path, rule FillRule) float64 {
	var f filler
	f.rule = rule
	f.color = [4]float32{1, 1, 1, 1}
	f.run(p.flatten(0.05))
	return triangleArea(f.verts, true)
}

func TestFillRectangleArea(t *testing.T) {
	var p Path
	p.Rect(10, 10, 30, 20)
	if a := fillArea(&p, FillNonZero); math.Abs(a-600) > 1e-3 {
		t.Errorf("rectangle area %.3f, want 600", a)
	}
}

func TestFillRulesWithHole(t *testing.T) {
	// A square with a smaller square inside drawn the same way round:
	// non-zero fills both, even-odd leaves a hole.
	var p Path
	p.Rect(0, 0, 10, 10).Rect(2, 2, 4, 4)
	if a := fillArea(&p, FillNonZero); math.Abs(a-100) > 1e-3 {
		t.Errorf("non-zero area %.3f, want 100", a)
	}
	if a := fillArea(&p, FillEvenOdd); math.Abs(a-84) > 1e-3 {
		t.Errorf("even-odd area %.3f, want 84", a)
	}
	// The inner square drawn the other way round is a hole under both rules.
	var q Path
	q.Rect(0, 0, 10, 10).MoveTo(2, 2).LineTo(2, 6).LineTo(6, 6).LineTo(6, 2).Close()
	if a := fillArea(&q, FillNonZero); math.Abs(a-84) > 1e-3 {
		t.Errorf("reversed hole non-zero area %.3f, want 84", a)
	}
}

func TestFillSelfIntersecting(t *testing.T) {
	// A bow tie: two triangles meeting at a point, each of area 25.
	var p Path
	p.MoveTo(0, 0).LineTo(10, 10).LineTo(10, 0).LineTo(0, 10).Close()
	for _, rule := range []FillRule{FillNonZero, FillEvenOdd} {
		if a := fillArea(&p, rule); math.Abs(a-50) > 1e-3 {
			t.Errorf("bow tie area under rule %d is %.3f, want 50", rule, a)
		}
	}
	// A pentagram: the inner pentagon is filled by non-zero, a hole by even-odd.
	var star Path
	for i := range 5 {
		a := float64(i) * 4 * math.Pi / 5
		x, y := float32(50+40*math.Sin(a)), float32(50-40*math.Cos(a))
		if i == 0 {
			star.MoveTo(x, y)
		} else {
			star.LineTo(x, y)
		}
	}
	star.Close()
	nz, eo := fillArea(&star, FillNonZero), fillArea(&star, FillEvenOdd)
	if nz <= eo || nz < 1000 || eo < 900 {
		t.Errorf("pentagram areas non-zero %.1f even-odd %.1f; non-zero should include the centre", nz, eo)
	}
}

func TestFillCircleArea(t *testing.T) {
	var p Path
	p.Circle(0, 0, 10)
	want := math.Pi * 100
	if a := fillArea(&p, FillNonZero); math.Abs(a-want) > want*0.005 {
		t.Errorf("circle area %.2f, want %.2f", a, want)
	}
}

func TestFringeIsOutside(t *testing.T) {
	// Fringe triangles of a filled rectangle must lie outside it.
	var p Path
	p.Rect(10, 10, 20, 20)
	var f filler
	f.rule, f.color, f.fringe = FillNonZero, [4]float32{1, 1, 1, 1}, 1
	f.run(p.flatten(0.05))
	for i := 0; i+2 < len(f.verts); i += 3 {
		for _, v := range f.verts[i : i+3] {
			if v.color[3] == 0 && v.pos.X > 10.001 && v.pos.X < 29.999 && v.pos.Y > 10.001 && v.pos.Y < 29.999 {
				t.Fatalf("fringe vertex %v is inside the rectangle", v.pos)
			}
		}
	}
	if a := triangleArea(f.verts, true); math.Abs(a-400) > 1e-3 {
		t.Errorf("opaque area with fringes %.3f, want 400", a)
	}
}

func TestStrokeArea(t *testing.T) {
	var p Path
	p.MoveTo(0, 0).LineTo(100, 0)
	s := stroker{color: [4]float32{1, 1, 1, 1}, opts: StrokeOptions{Width: 4}}
	s.run(p.flatten(0.05)[0])
	if a := triangleArea(s.verts, true); math.Abs(a-400) > 1e-3 {
		t.Errorf("butt stroke area %.3f, want 400", a)
	}
	s = stroker{color: [4]float32{1, 1, 1, 1}, opts: StrokeOptions{Width: 4, Cap: CapSquare}}
	s.run(p.flatten(0.05)[0])
	if a := triangleArea(s.verts, true); math.Abs(a-416) > 1e-3 {
		t.Errorf("square-cap stroke area %.3f, want 416", a)
	}
	s = stroker{color: [4]float32{1, 1, 1, 1}, opts: StrokeOptions{Width: 4, Cap: CapRound}}
	s.run(p.flatten(0.05)[0])
	if a := triangleArea(s.verts, true); math.Abs(a-(400+math.Pi*4)) > 0.5 {
		t.Errorf("round-cap stroke area %.3f, want %.3f", a, 400+math.Pi*4)
	}
}

func TestStrokeJoins(t *testing.T) {
	// A right-angle corner: miter adds a square corner (hw²), bevel a
	// triangle (hw²/2), round a quarter disc.
	var p Path
	p.MoveTo(0, 0).LineTo(10, 0).LineTo(10, 10)
	base := 2.0 * 10 * 2 // two segments of width 2
	for _, tc := range []struct {
		join LineJoin
		want float64
	}{{JoinMiter, base + 1}, {JoinBevel, base + 0.5}, {JoinRound, base + math.Pi/4}} {
		s := stroker{color: [4]float32{1, 1, 1, 1}, opts: StrokeOptions{Width: 2, Join: tc.join, MiterLimit: 4}}
		s.run(p.flatten(0.05)[0])
		if a := triangleArea(s.verts, true); math.Abs(a-tc.want) > 0.05 {
			t.Errorf("join %d area %.3f, want %.3f", tc.join, a, tc.want)
		}
	}
}

func TestPathChainingAndArc(t *testing.T) {
	var p Path
	p.MoveTo(0, 0).LineTo(10, 0).ArcTo(20, 0, 20, 10, 5).LineTo(20, 20).Close()
	subs := p.flatten(0.05)
	if len(subs) != 1 || !subs[0].closed || len(subs[0].pts) < 8 {
		t.Fatalf("flatten: %d sub-paths, closed %v, %d points", len(subs), subs[0].closed, len(subs[0].pts))
	}
	// Every arc point is 5 from the corner's rounding centre (15, 5).
	for _, q := range subs[0].pts {
		if q.X > 15 && q.Y < 5 {
			if d := q.Sub(lin.V2(15, 5)).Len(); math.Abs(float64(d-5)) > 0.05 {
				t.Errorf("arc point %v is %.3f from the centre, want 5", q, d)
			}
		}
	}
}

func TestAffine(t *testing.T) {
	m := lin.Translate2(10, 20).Mul(lin.Rotate2(math.Pi / 2)).Mul(lin.Scale2(2, 2))
	p := m.Apply(lin.V2(1, 0)) // scale to (2,0), rotate to (0,2), translate to (10,22)
	if p.Sub(lin.V2(10, 22)).Len() > 1e-5 {
		t.Errorf("composed transform gave %v, want (10, 22)", p)
	}
	back := m.Inverse().Apply(p)
	if back.Sub(lin.V2(1, 0)).Len() > 1e-5 {
		t.Errorf("inverse gave %v, want (1, 0)", back)
	}
	if s := m.Scale(); math.Abs(float64(s-2)) > 1e-5 {
		t.Errorf("scale %.4f, want 2", s)
	}
	sh := lin.Shear2(1, 0).Apply(lin.V2(0, 1))
	if sh != lin.V2(1, 1) {
		t.Errorf("shear gave %v, want (1, 1)", sh)
	}
}
