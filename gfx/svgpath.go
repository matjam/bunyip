package gfx

import (
	"math"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// SVG shapes are flattened into moves, lines, cubic curves and closes;
// quadratic curves and elliptical arcs become cubics, so the rasteriser
// sees one kind of curve.

type svgSegOp uint8

const (
	svgMoveTo svgSegOp = iota
	svgLineTo
	svgCubeTo
	svgClose
)

// svgSeg is one piece of a path in the document's user space.
type svgSeg struct {
	op  svgSegOp
	n   int // points the segment uses
	pts [3]lin.Vec2
}

// svgShapePath returns an element's outline, or nothing for an element
// that is not a filled shape.
func svgShapePath(el *svgElem) []svgSeg {
	num := func(name string) float32 { return svgNumber(el.attr[name], 0) }
	switch el.tag {
	case "path":
		return parseSVGPath(el.attr["d"])
	case "rect":
		w, h := num("width"), num("height")
		if w <= 0 || h <= 0 {
			return nil
		}
		rx, ry := num("rx"), num("ry")
		if el.attr["rx"] == "" {
			rx = ry
		}
		if el.attr["ry"] == "" {
			ry = rx
		}
		return svgRectPath(num("x"), num("y"), w, h, min(rx, w/2), min(ry, h/2))
	case "circle":
		if r := num("r"); r > 0 {
			return svgEllipsePath(num("cx"), num("cy"), r, r)
		}
	case "ellipse":
		if rx, ry := num("rx"), num("ry"); rx > 0 && ry > 0 {
			return svgEllipsePath(num("cx"), num("cy"), rx, ry)
		}
	case "polygon", "polyline":
		nums := svgNumbers(el.attr["points"])
		if len(nums) < 4 {
			return nil
		}
		var out []svgSeg
		for i := 0; i+1 < len(nums); i += 2 {
			op := svgLineTo
			if i == 0 {
				op = svgMoveTo
			}
			out = append(out, svgSeg{op: op, n: 1, pts: [3]lin.Vec2{lin.V2(nums[i], nums[i+1])}})
		}
		return append(out, svgSeg{op: svgClose})
	}
	return nil
}

// kappa is the cubic control offset that draws a quarter circle.
const kappa = 0.5522847498

// svgEllipsePath draws an ellipse as four cubic quarters.
func svgEllipsePath(cx, cy, rx, ry float32) []svgSeg {
	ox, oy := rx*kappa, ry*kappa
	move := svgSeg{op: svgMoveTo, n: 1, pts: [3]lin.Vec2{lin.V2(cx+rx, cy)}}
	cube := func(c1x, c1y, c2x, c2y, x, y float32) svgSeg {
		return svgSeg{op: svgCubeTo, n: 3, pts: [3]lin.Vec2{lin.V2(c1x, c1y), lin.V2(c2x, c2y), lin.V2(x, y)}}
	}
	return []svgSeg{
		move,
		cube(cx+rx, cy+oy, cx+ox, cy+ry, cx, cy+ry),
		cube(cx-ox, cy+ry, cx-rx, cy+oy, cx-rx, cy),
		cube(cx-rx, cy-oy, cx-ox, cy-ry, cx, cy-ry),
		cube(cx+ox, cy-ry, cx+rx, cy-oy, cx+rx, cy),
		{op: svgClose},
	}
}

// svgRectPath draws a rectangle, with rounded corners when it has them.
func svgRectPath(x, y, w, h, rx, ry float32) []svgSeg {
	line := func(px, py float32) svgSeg {
		return svgSeg{op: svgLineTo, n: 1, pts: [3]lin.Vec2{lin.V2(px, py)}}
	}
	if rx <= 0 || ry <= 0 {
		return []svgSeg{
			{op: svgMoveTo, n: 1, pts: [3]lin.Vec2{lin.V2(x, y)}},
			line(x+w, y), line(x+w, y+h), line(x, y+h), {op: svgClose},
		}
	}
	ox, oy := rx*kappa, ry*kappa
	cube := func(c1x, c1y, c2x, c2y, px, py float32) svgSeg {
		return svgSeg{op: svgCubeTo, n: 3, pts: [3]lin.Vec2{lin.V2(c1x, c1y), lin.V2(c2x, c2y), lin.V2(px, py)}}
	}
	return []svgSeg{
		{op: svgMoveTo, n: 1, pts: [3]lin.Vec2{lin.V2(x+rx, y)}},
		line(x+w-rx, y),
		cube(x+w-rx+ox, y, x+w, y+ry-oy, x+w, y+ry),
		line(x+w, y+h-ry),
		cube(x+w, y+h-ry+oy, x+w-rx+ox, y+h, x+w-rx, y+h),
		line(x+rx, y+h),
		cube(x+rx-ox, y+h, x, y+h-ry+oy, x, y+h-ry),
		line(x, y+ry),
		cube(x, y+ry-oy, x+rx-ox, y, x+rx, y),
		{op: svgClose},
	}
}

// parseSVGPath reads path data into segments.
func parseSVGPath(d string) []svgSeg {
	var out []svgSeg
	var cur, start, lastCube, lastQuad lin.Vec2
	var prev byte
	i := 0
	skip := func() {
		for i < len(d) && (d[i] == ' ' || d[i] == ',' || d[i] == '\t' || d[i] == '\n' || d[i] == '\r') {
			i++
		}
	}
	// number reads the next number of the data, reporting false at its end.
	number := func() (float32, bool) {
		skip()
		j := i
		if j < len(d) && (d[j] == '+' || d[j] == '-') {
			j++
		}
		seenDot, seenExp := false, false
		for j < len(d) {
			c := d[j]
			switch {
			case c >= '0' && c <= '9':
			case c == '.' && !seenDot && !seenExp:
				seenDot = true
			case (c == 'e' || c == 'E') && !seenExp && j > i:
				seenExp = true
				if j+1 < len(d) && (d[j+1] == '+' || d[j+1] == '-') {
					j++
				}
			default:
				goto done
			}
			j++
		}
	done:
		if j == i {
			return 0, false
		}
		v := svgNumber(d[i:j], 0)
		i = j
		return v, true
	}
	pair := func() (lin.Vec2, bool) {
		x, ok := number()
		if !ok {
			return lin.Vec2{}, false
		}
		y, ok := number()
		if !ok {
			return lin.Vec2{}, false
		}
		return lin.V2(x, y), true
	}
	move := func(p lin.Vec2) {
		out = append(out, svgSeg{op: svgMoveTo, n: 1, pts: [3]lin.Vec2{p}})
		cur, start = p, p
	}
	line := func(p lin.Vec2) {
		out = append(out, svgSeg{op: svgLineTo, n: 1, pts: [3]lin.Vec2{p}})
		cur = p
	}
	cube := func(c1, c2, p lin.Vec2) {
		out = append(out, svgSeg{op: svgCubeTo, n: 3, pts: [3]lin.Vec2{c1, c2, p}})
		cur, lastCube = p, c2
	}
	quad := func(c, p lin.Vec2) {
		// A quadratic curve is the cubic with its controls two thirds of
		// the way to the quadratic's own.
		c1 := cur.Add(c.Sub(cur).Mul(2.0 / 3))
		c2 := p.Add(c.Sub(p).Mul(2.0 / 3))
		out = append(out, svgSeg{op: svgCubeTo, n: 3, pts: [3]lin.Vec2{c1, c2, p}})
		cur, lastQuad = p, c
	}
	for {
		skip()
		if i >= len(d) {
			break
		}
		cmd := d[i]
		if strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", cmd) >= 0 {
			i++
		} else if prev != 0 {
			// A repeated command list: M becomes L, m becomes l.
			switch prev {
			case 'M':
				cmd = 'L'
			case 'm':
				cmd = 'l'
			default:
				cmd = prev
			}
		} else {
			i++
			continue
		}
		rel := cmd >= 'a' && cmd <= 'z'
		abs := func(p lin.Vec2) lin.Vec2 {
			if rel {
				return cur.Add(p)
			}
			return p
		}
		switch cmd | 0x20 { // the lower-case form of the command
		case 'm':
			p, ok := pair()
			if !ok {
				return out
			}
			move(abs(p))
		case 'l':
			p, ok := pair()
			if !ok {
				return out
			}
			line(abs(p))
		case 'h':
			x, ok := number()
			if !ok {
				return out
			}
			if rel {
				x += cur.X
			}
			line(lin.V2(x, cur.Y))
		case 'v':
			y, ok := number()
			if !ok {
				return out
			}
			if rel {
				y += cur.Y
			}
			line(lin.V2(cur.X, y))
		case 'c':
			c1, ok := pair()
			if !ok {
				return out
			}
			c2, ok := pair()
			if !ok {
				return out
			}
			p, ok := pair()
			if !ok {
				return out
			}
			cube(abs(c1), abs(c2), abs(p))
		case 's':
			c2, ok := pair()
			if !ok {
				return out
			}
			p, ok := pair()
			if !ok {
				return out
			}
			c1 := cur
			if prev == 'C' || prev == 'c' || prev == 'S' || prev == 's' {
				c1 = cur.Mul(2).Sub(lastCube)
			}
			cube(c1, abs(c2), abs(p))
		case 'q':
			c, ok := pair()
			if !ok {
				return out
			}
			p, ok := pair()
			if !ok {
				return out
			}
			quad(abs(c), abs(p))
		case 't':
			p, ok := pair()
			if !ok {
				return out
			}
			c := cur
			if prev == 'Q' || prev == 'q' || prev == 'T' || prev == 't' {
				c = cur.Mul(2).Sub(lastQuad)
			}
			quad(c, abs(p))
		case 'a':
			rx, ok := number()
			if !ok {
				return out
			}
			ry, ok := number()
			if !ok {
				return out
			}
			rot, ok := number()
			if !ok {
				return out
			}
			large, ok := number()
			if !ok {
				return out
			}
			sweep, ok := number()
			if !ok {
				return out
			}
			p, ok := pair()
			if !ok {
				return out
			}
			end := abs(p)
			for _, s := range svgArc(cur, end, rx, ry, rot*math.Pi/180, large != 0, sweep != 0) {
				out = append(out, s)
			}
			cur = end
		case 'z':
			out = append(out, svgSeg{op: svgClose})
			cur = start
			// A close takes no numbers, so it cannot be the command a bare
			// number repeats; forgetting it keeps such data from looping.
			cmd = 0
		}
		prev = cmd
	}
	return out
}

