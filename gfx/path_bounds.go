package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Bounds returns the tight axis-aligned bounds of the path's lines and
// Bézier curves, including isolated MoveTo points. It excludes stroke width,
// antialiasing and graphics transforms. An empty path returns a zero Rect.
// Arcs and ellipses are bounded as the cubic curves stored by Path.
func (p *Path) Bounds() lin.Rect {
	if len(p.cmds) == 0 {
		return lin.Rect{}
	}
	lo, hi := lin.V2(float32(math.Inf(1)), float32(math.Inf(1))), lin.V2(float32(math.Inf(-1)), float32(math.Inf(-1)))
	add := func(v lin.Vec2) {
		lo = lin.V2(min(lo.X, v.X), min(lo.Y, v.Y))
		hi = lin.V2(max(hi.X, v.X), max(hi.Y, v.Y))
	}
	var cur, start lin.Vec2
	for _, cmd := range p.cmds {
		switch cmd.op {
		case opMove:
			cur, start = cmd.p[0], cmd.p[0]
			add(cur)
		case opLine:
			cur = cmd.p[0]
			add(cur)
		case opQuad:
			control, end := cmd.p[0], cmd.p[1]
			for _, axis := range [][3]float32{{cur.X, control.X, end.X}, {cur.Y, control.Y, end.Y}} {
				a, b, c := float64(axis[0]), float64(axis[1]), float64(axis[2])
				if den := a - 2*b + c; den != 0 {
					t := (a - b) / den
					if t > 0 && t < 1 {
						u := 1 - t
						add(lin.V2(float32(u*u*float64(cur.X)+2*u*t*float64(control.X)+t*t*float64(end.X)), float32(u*u*float64(cur.Y)+2*u*t*float64(control.Y)+t*t*float64(end.Y))))
					}
				}
			}
			cur = end
			add(cur)
		case opCubic:
			c1, c2, end := cmd.p[0], cmd.p[1], cmd.p[2]
			for _, axis := range [][4]float32{{cur.X, c1.X, c2.X, end.X}, {cur.Y, c1.Y, c2.Y, end.Y}} {
				a, b, c, d := float64(axis[0]), float64(axis[1]), float64(axis[2]), float64(axis[3])
				for _, t := range curveRoots(-a+3*b-3*c+d, 2*(a-2*b+c), b-a) {
					if t > 0 && t < 1 {
						u := 1 - t
						point := func(a, b, c, d float32) float32 {
							return float32(u*u*u*float64(a) + 3*u*u*t*float64(b) + 3*u*t*t*float64(c) + t*t*t*float64(d))
						}
						add(lin.V2(point(cur.X, c1.X, c2.X, end.X), point(cur.Y, c1.Y, c2.Y, end.Y)))
					}
				}
			}
			cur = end
			add(cur)
		case opClose:
			cur = start
		}
	}
	return lin.R(lo.X, lo.Y, hi.X-lo.X, hi.Y-lo.Y)
}

func curveRoots(a, b, c float64) []float64 {
	if a == 0 {
		if b == 0 {
			return nil
		}
		return []float64{-c / b}
	}
	d := b*b - 4*a*c
	if d < 0 {
		return nil
	}
	q := -0.5 * (b + math.Copysign(math.Sqrt(d), b))
	if q == 0 {
		return []float64{-b / (2 * a)}
	}
	return []float64{q / a, c / q}
}
