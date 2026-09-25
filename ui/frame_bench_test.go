package ui

// Benchmarks of whole interface frames. Unlike newContext, the context
// here runs with validation off, since the validation layers would
// dominate a frame's cost.

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// benchContext is newContext at 1280x720 without validation.
func benchContext(b *testing.B) *Context {
	b.Helper()
	if err := vk.Load(); err != nil {
		b.Skipf("no Vulkan: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := render.Config{AppName: "ui_bench", Log: log}
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
	font, err := g.NewFont(goregular.TTF, 14, gfx.FontOptions{AtlasSize: 1024})
	if err != nil {
		b.Fatal(err)
	}
	c := New(g, DarkTheme(font))
	drivers[c] = gd
	b.Cleanup(func() { delete(drivers, c); font.Destroy(); gd.Destroy(); r.Destroy() })
	return c
}

// benchUIState is what the realistic frame edits.
type benchUIState struct {
	checks  [40]bool
	values  [40]float32
	texts   [40]string
	cells   [1000][3]string
	tree    []string
	winRect Rect
}

func newBenchUIState() *benchUIState {
	s := &benchUIState{winRect: Rect{X: 900, Y: 60, W: 360, H: 640}}
	for i := range s.texts {
		s.texts[i] = fmt.Sprintf("field text %d", i)
	}
	for i := range s.cells {
		s.cells[i] = [3]string{fmt.Sprintf("Item %d", i), fmt.Sprintf("%d gold", i*7), fmt.Sprintf("slot %d", i%40)}
	}
	for i := range 50 {
		s.tree = append(s.tree, fmt.Sprintf("node %d", i))
	}
	return s
}

// benchUIFrame is a realistic tool or settings frame: a menu bar, a
// panel of two hundred widgets, a scrolled table of rows and a window
// holding an open tree of fifty nodes.
func benchUIFrame(c *Context, s *benchUIState, rows int) {
	c.MenuBar(Rect{X: 0, Y: 0, W: 1280, H: 24}, func() {
		for _, m := range []string{"File", "Edit", "View"} {
			c.Menu(m, func() {
				c.MenuItem("Open")
				c.MenuItem("Save")
			})
		}
	})
	c.Panel("Widgets", Rect{X: 0, Y: 30, W: 420, H: 690}, func() {
		for i := range 40 {
			c.Button("Apply " + s.texts[i][11:])
			c.Checkbox("Enabled "+s.texts[i][11:], &s.checks[i])
			c.Slider("Volume "+s.texts[i][11:], &s.values[i], 0, 1)
			c.Label("A plain label " + s.texts[i][11:])
			c.TextField("Name "+s.texts[i][11:], &s.texts[i])
		}
	})
	c.Panel("Inventory", Rect{X: 430, Y: 30, W: 460, H: 690}, func() {
		c.ScrollArea("rows", Rect{X: 436, Y: 60, W: 448, H: 650}, float32(rows+1)*(c.Theme.RowHeight+c.Theme.Spacing), func() {
			c.Table([]string{"Name", "Value", "Slot"}, nil, rows, func(row, col int) {
				c.Cell(s.cells[row][col])
			})
		})
	})
	c.Window("Scene", &s.winRect, func() {
		c.TreeOpen("World", func() {
			for i, n := range s.tree {
				if i%10 == 0 {
					c.TreeOpen(n, func() {
						for j := range 3 {
							c.Label(s.tree[(i+j+1)%len(s.tree)])
						}
					})
				} else {
					c.Label(n)
				}
			}
		})
	})
}

// BenchmarkUIFrame runs whole frames of the realistic interface, with
// the table at none, a hundred and a thousand rows, against the same
// frame with an empty body as the baseline. Scrolling moves the table a
// line a frame.
func BenchmarkUIFrame(b *testing.B) {
	for _, rows := range []int{0, 100, 1000} {
		name := fmt.Sprintf("table%d", rows)
		b.Run(name, func(b *testing.B) {
			c := benchContext(b)
			in := newFeeder()
			s := newBenchUIState()
			in.Input.FeedMouseMove(600, 300)
			body := func() { benchUIFrame(c, s, rows) }
			for range 3 {
				benchFrame(b, c, in, body)
			}
			b.ReportAllocs()
			b.ResetTimer()
			frame := 0
			for b.Loop() {
				frame++
				if frame%200 < 100 {
					in.Input.FeedScroll(0, -1)
				} else {
					in.Input.FeedScroll(0, 1)
				}
				benchFrame(b, c, in, body)
			}
			b.StopTimer()
			b.ReportMetric(float64(len(c.Accessible())), "nodes")
		})
	}
	b.Run("empty", func(b *testing.B) {
		c := benchContext(b)
		in := newFeeder()
		b.ReportAllocs()
		for b.Loop() {
			benchFrame(b, c, in, func() {})
		}
	})
}

// BenchmarkUIIndexAt is the pointer-to-caret lookup a text field runs
// on every frame of a click or a drag selection, for a field of 40 and
// of 200 characters.
func BenchmarkUIIndexAt(b *testing.B) {
	c := benchContext(b)
	for _, n := range []int{40, 200} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			text := ""
			for len(text) < n {
				text += "lorem ipsum dolor sit amet "
			}
			text = text[:n]
			c.indexAt(text, 100)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				c.indexAt(text, 100)
			}
		})
	}
}

// BenchmarkUIID is the cost of deriving one widget identity.
func BenchmarkUIID(b *testing.B) {
	c := New(nil, Theme{})
	b.ReportAllocs()
	for b.Loop() {
		clear(c.seq)
		_ = c.id("Enabled 12")
	}
}
