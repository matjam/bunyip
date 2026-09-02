package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/lin"
)

// TestCamera2DAndLayers pans a 2D camera so a world-space rectangle lands
// in the view centre, draws a screen-space marker on a higher layer that
// was submitted first, and checks the layer order won.
func TestCamera2DAndLayers(t *testing.T) {
	g := newHeadless(t, 128, 128)
	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		// Submitted first but on layer 5: must end up on top.
		g.SetLayer(5)
		g.FillRect(60, 60, 8, 8, Color{0, 0, 1, 1})
		g.SetLayer(0)
		g.SetCamera2D(Camera2D{Position: lin.V2(1000, 1000), Zoom: 2})
		g.FillRect(1000-10, 1000-10, 20, 20, Color{1, 0, 0, 1}) // 40x40 on screen around the centre
		g.ScreenSpace()
		g.FillRect(0, 0, 8, 8, Color{0, 1, 0, 1})
		if img, err = g.end(true); err != nil {
			t.Fatal(err)
		}
	}
	if c := img.RGBAAt(64, 64); c != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("centre %v: the layer-5 blue marker should be on top", c)
	}
	if c := img.RGBAAt(50, 50); c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("(50,50) %v: expected the camera-framed red rectangle", c)
	}
	if c := img.RGBAAt(3, 3); c != (color.RGBA{0, 255, 0, 255}) {
		t.Errorf("(3,3) %v: expected the screen-space green marker", c)
	}
	if c := img.RGBAAt(100, 100); c != (color.RGBA{0, 0, 0, 255}) {
		t.Errorf("(100,100) %v: expected background", c)
	}
	cam := Camera2D{Position: lin.V2(1000, 1000), Zoom: 2}
	if p := cam.ViewToWorld(lin.V2(64, 64), 128, 128); math.Abs(float64(p.X-1000)) > 1e-3 {
		t.Errorf("ViewToWorld centre = %v", p)
	}
}

// TestTilemapAndNineSlice draws a two-frame sheet as a map and a nine-slice
// panel and checks representative pixels.
func TestTilemapAndNineSlice(t *testing.T) {
	g := newHeadless(t, 96, 64)
	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 4 {
				c = color.RGBA{0, 0, 255, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	sheet := NewSheet(tex, 4, 4)
	tm := NewTilemap(sheet, 2, 1)
	tm.Set(0, 0, 0)
	tm.Set(1, 0, 1)
	tm.TileW, tm.TileH = 16, 16
	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.DrawTilemap(tm, 0, 0, White)
		g.DrawNineSlice(NineSlice{Tex: tex, Left: 2, Top: 1, Right: 2, Bottom: 1}, lin.R(40, 0, 48, 32), White)
		if img, err = g.end(true); err != nil {
			t.Fatal(err)
		}
	}
	if c := img.RGBAAt(8, 8); c.R < 200 {
		t.Errorf("tile 0 %v should be red", c)
	}
	if c := img.RGBAAt(24, 8); c.B < 200 {
		t.Errorf("tile 1 %v should be blue", c)
	}
	if c := img.RGBAAt(41, 16); c.R < 200 {
		t.Errorf("nine-slice left edge %v should be red", c)
	}
	if c := img.RGBAAt(86, 16); c.B < 200 {
		t.Errorf("nine-slice right edge %v should be blue", c)
	}
}

func TestAnimationAndTransform(t *testing.T) {
	var st AnimState
	st.Play(&Animation{Frames: []int{3, 4, 5}, FPS: 10, Loop: false})
	if st.Frame() != 3 {
		t.Errorf("first frame %d", st.Frame())
	}
	st.Advance(0.15)
	if st.Frame() != 4 {
		t.Errorf("frame at 0.15 s = %d, want 4", st.Frame())
	}
	st.Advance(1)
	if st.Frame() != 5 || !st.Done {
		t.Errorf("should hold the last frame and be done: %d %v", st.Frame(), st.Done)
	}
	tf := At(1, 2, 3).Rotated(lin.V3(0, 1, 0), lin.Radians(90)).Scaled(2)
	p := tf.Matrix().MulPoint(lin.V3(1, 0, 0))
	if math.Abs(float64(p.X-1)) > 1e-4 || math.Abs(float64(p.Z-1)) > 1e-4 {
		t.Errorf("transformed point %v, want (1,2,1)", p)
	}
	cam := OrbitCamera(lin.V3(0, 0, 0), 0, 0, 5)
	if cam.Position.Z != 5 {
		t.Errorf("orbit camera at yaw 0 should sit on +Z: %v", cam.Position)
	}
}

func TestTextLayout(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 14, FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	lines := f.Layout("the quick brown fox jumps over the lazy dog", TextOptions{Width: 120})
	if len(lines) < 3 {
		t.Errorf("expected wrapping into several lines, got %q", lines)
	}
	for _, l := range lines {
		if w, _ := f.Measure(l, TextOptions{}); w > 120 {
			t.Errorf("line %q is %.0f wide", l, w)
		}
	}
	if got := f.Layout("a\nb", TextOptions{}); len(got) != 2 {
		t.Errorf("newline split: %q", got)
	}
	w, h := f.Measure("hello\nworld", TextOptions{})
	if h < 2*f.LineHeight-0.01 || w <= 0 {
		t.Errorf("block %v x %v", w, h)
	}
}
