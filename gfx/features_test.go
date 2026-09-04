package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// Within a layer, a lower sort key draws first whatever the submission
// order; equal keys keep it.
func TestSortKeyOrdersWithinLayer(t *testing.T) {
	g := newHeadless(t, 16, 16)
	img := render2D(t, g, Black, func() {
		// Drawn second but keyed lower, so the red rectangle ends up under
		// the green one.
		g.SetSortKey(2)
		g.FillRect(0, 0, 16, 16, Color{G: 1, A: 1})
		g.SetSortKey(1)
		g.FillRect(0, 0, 16, 16, Color{R: 1, A: 1})
		g.SetSortKey(0)
	})
	if c := img.RGBAAt(8, 8); c.G < 200 || c.R > 30 {
		t.Errorf("keyed draws in the wrong order: %v", c)
	}
	// Sort keys do not cross layers.
	img = render2D(t, g, Black, func() {
		g.SetLayer(1)
		g.SetSortKey(-5)
		g.FillRect(0, 0, 16, 16, Color{R: 1, A: 1})
		g.SetLayer(0)
		g.SetSortKey(5)
		g.FillRect(0, 0, 16, 16, Color{G: 1, A: 1})
		g.SetSortKey(0)
		g.SetLayer(0)
	})
	if c := img.RGBAAt(8, 8); c.R < 200 {
		t.Errorf("a sort key overrode the layer: %v", c)
	}
}

// Sprites outside the 2D camera's view are dropped and counted; ones
// inside, or partly inside, are kept.
func TestSpriteCulling(t *testing.T) {
	g := newHeadless(t, 32, 32)
	render2D(t, g, Black, func() {
		g.SetCamera2D(Camera2D{Position: lin.V2(16, 16)})
		g.FillRect(1000, 1000, 4, 4, White) // far away
		g.FillRect(30, 30, 8, 8, White)     // straddles the edge
		// Centred just outside the left edge but rotated, so a corner may
		// reach in: kept.
		g.Draw(nil, Sprite{Pos: lin.V2(-3, 16), Size: lin.V2(8, 8), Rotation: 1, Origin: lin.V2(0.5, 0.5)})
		g.ScreenSpace()
	})
	if s := g.Stats(); s.Culled2D != 1 {
		t.Errorf("culled %d sprites, want 1", s.Culled2D)
	}
	if !spriteVisible(Sprite{Pos: lin.V2(0, 0), Size: lin.V2(4, 4)}, lin.Identity2(), lin.R(2, 2, 10, 10)) {
		t.Error("a sprite touching the view was culled")
	}
	if spriteVisible(Sprite{Pos: lin.V2(0, 0), Size: lin.V2(4, 4)}, lin.Identity2(), lin.R(10, 10, 10, 10)) {
		t.Error("a sprite clear of the view was kept")
	}
}

// Culling tests the sprite's own quad, so a long thin rotated sprite is
// kept only while that quad meets the view, and a sprite moved by the
// transform stack is tested where it lands.
func TestSpriteCullingQuad(t *testing.T) {
	view := lin.R(0, 0, 100, 100)
	// A 200 by 4 bar turned 45 degrees about its middle, centred beyond
	// the bottom-right corner. It points back at the view and its end
	// lands inside.
	bar := Sprite{Pos: lin.V2(150, 150), Size: lin.V2(200, 4), Origin: lin.V2(0.5, 0.5), Rotation: math.Pi / 4}
	if !spriteVisible(bar, lin.Identity2(), view) {
		t.Error("a rotated bar reaching into the view was culled")
	}
	// The same bar turned the other way lies along the far diagonal,
	// seventy units clear of the corner. The circle around it, which is
	// what culling used to test, would keep it.
	bar.Rotation = 3 * math.Pi / 4
	if spriteVisible(bar, lin.Identity2(), view) {
		t.Error("a rotated bar clear of the view was kept")
	}
	// The transform stack moves a sprite before it is tested.
	s := Sprite{Pos: lin.V2(10, 10), Size: lin.V2(8, 8)}
	if spriteVisible(s, lin.Translate2(500, 0), view) {
		t.Error("a sprite translated out of the view was kept")
	}
	if !spriteVisible(s, lin.Translate2(500, 0), lin.R(500, 0, 100, 100)) {
		t.Error("a sprite translated into the view was culled")
	}
	if s := (Sprite{Pos: lin.V2(50, 50)}); !spriteVisible(s, lin.Identity2(), view) {
		t.Error("a sprite with no size inside the view was culled")
	}
}

// Culling applies under the transform stack, which it used to skip.
func TestSpriteCullingUnderTransform(t *testing.T) {
	g := newHeadless(t, 32, 32)
	render2D(t, g, Black, func() {
		g.SetCamera2D(Camera2D{Position: lin.V2(16, 16)})
		g.Transformed(lin.Translate2(1000, 0), func() {
			g.FillRect(0, 0, 8, 8, White) // pushed out of the view
		})
		g.Transformed(lin.Translate2(8, 8), func() {
			g.FillRect(0, 0, 8, 8, White) // still inside
		})
		g.ScreenSpace()
	})
	if s := g.Stats(); s.Culled2D != 1 {
		t.Errorf("culled %d sprites under a transform, want 1", s.Culled2D)
	}
}

