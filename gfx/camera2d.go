package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Camera2D frames a region of a 2D world: Position is the world point at
// the centre of the view, Zoom scales world units to view units (2 shows
// half as much), Rotation is radians anticlockwise.
type Camera2D struct {
	Position lin.Vec2
	Zoom     float32 // zero means 1
	Rotation float32
}

// Matrix returns the world-to-view transform for a view of the given size.
func (c Camera2D) Matrix(viewW, viewH float32) lin.Mat4 {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	centre := lin.Translate(lin.V3(viewW/2, viewH/2, 0))
	scale := lin.Scale(lin.V3(zoom, zoom, 1))
	rot := lin.Rotate(-c.Rotation, lin.V3(0, 0, 1))
	move := lin.Translate(lin.V3(-c.Position.X, -c.Position.Y, 0))
	return centre.Mul(scale).Mul(rot).Mul(move)
}

// WorldToView maps a world point through the camera.
func (c Camera2D) WorldToView(p lin.Vec2, viewW, viewH float32) lin.Vec2 {
	v := c.Matrix(viewW, viewH).MulPoint(lin.V3(p.X, p.Y, 0))
	return lin.V2(v.X, v.Y)
}

// ViewToWorld maps a view point (for example the mouse) back to the world.
func (c Camera2D) ViewToWorld(p lin.Vec2, viewW, viewH float32) lin.Vec2 {
	v := c.Matrix(viewW, viewH).Inverse().MulPoint(lin.V3(p.X, p.Y, 0))
	return lin.V2(v.X, v.Y)
}

// VisibleRect is the world-space box the camera can see, conservatively
// enlarged when rotated.
func (c Camera2D) VisibleRect(viewW, viewH float32) (minX, minY, maxX, maxY float32) {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	hw, hh := viewW/2/zoom, viewH/2/zoom
	if c.Rotation != 0 {
		r := float32(math.Hypot(float64(hw), float64(hh)))
		hw, hh = r, r
	}
	return c.Position.X - hw, c.Position.Y - hh, c.Position.X + hw, c.Position.Y + hh
}

// SetCamera2D makes later sprite draws world-space under cam. Call
// ScreenSpace to return to view coordinates for interface drawing.
func (g *Graphics) SetCamera2D(cam Camera2D) {
	q := g.cur
	q.cam2D, q.hasCam2D = cam, true
	q.spriteProj = q.proj.Mul(cam.Matrix(q.viewW, q.viewH))
}

// ScreenSpace returns sprite drawing to view coordinates.
func (g *Graphics) ScreenSpace() {
	q := g.cur
	q.hasCam2D = false
	q.spriteProj = q.proj
}

// Camera2D returns the active 2D camera and whether one is set.
func (g *Graphics) Camera2D() (Camera2D, bool) { return g.cur.cam2D, g.cur.hasCam2D }

// SetLayer sets the sort layer for later sprite draws. Sprites draw in
// ascending layer order and, within a layer, in submission order. Text and
// interface drawing typically use a high layer.
func (g *Graphics) SetLayer(layer int) { g.cur.layer = int32(layer) }

// Layer returns the current sprite layer.
func (g *Graphics) Layer() int { return int(g.cur.layer) }
