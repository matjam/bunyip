package ui

import (
	"fmt"
	"slices"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// CurveEditor edits a curve of points drawn as a graph: drag a point to
// move it, click an empty part of the graph to add one, right-click a
// point to remove it. It reports whether the curve changed this frame.
//
// The points are (x, y) pairs kept in increasing x. X runs from 0 to 1
// across the graph and y from lo to hi up it, so a particle curve over a
// lifetime passes lo and hi as the range its values may take. The first
// and last points keep their x, so a curve always spans the whole range;
// the rest move freely between their neighbours. A curve of fewer than
// two points is drawn but only its points move.
//
// height is the graph's height in view units; zero is 80.
func (c *Context) CurveEditor(label string, points *[]lin.Vec2, lo, hi, height float32) bool {
	if height <= 0 {
		height = 80
	}
	if hi <= lo {
		hi = lo + 1
	}
	id := c.id(label)
	_, captionH := c.Theme.Font.Measure(label, gfx.TextOptions{})
	full := c.next(height + captionH)
	graph := Rect{X: full.X, Y: full.Y + captionH, W: full.W, H: full.H - captionH}
	c.text(label, full.X, full.Y, c.Theme.TextDim)

	hover, held, clicked := c.interact(id, graph)
	pts := *points
	// Where a point sits on the graph, and back again.
	toView := func(p lin.Vec2) lin.Vec2 {
		return lin.V2(graph.X+p.X*graph.W, graph.Y+graph.H-(p.Y-lo)/(hi-lo)*graph.H)
	}
	toCurve := func(x, y float32) lin.Vec2 {
		return lin.V2(
			min(max((x-graph.X)/graph.W, 0), 1),
			min(max(lo+(graph.Y+graph.H-y)/graph.H*(hi-lo), min(lo, hi)), max(lo, hi)))
	}

	const grab = 9 // view units within which a click takes a point
	nearest := -1
	if hover || held {
		best := float32(grab * grab)
		for i, p := range pts {
			v := toView(p)
			dx, dy := v.X-c.mouseX, v.Y-c.mouseY
			if d := dx*dx + dy*dy; d < best {
				best, nearest = d, i
			}
		}
	}

	changed := false
	state := c.curves[id]
	if c.pressed && hover {
		if nearest >= 0 {
			state.dragging, state.point = true, nearest
		} else if len(pts) >= 2 {
			// A click clear of every point adds one where it landed.
			p := toCurve(c.mouseX, c.mouseY)
			at := 0
			for at < len(pts) && pts[at].X < p.X {
				at++
			}
			pts = slices.Insert(pts, at, p)
			state.dragging, state.point = true, at
			changed = true
		}
	}
	if !c.down {
		state.dragging = false
	}
	if state.dragging && state.point < len(pts) {
		i := state.point
		p := toCurve(c.mouseX, c.mouseY)
		// The ends anchor the curve to the whole range; the rest stay
		// between their neighbours so the points keep their order.
		switch {
		case i == 0:
			p.X = pts[0].X
		case i == len(pts)-1:
			p.X = pts[i].X
		default:
			p.X = min(max(p.X, pts[i-1].X), pts[i+1].X)
		}
		if p != pts[i] {
			pts[i] = p
			changed = true
		}
	}
	// A right-click takes a point out, as long as two are left.
	if c.in != nil && c.in.MousePressed(input.MouseRight) && hover && nearest >= 0 && len(pts) > 2 {
		pts = slices.Delete(pts, nearest, nearest+1)
		state.dragging = false
		changed = true
	}
	if c.curves == nil {
		c.curves = map[widgetID]curveState{}
	}
	c.curves[id] = state
	*points = pts

	// The graph: a box, a line through zero when the range crosses it,
	// then the curve and its points.
	c.box(c.skin().Field, graph, c.Theme.Field, c.Theme.PanelBorder)
	if lo < 0 && hi > 0 {
		y := toView(lin.V2(0, 0)).Y
		c.g.FillRect(graph.X, y, graph.W, 1, c.Theme.PanelBorder)
	}
	if len(pts) > 0 {
		// Held flat outside the first and last points, as Curve.At reads it.
		first, last := toView(pts[0]), toView(pts[len(pts)-1])
		c.g.StrokeLine(graph.X, first.Y, first.X, first.Y, 1, c.Theme.TextDim)
		c.g.StrokeLine(last.X, last.Y, graph.X+graph.W, last.Y, 1, c.Theme.TextDim)
		for i := 1; i < len(pts); i++ {
			a, b := toView(pts[i-1]), toView(pts[i])
			c.g.StrokeLine(a.X, a.Y, b.X, b.Y, 2, c.Theme.Accent)
		}
		for i, p := range pts {
			v := toView(p)
			col := c.Theme.Text
			if i == nearest {
				col = c.Theme.Accent
			}
			c.g.FillRect(v.X-3, v.Y-3, 6, 6, col)
		}
	}
	if clicked {
		c.setFocus(id)
	}
	c.note("curve", label, fmt.Sprintf("%d points", len(pts)), false)
	return changed
}

// curveState remembers which point a drag took hold of, because a drag
// carries on while the pointer is outside the graph.
type curveState struct {
	dragging bool
	point    int
}
