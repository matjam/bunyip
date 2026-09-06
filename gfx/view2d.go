package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// View2D maps a local 2D coordinate space into a rectangle of its enclosing
// view. Viewport is in enclosing view units, independent of camera and
// transform state. Size is the local virtual size; a zero component uses
// the matching viewport dimension. Coordinates outside the viewport clip.
//
// Viewport dimensions must be positive, Size components nonnegative, and
// all values finite. Mapping methods and WithView panic on invalid values.
type View2D struct {
	Viewport lin.Rect
	Size     lin.Vec2
}

func (v View2D) resolved() (lin.Vec2, lin.Affine) {
	r, size := v.Viewport, v.Size
	for _, x := range []float32{r.X, r.Y, r.W, r.H, r.X + r.W, r.Y + r.H, size.X, size.Y} {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			panic("gfx: view coordinates must be finite")
		}
	}
	if r.W <= 0 || r.H <= 0 || size.X < 0 || size.Y < 0 {
		panic("gfx: view needs a positive viewport and nonnegative virtual size")
	}
	if size.X == 0 {
		size.X = r.W
	}
	if size.Y == 0 {
		size.Y = r.H
	}
	sx, sy := r.W/size.X, r.H/size.Y
	if sx == 0 || sy == 0 || math.IsInf(float64(sx), 0) || math.IsInf(float64(sy), 0) {
		panic("gfx: view scale is out of range")
	}
	return size, lin.Translate2(r.X, r.Y).Mul(lin.Scale2(sx, sy))
}

// LocalToParent maps a local view point into the enclosing view. It does
// not apply a camera or clamp points to the viewport.
func (v View2D) LocalToParent(p lin.Vec2) lin.Vec2 {
	_, transform := v.resolved()
	return transform.Apply(p)
}

// ParentToLocal maps an enclosing view point, such as pointer input, into
// local view coordinates. Points outside the viewport are not clamped.
func (v View2D) ParentToLocal(p lin.Vec2) lin.Vec2 {
	_, transform := v.resolved()
	return lin.V2((p.X-transform.C)/transform.A, (p.Y-transform.F)/transform.E)
}

// WorldToParent maps a world point through camera and into the enclosing
// view, using this view's resolved virtual size.
func (v View2D) WorldToParent(p lin.Vec2, camera Camera2D) lin.Vec2 {
	size, transform := v.resolved()
	return transform.Apply(camera.WorldToView(p, size.X, size.Y))
}

// ParentToWorld maps an enclosing view point through the inverse camera
// into world coordinates. Test Viewport.Contains first when input outside
// the view should be ignored. Nested views map through each parent in turn.
func (v View2D) ParentToWorld(p lin.Vec2, camera Camera2D) lin.Vec2 {
	size, transform := v.resolved()
	local := lin.V2((p.X-transform.C)/transform.A, (p.Y-transform.F)/transform.E)
	return camera.ViewToWorld(local, size.X, size.Y)
}

// WithView draws in a local 2D view, clipped to its viewport and enclosing
// clips. It inherits the current camera, recalculated for the virtual size;
// WithCamera2D can select another camera inside. The viewport itself is in
// enclosing view coordinates and is not moved by a camera or transform.
//
// View geometry is validated before any state changes. The previous view,
// camera and clips are restored even on panic; queued draws remain queued.
// This affects sprites, paths, text, geometry and 2D particles, not 3D passes.
// Use render textures for separately rendered 3D cameras.
// SetView and SetViewport panic inside the closure; configure the main output
// outside view scopes. DrawTo may select another target normally.
func (g *Graphics) WithView(view View2D, draw func()) {
	size, local := view.resolved()
	q := g.cur
	w, h, proj, spriteProj := q.viewW, q.viewH, q.proj, q.spriteProj
	layout, clips := q.layout, q.clips
	childLayout, childProj := layout.Mul(local), proj.Mul(local.Mat4())
	for _, x := range append(childProj[:], childLayout.A, childLayout.C, childLayout.E, childLayout.F) {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			panic("gfx: composed view coordinates must be finite")
		}
	}
	if childLayout.A == 0 || childLayout.E == 0 || childProj[0] == 0 || childProj[5] == 0 {
		panic("gfx: composed view scale is out of range")
	}
	cam, hasCam, visible := q.cam2D, q.hasCam2D, q.visible
	g.viewScopes++
	defer func() {
		g.viewScopes--
		q.viewW, q.viewH, q.proj, q.spriteProj = w, h, proj, spriteProj
		q.layout, q.clips = layout, clips
		q.cam2D, q.hasCam2D, q.visible = cam, hasCam, visible
	}()
	// PushClip converts from the enclosing local coordinates into root
	// coordinates before we change the layout used by child clips.
	g.PushClip(view.Viewport)
	q.viewW, q.viewH = size.X, size.Y
	q.layout = childLayout
	q.proj = childProj
	q.spriteProj = q.proj
	if q.hasCam2D {
		g.SetCamera2D(q.cam2D)
	}
	draw()
}

// drawingPixelScale is framebuffer pixels per current local view unit on
// X. Layout scale is independent of camera zoom and the drawing transform.
func (g *Graphics) drawingPixelScale() float32 {
	q := g.cur
	if q == nil || q.rootW <= 0 {
		return 1
	}
	pixels := q.pixelW
	if q == g.main {
		pixels = float32(g.mainExtent().Width)
	}
	return pixels / q.rootW * q.layout.A
}

func (g *Graphics) frame2DState() lin.Vec4 {
	q := g.cur
	return lin.V4(g.time, q.viewW, q.viewH, g.drawingPixelScale())
}
