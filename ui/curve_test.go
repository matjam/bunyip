package ui

import (
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// curveHarness lays a curve editor out on its own and returns the body
// to run, the points it edits and the graph's rectangle.
func curveHarness(c *Context, pts *[]lin.Vec2, changed *bool) (func(), Rect) {
	// The panel's padding and the caption above the graph decide where it
	// lands; the accessibility node reports the widget rectangle, but the
	// numbers here are fixed by the layout so the test can aim at them.
	const panelX, panelY = 10, 10
	body := func() {
		c.Panel("", Rect{X: panelX, Y: panelY, W: 220, H: 160}, func() {
			if c.CurveEditor("size", pts, 0, 1, 80) {
				*changed = true
			}
		})
	}
	_, captionH := c.Theme.Font.Measure("size", gfx.TextOptions{})
	graph := Rect{
		X: panelX + c.Theme.Padding,
		Y: panelY + c.Theme.Padding + captionH,
		W: 220 - 2*c.Theme.Padding,
		H: 80,
	}
	return body, graph
}

// at is the view position of a curve point on a graph.
func at(graph Rect, p lin.Vec2, lo, hi float32) (float32, float32) {
	return graph.X + p.X*graph.W, graph.Y + graph.H - (p.Y-lo)/(hi-lo)*graph.H
}

// TestCurveEditorDrag checks that dragging a point moves it and reports
// the change, and that the first point keeps its x so the curve still
// spans the whole lifetime.
func TestCurveEditorDrag(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	pts := []lin.Vec2{{X: 0, Y: 1}, {X: 1, Y: 0}}
	changed := false
	body, graph := curveHarness(c, &pts, &changed)
	run(t, c, in, body)

	// Take hold of the first point, at the graph's top-left.
	px, py := at(graph, pts[0], 0, 1)
	in.FeedMouseMove(px, py)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, px, py)
	run(t, c, in, body)
	// Drag it to the middle of the graph's height.
	my := graph.Y + graph.H/2
	in.FeedMouseMove(px, my)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, false, px, my)
	run(t, c, in, body)

	if !changed {
		t.Error("dragging a point reported no change")
	}
	if len(pts) != 2 {
		t.Fatalf("the drag changed the point count to %d", len(pts))
	}
	if pts[0].X != 0 {
		t.Errorf("the first point moved to x = %v; it anchors the curve at 0", pts[0].X)
	}
	if pts[0].Y > 0.75 || pts[0].Y < 0.25 {
		t.Errorf("the first point is at y = %v, want about 0.5", pts[0].Y)
	}
}

// TestCurveEditorAddAndRemove checks that a click clear of every point
// adds one and a right-click on a point takes it away, down to the two
// the curve keeps.
func TestCurveEditorAddAndRemove(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	pts := []lin.Vec2{{X: 0, Y: 0}, {X: 1, Y: 1}}
	changed := false
	body, graph := curveHarness(c, &pts, &changed)
	run(t, c, in, body)

	// The middle of the graph is far from both ends of this curve.
	mx, my := graph.X+graph.W/2, graph.Y+graph.H/2
	in.FeedMouseMove(mx, my)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, mx, my)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, false, mx, my)
	run(t, c, in, body)
	if len(pts) != 3 {
		t.Fatalf("clicking an empty part of the graph gave %d points, want 3", len(pts))
	}
	if pts[1].X <= pts[0].X || pts[1].X >= pts[2].X {
		t.Errorf("the new point at %v is out of order in %v", pts[1], pts)
	}

	// Right-click the point just added.
	nx, ny := at(graph, pts[1], 0, 1)
	in.FeedMouseMove(nx, ny)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseRight, true, nx, ny)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseRight, false, nx, ny)
	run(t, c, in, body)
	if len(pts) != 2 {
		t.Fatalf("right-clicking a point gave %d points, want 2", len(pts))
	}

	// The last two cannot be removed: a curve keeps its ends.
	ex, ey := at(graph, pts[0], 0, 1)
	in.FeedMouseMove(ex, ey)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseRight, true, ex, ey)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseRight, false, ex, ey)
	run(t, c, in, body)
	if len(pts) != 2 {
		t.Errorf("a right-click took the curve down to %d points", len(pts))
	}
}

// TestCurveEditorQuiet checks that a frame with no input reports no
// change, so a game can use the return value to rebuild an emitter.
func TestCurveEditorQuiet(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	pts := []lin.Vec2{{X: 0, Y: 1}, {X: 0.5, Y: 0.2}, {X: 1, Y: 0}}
	changed := false
	body, _ := curveHarness(c, &pts, &changed)
	for range 3 {
		run(t, c, in, body)
	}
	if changed {
		t.Error("an untouched curve editor reported a change")
	}
	if len(pts) != 3 {
		t.Errorf("an untouched curve editor changed the points to %v", pts)
	}
}
