package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/lin"
)

// frame2D draws with the queue in screen space and returns the image.
func frame2D(t *testing.T, g *Graphics, draw func()) *image.RGBA {
	t.Helper()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	if ok, err := g.Begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	draw()
	img, err := g.End(true)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestColorMatrix(t *testing.T) {
	if c := Invert().Apply(Color{1, 0, 0, 1}); c.R > 0.01 || c.G < 0.99 {
		t.Errorf("Invert = %v", c)
	}
	if c := Grayscale().Apply(Color{1, 0, 0, 1}); math.Abs(float64(c.R-c.G)) > 1e-5 || c.R < 0.2 || c.R > 0.22 {
		t.Errorf("Grayscale = %v, want the red luminance in every channel", c)
	}
	// The luminance-preserving rotation turns red into a darker green.
	if c := HueRotate(math.Pi * 2 / 3).Apply(Color{1, 0, 0, 1}); c.G < 0.3 || c.R > 0.05 || c.B > 0.05 {
		t.Errorf("HueRotate 120 degrees of red = %v, want green", c)
	}
	if c := Contrast(1).Mul(Brightness(0.5)).Apply(Color{1, 1, 1, 1}); math.Abs(float64(c.R-0.5)) > 1e-5 {
		t.Errorf("Brightness 0.5 = %v", c)
	}
	g := newHeadless(t, 32, 32)
	img := frame2D(t, g, func() {
		g.ColorMatrixed(Invert(), func() { g.FillRect(0, 0, 16, 32, RGB(255, 0, 0)) })
		g.FillRect(16, 0, 16, 32, RGB(255, 0, 0))
	})
	if c := img.RGBAAt(8, 16); c.R > 60 || c.G < 180 {
		t.Errorf("inverted red is %v, want cyan", c)
	}
	if c := img.RGBAAt(24, 16); c.R < 180 || c.G > 60 {
		t.Errorf("plain red after the matrix is %v", c)
	}
}

func TestFlipsFilterAndIndexed(t *testing.T) {
	g := newHeadless(t, 32, 32)
	two := image.NewRGBA(image.Rect(0, 0, 2, 1))
	two.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	two.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	tex, err := g.NewTexture(two, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	img := frame2D(t, g, func() {
		g.Draw(tex, Sprite{Pos: lin.V2(0, 0), Size: lin.V2(16, 8)})
		g.Draw(tex, Sprite{Pos: lin.V2(0, 8), Size: lin.V2(16, 8), FlipX: true})
		g.Draw(tex, Sprite{Pos: lin.V2(0, 16), Size: lin.V2(16, 8), Filter: FilterLinear})
		g.DrawIndexed(tex, []Vertex2D{{Pos: lin.V2(16, 0)}, {Pos: lin.V2(32, 0), UV: lin.V2(1, 0)}, {Pos: lin.V2(32, 32), UV: lin.V2(1, 1)}, {Pos: lin.V2(16, 32), UV: lin.V2(0, 1)}}, []uint32{0, 1, 2, 0, 2, 3})
	})
	if c := img.RGBAAt(2, 4); c.R < 180 {
		t.Errorf("unflipped left is %v, want red", c)
	}
	if c := img.RGBAAt(2, 12); c.B < 180 {
		t.Errorf("flipped left is %v, want blue", c)
	}
	mid := img.RGBAAt(8, 20)
	if mid.R < 40 || mid.B < 40 {
		t.Errorf("linear filtering at the seam is %v, want a blend", mid)
	}
	if c := img.RGBAAt(18, 16); c.R < 180 {
		t.Errorf("indexed quad left is %v, want red", c)
	}
}

func TestGradientDashAndTiledSlice(t *testing.T) {
	g := newHeadless(t, 64, 64)
	grad, err := g.NewGradient(GradientStop{0, RGB(255, 0, 0)}, GradientStop{1, RGB(0, 0, 255)})
	if err != nil {
		t.Fatal(err)
	}
	defer grad.Destroy()
	img := frame2D(t, g, func() {
		g.FillGradient(lin.R(0, 0, 64, 16), grad.Linear(lin.V2(0, 0), lin.V2(64, 0)))
		var p Path
		p.MoveTo(0, 32).LineTo(64, 32)
		g.StrokePath(&p, White, StrokeOptions{Width: 4, Dash: []float32{8, 8}, NoAntiAlias: true})
	})
	if c := img.RGBAAt(2, 8); c.R < 180 || c.B > 60 {
		t.Errorf("gradient start is %v, want red", c)
	}
	if c := img.RGBAAt(61, 8); c.B < 180 || c.R > 60 {
		t.Errorf("gradient end is %v, want blue", c)
	}
	if !bright(img, 4, 32) || bright(img, 12, 32) || !bright(img, 20, 32) {
		t.Error("dashes are not 8 on, 8 off")
	}
	// A tiled nine-slice repeats a 2x2 checker in its centre.
	check := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{0, 0, 0, 255}
			if x >= 1 && x < 3 && y >= 1 && y < 3 && (x+y)%2 == 0 {
				c = color.RGBA{255, 255, 255, 255}
			}
			check.SetRGBA(x, y, c)
		}
	}
	tex, err := g.NewTexture(check, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	img = frame2D(t, g, func() {
		g.DrawNineSlice(NineSlice{Tex: tex, Left: 1, Top: 1, Right: 1, Bottom: 1, Tile: true}, lin.R(0, 0, 64, 64), White)
	})
	lit := 0
	for y := 1; y < 63; y++ {
		for x := 1; x < 63; x++ {
			if bright(img, x, y) {
				lit++
			}
		}
	}
	if lit < 62*62*3/10 || lit > 62*62*7/10 {
		t.Errorf("tiled centre lit %d of %d pixels, want about half", lit, 62*62)
	}
}

func TestTextOnPathAndLit(t *testing.T) {
	g := newHeadless(t, 96, 96)
	f, err := g.NewFont(goregular.TTF, 12, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// A vertical path: text along it lands in a column, not a row.
	img := frame2D(t, g, func() {
		var p Path
		p.MoveTo(48, 4).LineTo(48, 92)
		g.DrawTextOnPath(f, "HELLO", &p, 4, TextOptions{}, White)
	})
	col, row := 0, 0
	for y := 4; y < 92; y++ {
		if bright(img, 48, y) || bright(img, 44, y) || bright(img, 52, y) {
			col++
		}
	}
	for x := 4; x < 92; x++ {
		if bright(img, x, 8) {
			row++
		}
	}
	if col < 15 || row > 12 {
		t.Errorf("text on a vertical path lit %d column pixels and %d row pixels", col, row)
	}
	// A lit sprite: a flat normal map under a light on the left is
	// brighter on its left than its right.
	flat := image.NewRGBA(image.Rect(0, 0, 1, 1))
	flat.SetRGBA(0, 0, color.RGBA{128, 128, 255, 255})
	normal, err := g.NewTexture(flat, TextureOptions{Data: true})
	if err != nil {
		t.Fatal(err)
	}
	defer normal.Destroy()
	img = frame2D(t, g, func() {
		g.SetLights2D(Color{0.05, 0.05, 0.05, 1}, Light2D{Pos: lin.V2(0, 48), Height: 20, Radius: 80})
		g.DrawLit(nil, normal, Sprite{Pos: lin.V2(0, 0), Size: lin.V2(96, 96)})
	})
	left, right := img.RGBAAt(8, 48), img.RGBAAt(88, 48)
	if left.R < right.R+40 {
		t.Errorf("lit sprite left %v right %v, want brighter on the light's side", left, right)
	}
}

func TestStreamingWrite(t *testing.T) {
	g := newHeadless(t, 16, 16)
	tex, err := g.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	red := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range red.Pix {
		red.Pix[i] = 255
		if i%4 == 1 || i%4 == 2 {
			red.Pix[i] = 0
		}
	}
	// Written inside the frame, the texture draws with its new pixels the
	// same frame, with no device wait.
	img := frame2D(t, g, func() {
		if err := tex.Write(0, 0, red); err != nil {
			t.Fatal(err)
		}
		g.Draw(tex, Sprite{Size: lin.V2(16, 16)})
	})
	if c := img.RGBAAt(8, 8); c.R < 180 || c.G > 40 {
		t.Errorf("streamed write drew %v, want red", c)
	}
	if g.Stats().Draws2D != 1 {
		t.Errorf("stats report %d 2D draws, want 1", g.Stats().Draws2D)
	}
}
