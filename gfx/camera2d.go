package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Camera2D frames a region of a 2D world: Position is the world point at
// the centre of the view, Zoom scales world units to view units (2 shows
// half as much), Rotation is radians anticlockwise. Follow, Clamp and
// Shake move it over time; a camera used by value is still valid, it
// just has no motion of its own.
type Camera2D struct {
	Position lin.Vec2
	Zoom     float32 // zero means 1
	Rotation float32

	// A shake in progress: how much is left, how long it was, how far it
	// throws the view, and the offset it currently adds to Position.
	shakeLeft, shakeTotal, shakeAmp float32
	shakeOffset                     lin.Vec2
	shakeSeed                       uint32
}

// centre is the position the view is really framed on: Position plus the
// shake in progress.
func (c Camera2D) centre() lin.Vec2 { return c.Position.Add(c.shakeOffset) }

// Matrix returns the world-to-view transform for a view of the given size.
func (c Camera2D) Matrix(viewW, viewH float32) lin.Mat4 {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	centre := lin.Translate(lin.V3(viewW/2, viewH/2, 0))
	scale := lin.Scale(lin.V3(zoom, zoom, 1))
	rot := lin.Rotate(-c.Rotation, lin.V3(0, 0, 1))
	p := c.centre()
	move := lin.Translate(lin.V3(-p.X, -p.Y, 0))
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
func (c Camera2D) VisibleRect(viewW, viewH float32) lin.Rect {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	hw, hh := viewW/2/zoom, viewH/2/zoom
	if c.Rotation != 0 {
		r := float32(math.Hypot(float64(hw), float64(hh)))
		hw, hh = r, r
	}
	return lin.RectAround(c.centre(), 2*hw, 2*hh)
}

// Follow moves the camera towards a target, closing the gap at rate per
// second: 5 trails the player softly, 20 keeps close, and zero snaps.
// The motion is the same at any frame rate. Call it from Update with the
// step, or from Draw with the frame's time.
func (c *Camera2D) Follow(target lin.Vec2, rate float32, dt float64) {
	if rate <= 0 || dt <= 0 {
		c.Position = target
		return
	}
	t := 1 - float32(math.Exp(-float64(rate)*dt))
	c.Position = c.Position.Add(target.Sub(c.Position).Mul(t))
}

// Clamp keeps the view inside a world rectangle, so the camera stops at
// the edge of a level rather than showing what lies beyond it. Where the
// rectangle is narrower or shorter than the view, the view is centred on
// it along that axis. Rotation is ignored.
func (c *Camera2D) Clamp(bounds lin.Rect, viewW, viewH float32) {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	hw, hh := viewW/2/zoom, viewH/2/zoom
	if bounds.W <= 2*hw {
		c.Position.X = bounds.X + bounds.W/2
	} else {
		c.Position.X = lin.Clamp(c.Position.X, bounds.X+hw, bounds.X+bounds.W-hw)
	}
	if bounds.H <= 2*hh {
		c.Position.Y = bounds.Y + bounds.H/2
	} else {
		c.Position.Y = lin.Clamp(c.Position.Y, bounds.Y+hh, bounds.Y+bounds.H-hh)
	}
}

// Shake throws the view about by up to amplitude world units, fading
// out over seconds: an explosion, a heavy landing. A shake started while
// one is running takes the larger amplitude and the longer time left.
// Advance runs it.
func (c *Camera2D) Shake(amplitude, seconds float32) {
	if amplitude <= 0 || seconds <= 0 {
		return
	}
	c.shakeAmp = max(c.shakeAmp, amplitude)
	c.shakeLeft = max(c.shakeLeft, seconds)
	c.shakeTotal = max(c.shakeTotal, seconds)
	if c.shakeSeed == 0 {
		c.shakeSeed = 0x9E3779B9
	}
}

// Advance steps the camera's shake by dt seconds; call it once per
// update. Without a shake in progress it does nothing.
func (c *Camera2D) Advance(dt float64) {
	if c.shakeLeft <= 0 {
		c.shakeOffset = lin.Vec2{}
		c.shakeAmp, c.shakeTotal = 0, 0
		return
	}
	c.shakeLeft -= float32(dt)
	if c.shakeLeft <= 0 {
		c.shakeLeft, c.shakeAmp, c.shakeTotal = 0, 0, 0
		c.shakeOffset = lin.Vec2{}
		return
	}
	// A fresh random direction each step, scaled by what is left, so the
	// shake decays rather than stops.
	strength := c.shakeAmp * c.shakeLeft / c.shakeTotal
	c.shakeOffset = lin.V2(c.shakeRand()*strength, c.shakeRand()*strength)
}

// Shaking reports whether a shake is in progress.
func (c *Camera2D) Shaking() bool { return c.shakeLeft > 0 }

// shakeRand is a value in [-1, 1] from a small generator the camera
// carries, so shaking needs no other state and is the same for the same
// sequence of calls.
func (c *Camera2D) shakeRand() float32 {
	c.shakeSeed ^= c.shakeSeed << 13
	c.shakeSeed ^= c.shakeSeed >> 17
	c.shakeSeed ^= c.shakeSeed << 5
	return float32(c.shakeSeed>>8)/float32(1<<24)*2 - 1
}

// SetCamera2D makes later sprite draws world-space under cam. Call
// ScreenSpace to return to view coordinates for interface drawing.
// Sprites wholly outside the camera's view are dropped before they reach
// the vertex stream.
func (g *Graphics) SetCamera2D(cam Camera2D) {
	q := g.cur
	q.cam2D, q.hasCam2D = cam, true
	q.spriteProj = q.proj.Mul(cam.Matrix(q.viewW, q.viewH))
	q.visible = cam.VisibleRect(q.viewW, q.viewH)
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
// ascending layer order and, within a layer, by sort key (SetSortKey)
// and then in submission order. Text and interface drawing typically
// use a high layer.
func (g *Graphics) SetLayer(layer int) { g.cur.layer = int32(layer) }

// Layer returns the current sprite layer.
func (g *Graphics) Layer() int { return int(g.cur.layer) }

// SetSortKey orders later sprite draws within their layer: draws with a
// lower key are drawn first, and equal keys keep submission order. A
// game that sorts by depth sets the key to each sprite's feet, so a
// character standing lower on the screen draws over one behind it,
// without ordering its own draw calls. Zero, the default, keeps
// submission order alone.
func (g *Graphics) SetSortKey(key float32) { g.cur.sortKey = key }

// SortKey returns the current sort key.
func (g *Graphics) SortKey() float32 { return g.cur.sortKey }
