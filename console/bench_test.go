package console

// Benchmarks of the console's cost over a game while it is closed, while
// it is open over a long scrollback, and of printing into a full buffer.

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// benchRig is newRig at 1280x720 with validation off.
func benchRig(b *testing.B, opts Options) *rig {
	b.Helper()
	if err := vk.Load(); err != nil {
		b.Skipf("no Vulkan: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := render.Config{AppName: "console_bench", Log: log}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 1280, Height: 720}, true)
	if err != nil {
		b.Skipf("no renderer: %v", err)
	}
	gd, err := hook.NewGraphics(r)
	if err != nil {
		b.Fatal(err)
	}
	g := gd.Game().(*gfx.Graphics)
	g.SetView(1280, 720)
	rg := &rig{con: New(opts), in: newFeeder(), gfx: gd, g: g, scale: 1,
		frame: Frame{Width: 1280, Height: 720, Stats: Stats{FPS: 60, FrameMS: 16.7}}}
	b.Cleanup(func() {
		rg.con.Destroy()
		gd.Destroy()
		r.Destroy()
	})
	return rg
}

// benchFill prints n distinct log-like lines.
func benchFill(c *Console, n int) {
	for i := range n {
		c.Print(fmt.Sprintf("[%06d] render: frame submitted in %d.%02d ms with %d draws", i, i%17, i%100, i%500))
	}
}

// BenchmarkConsoleClosed is Draw with the console shut, which the engine
// calls every frame: it should cost next to nothing. It runs inside one
// open frame, since a closed console queues no drawing.
func BenchmarkConsoleClosed(b *testing.B) {
	r := benchRig(b, Options{Lines: 10000})
	benchFill(r.con, 10000)
	ok, err := r.gfx.Begin([4]float32{0, 0, 0, 1})
	if err != nil || !ok {
		b.Skip("no frame")
	}
	r.in.SetDrawing(true)
	b.ReportAllocs()
	for b.Loop() {
		if err := r.con.Draw(r); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	r.in.SetDrawing(false)
	if _, err := r.gfx.End(false); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkConsoleOpen draws whole frames with the drop-down open over
// ten thousand lines, at the bottom and scrolled back, against an empty
// frame as the baseline.
func BenchmarkConsoleOpen(b *testing.B) {
	for _, scroll := range []int{0, 5000} {
		b.Run(fmt.Sprint("scroll=", scroll), func(b *testing.B) {
			r := benchRig(b, Options{Lines: 10000})
			benchFill(r.con, 10000)
			r.con.SetOpen(true)
			r.draw(b)
			r.con.scroll = scroll
			r.draw(b)
			r.draw(b)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r.draw(b)
			}
		})
	}
	b.Run("empty_frame", func(b *testing.B) {
		r := benchRig(b, Options{})
		b.ReportAllocs()
		for b.Loop() {
			r.in.EndUpdate()
			ok, err := r.gfx.Begin([4]float32{0, 0, 0, 1})
			if err != nil || !ok {
				b.Skip("no frame")
			}
			if _, err := r.gfx.End(false); err != nil {
				b.Fatal(err)
			}
			r.in.EndFrame()
		}
	})
}

// BenchmarkConsolePrintFull prints one line into a console whose buffer
// is already full, which a log burst does line after line.
func BenchmarkConsolePrintFull(b *testing.B) {
	for _, lines := range []int{1000, 10000} {
		b.Run(fmt.Sprint("lines=", lines), func(b *testing.B) {
			c := New(Options{Lines: lines})
			benchFill(c, lines)
			b.ReportAllocs()
			for b.Loop() {
				c.Print("render: frame submitted")
			}
		})
	}
}
