package engine

import (
	"log/slog"
	"math"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

type textInputWindow struct {
	window
	width, height, pixelsW, pixelsH int
	density                         float64
	rect                            [4]float64
	calls                           int
}

func (w *textInputWindow) Size() (int, int)      { return w.width, w.height }
func (w *textInputWindow) PixelSize() (int, int) { return w.pixelsW, w.pixelsH }
func (w *textInputWindow) Scale() float64        { return w.density }
func (w *textInputWindow) SetTextInputRect(x, y, width, height float64) {
	w.rect = [4]float64{x, y, width, height}
	w.calls++
}

func newTextInputLoop(t *testing.T) (*loop, *textInputWindow) {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	r, err := render.NewRenderer(render.Config{AppName: "text input rectangle", Validation: true},
		render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface,
		vk.VkExtent2D{Width: 1280, Height: 720}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	gd, err := hook.NewGraphics(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gd.Destroy)
	w := &textInputWindow{}
	l := &loop{win: w, gfx: gd, ctx: &Context{win: w, Gfx: gd.Game().(*gfx.Graphics), Log: slog.Default()}}
	return l, w
}

func TestTextInputRectViewToWindow(t *testing.T) {
	l, w := newTextInputLoop(t)
	for _, tc := range []struct {
		name                            string
		width, height, pixelsW, pixelsH int
		density                         float64
		fixed                           bool
		scaling                         Scaling
		want                            [4]float64
	}{
		{"window points", 1280, 720, 1280, 720, 1, false, ScaleFit, [4]float64{100, 100, 10, 20}},
		{"window points HiDPI", 1280, 720, 2560, 1440, 2, false, ScaleFit, [4]float64{100, 100, 10, 20}},
		{"fixed doubled", 1280, 720, 1280, 720, 1, true, ScaleFit, [4]float64{200, 200, 20, 40}},
		{"fixed HiDPI", 1280, 720, 2560, 1440, 2, true, ScaleFit, [4]float64{200, 200, 20, 40}},
		{"fit rounded letterbox", 1000, 600, 1000, 600, 1, true, ScaleFit, [4]float64{156.25, 18 + 100.0*563/360, 15.625, 20.0 * 563 / 360}},
		{"fit pillarbox", 1280, 600, 1280, 600, 1, true, ScaleFit, [4]float64{106 + 100.0*1067/640, 100.0 * 600 / 360, 10.0 * 1067 / 640, 20.0 * 600 / 360}},
		{"integer bars", 1000, 600, 1000, 600, 1, true, ScaleInteger, [4]float64{280, 220, 10, 20}},
		{"integer downscale fallback", 320, 240, 320, 240, 1, true, ScaleInteger, [4]float64{50, 80, 5, 10}},
		{"stretch axes", 1000, 600, 1000, 600, 1, true, ScaleStretch, [4]float64{156.25, 100.0 * 600 / 360, 15.625, 20.0 * 600 / 360}},
		{"fractional density", 800, 600, 1000, 750, 1.25, true, ScaleFit, [4]float64{125, (93 + 100.0*563/360) / 1.25, 12.5, 20.0 * 563 / 360 / 1.25}},
		{"return to window points", 800, 600, 1000, 750, 1.25, false, ScaleFit, [4]float64{100, 100, 10, 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.width, w.height, w.pixelsW, w.pixelsH, w.density = tc.width, tc.height, tc.pixelsW, tc.pixelsH, tc.density
			l.cfg = Config{Scaling: tc.scaling}
			if tc.fixed {
				l.cfg.ViewWidth, l.cfg.ViewHeight = 640, 360
			}
			// Drive the real resize event path, reusing the Context so a
			// mapping retained from an earlier size or scale cannot pass.
			l.handleEvents([]platform.Event{{Kind: platform.EventResize}})
			calls := w.calls
			l.ctx.SetTextInputRect(100, 100, 10, 20)
			if w.calls != calls+1 {
				t.Fatalf("platform calls = %d, want %d", w.calls, calls+1)
			}
			for i, want := range tc.want {
				if !(math.Abs(w.rect[i]-want) <= 0.0001) {
					t.Errorf("rectangle[%d] = %g, want %g", i, w.rect[i], want)
				}
			}
			x, y := l.toView(w.rect[0], w.rect[1])
			right, bottom := l.toView(w.rect[0]+w.rect[2], w.rect[1]+w.rect[3])
			if !(math.Abs(float64(x-100)) <= 0.0001 && math.Abs(float64(y-100)) <= 0.0001 &&
				math.Abs(float64(right-110)) <= 0.0001 && math.Abs(float64(bottom-120)) <= 0.0001) {
				t.Errorf("pointer inverse = (%g, %g)-(%g, %g), want (100, 100)-(110, 120)", x, y, right, bottom)
			}
			dx, dy := l.toViewDelta(w.rect[2], w.rect[3])
			if !(math.Abs(float64(dx-10)) <= 0.0001 && math.Abs(float64(dy-20)) <= 0.0001) {
				t.Errorf("pointer delta inverse = (%g, %g), want (10, 20)", dx, dy)
			}
			l.ctx.SetTextInputRect(100, 100, 0, 20)
			if w.rect[2] != 0 {
				t.Errorf("zero-width caret became %g points wide", w.rect[2])
			}
		})
	}
}

func TestTextInputRectInvalidSizeAndRecovery(t *testing.T) {
	l, w := newTextInputLoop(t)
	for _, tc := range []struct {
		name   string
		cfg    Config
		change func(*textInputWindow)
	}{
		{"zero view width", Config{}, func(w *textInputWindow) { w.width = 0 }},
		{"zero view height", Config{}, func(w *textInputWindow) { w.height = 0 }},
		{"zero framebuffer width", Config{}, func(w *textInputWindow) { w.pixelsW = 0 }},
		{"zero framebuffer height", Config{}, func(w *textInputWindow) { w.pixelsH = 0 }},
		{"zero density", Config{}, func(w *textInputWindow) { w.density = 0 }},
		{"fixed minimized", Config{ViewWidth: 640, ViewHeight: 360}, func(w *textInputWindow) { w.pixelsW, w.pixelsH = 0, 0 }},
		{"stretch minimized", Config{ViewWidth: 640, ViewHeight: 360, Scaling: ScaleStretch}, func(w *textInputWindow) { w.pixelsW = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l.cfg = tc.cfg
			w.width, w.height, w.pixelsW, w.pixelsH, w.density = 1280, 720, 1280, 720, 1
			l.applySize()
			l.ctx.SetTextInputRect(100, 100, 10, 20)
			want, calls := w.rect, w.calls
			tc.change(w)
			l.handleEvents([]platform.Event{{Kind: platform.EventResize}})
			l.ctx.SetTextInputRect(100, 100, 10, 20)
			if w.calls != calls {
				t.Errorf("invalid size sent platform rectangle %v", w.rect)
			}
			w.width, w.height, w.pixelsW, w.pixelsH, w.density = 1280, 720, 1280, 720, 1
			l.handleEvents([]platform.Event{{Kind: platform.EventResize}})
			l.ctx.SetTextInputRect(100, 100, 10, 20)
			if w.calls != calls+1 || w.rect != want {
				t.Errorf("restored rectangle = %v, calls = %d; want %v, %d", w.rect, w.calls, want, calls+1)
			}
		})
	}
}
