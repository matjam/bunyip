package engine

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

type uiClipboard struct{ text string }

func (*uiClipboard) Wake()                         {}
func (a *uiClipboard) Clipboard() (string, error)  { return a.text, nil }
func (a *uiClipboard) SetClipboard(s string) error { a.text = s; return nil }

func TestContextNewUI(t *testing.T) {
	l, win := newTextInputLoop(t)
	c := l.ctx
	c.app = &uiClipboard{}
	c.Width, c.Height = 1280, 720
	c.viewport, c.pixelsPerPoint = lin.Rect{W: 1280, H: 720}, 1
	u, err := c.NewUI(ui.Theme{})
	if err != nil {
		t.Fatal(err)
	}
	if u.Theme.Font == nil || u.Theme != ui.DarkTheme(c.Gfx.DebugFont()) {
		t.Fatal("zero theme did not select the shared dark theme and font")
	}
	other, err := c.NewUI(ui.Theme{})
	if err != nil || other.Theme.Font != u.Theme.Font {
		t.Fatal("UI contexts did not reuse the built-in font")
	}
	if u.Clipboard != c || u.OnTextInputRect == nil {
		t.Fatal("platform callbacks not connected")
	}
	if err := u.Clipboard.SetClipboard("hello"); err != nil {
		t.Fatal(err)
	}
	if text, err := u.Clipboard.Clipboard(); text != "hello" || err != nil {
		t.Fatalf("clipboard: %q, %v", text, err)
	}
	u.OnTextInputRect(10, 20, 30, 40)
	if win.calls != 1 || win.rect != [4]float64{10, 20, 30, 40} {
		t.Fatalf("input rectangle: %v (%d calls)", win.rect, win.calls)
	}
	if ok, err := l.gfx.Begin([4]float32{0, 0, 0, 1}); err != nil || !ok {
		t.Fatalf("begin: %t, %v", ok, err)
	}
	u.Begin(&input.State{}, func() {
		u.Panel("", ui.Rect{X: 10, Y: 10, W: 240, H: 80}, func() { u.Label("Ready") })
	})
	img, err := l.gfx.End(true)
	if err != nil {
		t.Fatal(err)
	}
	lightPixels := 0
	for y := 10; y < 80; y++ {
		for x := 10; x < 250; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 160 && p.G > 160 && p.B > 160 {
				lightPixels++
			}
		}
	}
	if lightPixels < 10 {
		t.Fatalf("default UI text not visible: %d light pixels", lightPixels)
	}

	custom := ui.Theme{Text: gfx.RGB(240, 100, 50), Padding: 3, RowHeight: 21, Skin: &ui.Skin{}}
	want := custom
	want.Font = u.Theme.Font
	got, err := c.NewUI(custom)
	if err != nil || got.Theme != want {
		t.Fatalf("custom theme was changed: %+v, %v", got, err)
	}
	font, err := c.Gfx.NewFont(goregular.TTF, 19, gfx.FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	custom.Font, custom.BoldFont = font, font
	got, err = c.NewUI(custom)
	if err != nil || got.Theme != custom {
		t.Fatalf("custom font/theme was changed: %+v, %v", got, err)
	}
}

func TestContextNewUIRequiresGraphics(t *testing.T) {
	c := &Context{}
	if u, err := c.NewUI(ui.Theme{}); err == nil || u != nil {
		t.Fatalf("without graphics: UI=%v error=%v", u, err)
	}
}
