package ui

import (
	"log/slog"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

func newContext(t *testing.T) *Context {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "ui_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 320, Height: 240}, true)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gfx.New(r)
	if err != nil {
		t.Fatal(err)
	}
	font, err := g.NewFont(goregular.TTF, 14, gfx.FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { font.Destroy(); g.Destroy(); r.Destroy() })
	return New(g, DarkTheme(font))
}

// frame runs one interface frame the way the engine does: an Update
// consumes the input edges first, then Draw builds a panel with a button,
// a checkbox and a text field, returning what the button reported.
func frame(t *testing.T, c *Context, in *input.State, checked *bool, text *string) bool {
	t.Helper()
	in.EndUpdate() // the game's Update ran and its edges were cleared
	in.SetDrawing(true)
	defer func() {
		in.SetDrawing(false)
		in.EndFrame()
	}()
	ok, err := c.g.Begin(gfx.Black)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return false
	}
	var clicked bool
	c.Begin(in, func() {
		c.Panel("Test", Rect{X: 10, Y: 10, W: 200, H: 200}, func() {
			clicked = c.Button("Press")
			c.Checkbox("Tick", checked)
			c.TextField("name", text)
		})
	})
	if _, err := c.g.End(false); err != nil {
		t.Fatal(err)
	}
	return clicked
}

func TestWidgets(t *testing.T) {
	c := newContext(t)
	in := &input.State{}
	var checked bool
	var text string
	// Layout: panel padding 8, title line, then rows of 28 with 6 spacing.
	_, titleH := c.Theme.Font.Measure("Test", gfx.TextOptions{})
	buttonY := 10 + 8 + titleH + 6 + 14
	checkboxY := buttonY + 28 + 6
	fieldY := checkboxY + 28 + 6

	frame(t, c, in, &checked, &text) // settle hot state
	in.FeedMouseMove(60, float32(buttonY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseButton(input.MouseLeft, true, 60, float32(buttonY))
	if frame(t, c, in, &checked, &text) {
		t.Error("button reported a click on press; it should wait for release")
	}
	in.FeedMouseButton(input.MouseLeft, false, 60, float32(buttonY))
	if !frame(t, c, in, &checked, &text) {
		t.Error("button did not report the click on release")
	}
	// Press on the button, drag off, release: no click.
	in.FeedMouseButton(input.MouseLeft, true, 60, float32(buttonY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseMove(300, 300)
	in.FeedMouseButton(input.MouseLeft, false, 300, 300)
	if frame(t, c, in, &checked, &text) {
		t.Error("click reported after releasing outside the button")
	}
	// Checkbox toggles.
	in.FeedMouseMove(20, float32(checkboxY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseButton(input.MouseLeft, true, 20, float32(checkboxY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseButton(input.MouseLeft, false, 20, float32(checkboxY))
	frame(t, c, in, &checked, &text)
	if !checked {
		t.Error("checkbox did not toggle")
	}
	// Text field takes focus and text.
	in.FeedMouseMove(60, float32(fieldY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseButton(input.MouseLeft, true, 60, float32(fieldY))
	frame(t, c, in, &checked, &text)
	in.FeedMouseButton(input.MouseLeft, false, 60, float32(fieldY))
	frame(t, c, in, &checked, &text)
	if !c.WantsKeyboard() {
		t.Fatal("text field did not take focus")
	}
	for _, r := range "hi!" {
		in.FeedChar(r)
	}
	frame(t, c, in, &checked, &text)
	in.FeedKey(input.KeyBackspace, true, false, 0)
	frame(t, c, in, &checked, &text)
	if text != "hi" {
		t.Errorf("text = %q, want %q", text, "hi")
	}
	in.FeedKey(input.KeyEnter, true, false, 0)
	frame(t, c, in, &checked, &text)
	if c.WantsKeyboard() {
		t.Error("enter did not release focus")
	}
	if !c.WantsMouse() {
		t.Error("pointer over the panel should be claimed")
	}
}
