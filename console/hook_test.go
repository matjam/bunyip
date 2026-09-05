package console

import (
	"image"
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// The tests drive a console the way the engine loop does: through
// internal/hook, which owns the frame boundaries and the event feed.
// feeder scripts input; rig holds one console, its graphics and the
// frame it draws from.

// feeder is an input State plus the driver the engine pushes events
// through, with the typed methods a test writing input wants.
type feeder struct {
	hook.Input
	state *input.State
}

func newFeeder() *feeder {
	d := hook.NewInput()
	return &feeder{Input: d, state: d.Game().(*input.State)}
}

func (f *feeder) FeedKey(k input.Key, down, repeat bool, mods input.Mods) {
	f.Input.FeedKey(uint8(k), down, repeat, uint8(mods))
}

func (f *feeder) FeedMouseButton(b input.MouseButton, down bool, x, y float32) {
	f.Input.FeedMouseButton(uint8(b), down, x, y)
}

// press feeds a key going down and up again, as one keystroke.
func (f *feeder) press(k input.Key) {
	f.FeedKey(k, true, false, 0)
	f.FeedKey(k, false, false, 0)
}

// typed feeds the characters of a string.
func (f *feeder) typed(s string) {
	for _, r := range s {
		f.Input.FeedChar(r)
	}
}

// rig is a console under test with everything it draws through.
type rig struct {
	con    *Console
	in     *feeder
	gfx    hook.Graphics
	g      *gfx.Graphics
	frame  Frame
	frames uint64
	quit   bool
	shot   string
	scale  float64
}

// ConsoleFrame makes the rig a Host, as bunyip.Context is in a game.
func (r *rig) ConsoleFrame() Frame {
	f := r.frame
	f.Gfx, f.Input, f.FrameCount = r.g, r.in.state, r.frames
	f.Quit = func() { r.quit = true }
	f.Screenshot = func(path string) { r.shot = path }
	f.SetTimeScale = func(s float64) { r.scale = s }
	f.TimeScale = func() float64 { return r.scale }
	return f
}

// newRig builds a console on an offscreen surface, skipping the test
// when the machine has no Vulkan driver.
func newRig(t testing.TB, opts Options) *rig {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "console_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 640, Height: 480}, true)
	if err != nil {
		t.Fatal(err)
	}
	gd, err := hook.NewGraphics(r)
	if err != nil {
		t.Fatal(err)
	}
	g := gd.Game().(*gfx.Graphics)
	g.SetView(640, 480)
	rg := &rig{con: New(opts), in: newFeeder(), gfx: gd, g: g, scale: 1,
		frame: Frame{Width: 640, Height: 480, Stats: Stats{FPS: 60, FrameMS: 16.7}}}
	t.Cleanup(func() {
		rg.con.Destroy()
		gd.Destroy()
		r.Destroy()
	})
	return rg
}

// draw runs one frame the way the engine does: the update's edges are
// cleared, then the console draws inside an open frame.
func (r *rig) draw(t testing.TB) {
	t.Helper()
	r.in.EndUpdate()
	r.in.SetDrawing(true)
	defer func() {
		r.in.SetDrawing(false)
		r.in.EndFrame()
	}()
	ok, err := r.gfx.Begin([4]float32{0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return
	}
	if err := r.con.Draw(r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gfx.End(false); err != nil {
		t.Fatal(err)
	}
	r.frames++
}

// capture runs one frame and returns the image it drew, for a test that
// checks the console reached the screen.
func (r *rig) capture(t testing.TB) *image.RGBA {
	t.Helper()
	r.in.EndUpdate()
	r.in.SetDrawing(true)
	defer func() {
		r.in.SetDrawing(false)
		r.in.EndFrame()
	}()
	ok, err := r.gfx.Begin([4]float32{0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the frame was skipped")
	}
	if err := r.con.Draw(r); err != nil {
		t.Fatal(err)
	}
	img, err := r.gfx.End(true)
	if err != nil {
		t.Fatal(err)
	}
	r.frames++
	return img
}

// drawN runs n frames, for the hover and glyph lags the interface has.
func (r *rig) drawN(t testing.TB, n int) {
	t.Helper()
	for range n {
		r.draw(t)
	}
}

// output is every line the console has printed, joined by newlines.
func (r *rig) output() string {
	out := ""
	for _, l := range r.con.Lines() {
		out += l + "\n"
	}
	return out
}
