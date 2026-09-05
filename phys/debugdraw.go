package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// DebugColors are the colours the debug drawing uses. A zero colour
// where one is expected is left at the default noted, so DebugColors{}
// draws awake bodies orange, sleeping bodies grey, static colliders blue
// and contact normals red.
type DebugColors struct {
	Awake    gfx.Color // bodies that are simulating
	Asleep   gfx.Color // bodies that have gone to sleep
	Static   gfx.Color // colliders with no body
	Contacts gfx.Color // contact points and their normals
}

// fill returns the colours with every zero one replaced by its default.
func (c DebugColors) fill() DebugColors {
	zero := gfx.Color{}
	if c.Awake == zero {
		c.Awake = gfx.RGB(255, 170, 40)
	}
	if c.Asleep == zero {
		c.Asleep = gfx.RGB(150, 150, 160)
	}
	if c.Static == zero {
		c.Static = gfx.RGB(90, 200, 255)
	}
	if c.Contacts == zero {
		c.Contacts = gfx.RGB(255, 60, 60)
	}
	return c
}

// DrawColliders3 outlines every 3D collider in the world as debug lines
// over the scene, and draws the normal of each contact the last update
// reported. Call it from Draw, with the same camera the scene is drawn
// with. Awake bodies, sleeping bodies and static colliders are told
// apart by colour; DrawCollidersColors3 chooses the colours.
func DrawColliders3(g *gfx.Graphics, w *ecs.World) {
	DrawCollidersColors3(g, w, DebugColors{})
}

// DrawCollidersColors3 is DrawColliders3 with the colours chosen.
func DrawCollidersColors3(g *gfx.Graphics, w *ecs.World, colors DebugColors) {
	if g == nil || w == nil {
		return
	}
	col := colors.fill()
	w.Each2(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		shade := col.Static
		if b, ok := w.Get[Body3](e); ok {
			shade = col.Awake
			if b.Asleep() {
				shade = col.Asleep
			}
		}
		DrawShape3(g, c.Shape, *t, shade)
	})
	for _, hit := range w.Events[Collision3]() {
		g.DrawLine3D(hit.Point, hit.Point.Add(hit.Normal.Mul(0.5)), col.Contacts)
	}
}

// DrawShape3 outlines one 3D shape placed by a transform, as debug
// lines: a sphere as three rings, a box as its edges, a capsule as two
// spheres and the lines between them, a hull as every edge between its
// points, a compound as each of its parts. A mesh shape is left out,
// since a terrain mesh is already drawn.
func DrawShape3(g *gfx.Graphics, s Shape3, t gfx.Transform, c gfx.Color) {
	rot := t.Rotation
	if rot == (lin.Quat{}) {
		rot = lin.QuatIdentity()
	}
	switch sh := s.(type) {
	case Sphere:
		g.DrawWireSphere(t.Position, sh.Radius, c)
	case Box3:
		g.DrawWireCube(lin.TRS(t.Position, rot, sh.Half.Mul(2)), c)
	case Capsule:
		up := rot.Rotate(lin.V3(0, sh.HalfHeight, 0))
		a, b := t.Position.Sub(up), t.Position.Add(up)
		g.DrawWireSphere(a, sh.Radius, c)
		g.DrawWireSphere(b, sh.Radius, c)
		for _, d := range []lin.Vec3{rot.Rotate(lin.V3(sh.Radius, 0, 0)), rot.Rotate(lin.V3(0, 0, sh.Radius))} {
			g.DrawLine3D(a.Add(d), b.Add(d), c)
			g.DrawLine3D(a.Sub(d), b.Sub(d), c)
		}
	case ConvexHull:
		// Every pair of points: a dense outline that shows the volume.
		pts := make([]lin.Vec3, len(sh.Points))
		for i, p := range sh.Points {
			pts[i] = t.Position.Add(rot.Rotate(p))
		}
		for i := range pts {
			for j := i + 1; j < len(pts); j++ {
				g.DrawLine3D(pts[i], pts[j], c)
			}
		}
	case Compound3:
		for _, p := range sh.Parts {
			pt := gfx.Transform{Position: t.Position.Add(rot.Rotate(p.Offset)), Rotation: rot.Mul(p.Rotation)}
			if p.Rotation == (lin.Quat{}) {
				pt.Rotation = rot
			}
			DrawShape3(g, p.Shape, pt, c)
		}
	}
}