func TestCamera2DFollowClampShake(t *testing.T) {
	var c Camera2D
	c.Follow(lin.V2(100, 0), 0, 1) // zero rate snaps
	if c.Position != lin.V2(100, 0) {
		t.Errorf("snap: %v", c.Position)
	}
	c.Follow(lin.V2(0, 0), 5, 1.0/60)
	if c.Position.X >= 100 || c.Position.X <= 90 {
		t.Errorf("one step of following moved to %v", c.Position)
	}
	// Following is frame-rate independent: sixty small steps equal one
	// large one.
	a := Camera2D{Position: lin.V2(100, 0)}
	b := a
	for range 60 {
		a.Follow(lin.V2(0, 0), 5, 1.0/60)
	}
	b.Follow(lin.V2(0, 0), 5, 1)
	if math.Abs(float64(a.Position.X-b.Position.X)) > 0.01 {
		t.Errorf("60 small steps %v, one step %v", a.Position.X, b.Position.X)
	}
	// Clamp keeps the view inside the world, or centres on a small one.
	c = Camera2D{Position: lin.V2(-50, 500)}
	c.Clamp(lin.R(0, 0, 1000, 1000), 200, 100)
	if c.Position != lin.V2(100, 500) {
		t.Errorf("clamped to %v", c.Position)
	}
	c.Clamp(lin.R(0, 0, 100, 40), 200, 100)
	if c.Position != lin.V2(50, 20) {
		t.Errorf("centred on a small world at %v", c.Position)
	}
	// Shake offsets the view while it runs and settles afterwards.
	c = Camera2D{Position: lin.V2(10, 10)}
	c.Shake(4, 0.5)
	moved := false
	for range 10 {
		c.Advance(1.0 / 60)
		if c.centre() != c.Position {
			moved = true
		}
		if d := c.centre().Sub(c.Position); math.Abs(float64(d.X)) > 4 || math.Abs(float64(d.Y)) > 4 {
			t.Errorf("shake offset %v exceeds its amplitude", d)
		}
	}
	if !moved || !c.Shaking() {
		t.Error("shake did not move the view")
	}
	for range 60 {
		c.Advance(1.0 / 60)
	}
	if c.Shaking() || c.centre() != c.Position {
		t.Errorf("shake did not settle: %v", c.centre())
	}
}

// Camera.Project and ScreenRay answer the same as the Graphics versions
// given the view size.
func TestCameraPickingMatchesGraphics(t *testing.T) {
	g := newHeadless(t, 64, 48)
	cam := Camera{Position: lin.V3(0, 2, 6), Target: lin.V3(0, 0, 0)}
	render2D(t, g, Black, func() {
		g.SetCamera(cam)
		p := lin.V3(0.5, 0.2, -1)
		x1, y1, ok1 := g.Project(p)
		x2, y2, ok2 := cam.Project(p, 64, 48)
		if ok1 != ok2 || math.Abs(float64(x1-x2)) > 1e-3 || math.Abs(float64(y1-y2)) > 1e-3 {
			t.Errorf("Project: %v %v %v vs %v %v %v", x1, y1, ok1, x2, y2, ok2)
		}
		r1, r2 := g.ScreenRay(20, 10), cam.ScreenRay(20, 10, 64, 48)
		if r1.Origin.Sub(r2.Origin).Len() > 1e-3 || r1.Dir.Sub(r2.Dir).Len() > 1e-3 {
			t.Errorf("ScreenRay: %v vs %v", r1, r2)
		}
	})
}

func TestRegionAnimationAt(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	g := newHeadless(t, 8, 8)
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	frames := []Region{NewRegion(tex, lin.R(0, 0, 1, 1)), NewRegion(tex, lin.R(1, 0, 1, 1)), NewRegion(tex, lin.R(2, 0, 1, 1))}
	a := RegionAnimation{Frames: frames, Durations: []float32{0.1, 0.3, 0.1}, Loop: true}
	if l := a.Length(); math.Abs(l-0.5) > 1e-6 {
		t.Errorf("length %v", l)
	}
	for _, tc := range []struct {
		t    float64
		want int
	}{{0, 0}, {0.05, 0}, {0.15, 1}, {0.35, 1}, {0.45, 2}, {0.55, 0}, {1.15, 1}} {
		if f, done := a.At(tc.t); f != frames[tc.want] || done {
			t.Errorf("At(%v) gave frame %v done=%v, want frame %d", tc.t, f.UV0, done, tc.want)
		}
	}
	once := RegionAnimation{Frames: frames, Durations: []float32{0.1, 0.1, 0.1}}
	if f, done := once.At(5); f != frames[2] || !done {
		t.Errorf("a finished one-shot gave %v done=%v", f.UV0, done)
	}
	if _, done := (RegionAnimation{}).At(0); !done {
		t.Error("an empty animation is not done")
	}
}

// Dropped lights are counted so a scene can tell it went over MaxLights.
func TestLightsDroppedCounted(t *testing.T) {
	g := newHeadless(t, 8, 8)
	render2D(t, g, Black, func() {
		for range MaxLights + 3 {
			g.AddPointLight(lin.V3(0, 0, 0), White, 5)
		}
	})
	if s := g.Stats(); s.LightsDropped != 3 {
		t.Errorf("LightsDropped = %d, want 3", s.LightsDropped)
	}
}