// svgArc turns an elliptical arc into cubic curves, by the endpoint to
// centre conversion the SVG specification gives.
func svgArc(from, to lin.Vec2, rx, ry, rot float32, large, sweep bool) []svgSeg {
	if rx == 0 || ry == 0 || from == to {
		return []svgSeg{{op: svgLineTo, n: 1, pts: [3]lin.Vec2{to}}}
	}
	rx, ry = abs32(rx), abs32(ry)
	cosR, sinR := float32(math.Cos(float64(rot))), float32(math.Sin(float64(rot)))
	// The arc in the space where the ellipse is a unit circle.
	dx, dy := (from.X-to.X)/2, (from.Y-to.Y)/2
	x1 := cosR*dx + sinR*dy
	y1 := -sinR*dx + cosR*dy
	// Radii too small for the two ends are scaled up until they fit.
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := float32(math.Sqrt(float64(l)))
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	if den == 0 {
		return []svgSeg{{op: svgLineTo, n: 1, pts: [3]lin.Vec2{to}}}
	}
	co := float32(math.Sqrt(math.Max(0, float64(num/den))))
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1/ry, -co*ry*x1/rx
	cx := cosR*cxp - sinR*cyp + (from.X+to.X)/2
	cy := sinR*cxp + cosR*cyp + (from.Y+to.Y)/2
	angle := func(ux, uy float32) float32 {
		return float32(math.Atan2(float64(uy), float64(ux)))
	}
	start := angle((x1-cxp)/rx, (y1-cyp)/ry)
	end := angle((-x1-cxp)/rx, (-y1-cyp)/ry)
	sweepAngle := end - start
	if !sweep && sweepAngle > 0 {
		sweepAngle -= 2 * math.Pi
	} else if sweep && sweepAngle < 0 {
		sweepAngle += 2 * math.Pi
	}
	// A cubic holds a quarter turn well, so the arc is cut into quarters.
	steps := max(1, int(math.Ceil(math.Abs(float64(sweepAngle))/(math.Pi/2))))
	step := sweepAngle / float32(steps)
	k := 4.0 / 3 * float32(math.Tan(float64(step)/4))
	point := func(a float32) (lin.Vec2, lin.Vec2) {
		cosA, sinA := float32(math.Cos(float64(a))), float32(math.Sin(float64(a)))
		x, y := rx*cosA, ry*sinA
		dxa, dya := -rx*sinA, ry*cosA
		return lin.V2(cosR*x-sinR*y+cx, sinR*x+cosR*y+cy), lin.V2(cosR*dxa-sinR*dya, sinR*dxa+cosR*dya)
	}
	out := make([]svgSeg, 0, steps)
	a := start
	p0, d0 := point(a)
	for range steps {
		a += step
		p1, d1 := point(a)
		out = append(out, svgSeg{op: svgCubeTo, n: 3, pts: [3]lin.Vec2{
			p0.Add(d0.Mul(k)), p1.Sub(d1.Mul(k)), p1,
		}})
		p0, d0 = p1, d1
	}
	return out
}