// DrawColliders2 outlines every 2D collider in the world as stroked
// paths in world units, and draws the normal of each contact the last
// update reported. Call it from Draw, under the same 2D camera the
// world is drawn with.
func DrawColliders2(g *gfx.Graphics, w *ecs.World) {
	DrawCollidersColors2(g, w, DebugColors{})
}

// DrawCollidersColors2 is DrawColliders2 with the colours chosen.
func DrawCollidersColors2(g *gfx.Graphics, w *ecs.World, colors DebugColors) {
	if g == nil || w == nil {
		return
	}
	col := colors.fill()
	w.Each2(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		shade := col.Static
		if b, ok := w.Get[Body2](e); ok {
			shade = col.Awake
			if b.Asleep() {
				shade = col.Asleep
			}
		}
		DrawShape2(g, c.Shape, *t, shade)
	})
	for _, hit := range w.Events[Collision2]() {
		p := hit.Point
		n := p.Add(hit.Normal.Mul(0.5))
		g.StrokeLine(p.X, p.Y, n.X, n.Y, debugLineWidth, col.Contacts)
	}
}

// debugLineWidth is how wide the 2D outlines are drawn, in world units.
const debugLineWidth = 0.05

// DrawShape2 outlines one 2D shape placed by a transform, as a stroked
// path in world units: a circle, a box, a polygon, a capsule as two
// circles and their sides, an edge or chain as its segments.
func DrawShape2(g *gfx.Graphics, s Shape2, t gfx.Transform2, c gfx.Color) {
	pos, rot := t.Position, t.Rotation
	at := func(p lin.Vec2) lin.Vec2 {
		sn, cs := float32(math.Sin(float64(rot))), float32(math.Cos(float64(rot)))
		return lin.V2(pos.X+p.X*cs-p.Y*sn, pos.Y+p.X*sn+p.Y*cs)
	}
	line := func(a, b lin.Vec2) { g.StrokeLine(a.X, a.Y, b.X, b.Y, debugLineWidth, c) }
	loop := func(pts []lin.Vec2) {
		for i := range pts {
			line(at(pts[i]), at(pts[(i+1)%len(pts)]))
		}
	}
	switch sh := s.(type) {
	case Circle:
		g.StrokeCircle(pos.X, pos.Y, sh.Radius, debugLineWidth, c)
		line(pos, at(lin.V2(sh.Radius, 0))) // a spoke, so rotation shows
	case Box2:
		loop([]lin.Vec2{{X: -sh.HalfW, Y: -sh.HalfH}, {X: sh.HalfW, Y: -sh.HalfH},
			{X: sh.HalfW, Y: sh.HalfH}, {X: -sh.HalfW, Y: sh.HalfH}})
	case Polygon2:
		loop(sh.Points)
	case Capsule2:
		a, b := at(lin.V2(0, -sh.HalfHeight)), at(lin.V2(0, sh.HalfHeight))
		g.StrokeCircle(a.X, a.Y, sh.Radius, debugLineWidth, c)
		g.StrokeCircle(b.X, b.Y, sh.Radius, debugLineWidth, c)
		d := at(lin.V2(sh.Radius, 0)).Sub(pos)
		line(a.Add(d), b.Add(d))
		line(a.Sub(d), b.Sub(d))
	case Edge2:
		line(at(sh.A), at(sh.B))
	case Chain2:
		for i := 0; i+1 < len(sh.Points); i++ {
			line(at(sh.Points[i]), at(sh.Points[i+1]))
		}
	}
}
