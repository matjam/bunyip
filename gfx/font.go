package gfx

import (
	"fmt"
	"image"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/matjam/bunyip/lin"
)

// Font is a TrueType/OpenType face rasterised into a glyph atlas at one
// size. Glyphs are rendered at the framebuffer's pixel density at creation
// time and drawn in view units, so text is crisp on high-DPI displays.
type Font struct {
	Size       float32 // in view units
	LineHeight float32
	Ascent     float32
	atlas      *Texture
	glyphs     map[rune]glyph
	face       font.Face
	scale      float32 // atlas pixels per view unit
	packer     shelfPacker
	pix        *image.RGBA
	dirty      bool
	sdf        bool
	g          *Graphics
}

type glyph struct {
	uv0, uv1 lin.Vec2
	size     lin.Vec2 // in view units
	bearing  lin.Vec2 // offset from the pen position to the glyph's top-left, view units
	advance  float32
	empty    bool
}

// FontOptions tunes rasterisation.
type FontOptions struct {
	AtlasSize int       // texture side in pixels; default 1024
	Preload   []rune    // glyphs rendered up front; ASCII is always included
	Ranges    [][2]rune // inclusive ranges rendered up front
}

// NewFont parses TTF/OTF bytes and prepares an atlas for size view units.
func (g *Graphics) NewFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("gfx: parse font: %w", err)
	}
	scale := g.pixelScale()
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size * scale), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("gfx: font face: %w", err)
	}
	side := opts.AtlasSize
	if side <= 0 {
		side = 1024
	}
	metrics := face.Metrics()
	f := &Font{
		Size:       size,
		LineHeight: fixedToFloat(metrics.Height) / scale,
		Ascent:     fixedToFloat(metrics.Ascent) / scale,
		glyphs:     map[rune]glyph{},
		face:       face,
		scale:      scale,
		packer:     shelfPacker{width: side, height: side, pad: 1},
		pix:        image.NewRGBA(image.Rect(0, 0, side, side)),
		g:          g,
	}
	for r := rune(32); r < 127; r++ {
		f.add(r)
	}
	for _, r := range opts.Preload {
		f.add(r)
	}
	for _, rg := range opts.Ranges {
		for r := rg[0]; r <= rg[1]; r++ {
			f.add(r)
		}
	}
	if err := f.flush(); err != nil {
		return nil, err
	}
	return f, nil
}

// pixelScale is framebuffer pixels per view unit along X.
func (g *Graphics) pixelScale() float32 {
	if g.main.viewW <= 0 {
		return 1
	}
	return float32(g.R.Swapchain.Extent.Width) / g.main.viewW
}

func fixedToFloat(v fixed.Int26_6) float32 { return float32(v) / 64 }

// add rasterises one glyph into the CPU atlas. Missing glyphs get the
// face's notdef box, which is what the user wants to see.
func (f *Font) add(r rune) {
	if _, ok := f.glyphs[r]; ok {
		return
	}
	if f.sdf {
		f.addSDF(r)
		return
	}
	bounds, advance, ok := f.face.GlyphBounds(r)
	if !ok {
		bounds, advance, _ = f.face.GlyphBounds(0xFFFD)
	}
	gl := glyph{advance: fixedToFloat(advance) / f.scale}
	w := (bounds.Max.X - bounds.Min.X).Ceil() + 1
	h := (bounds.Max.Y - bounds.Min.Y).Ceil() + 1
	if w <= 1 || h <= 1 {
		gl.empty = true
		f.glyphs[r] = gl
		return
	}
	x, y, ok := f.packer.place(w, h)
	if !ok {
		gl.empty = true // atlas full; drawn as nothing rather than garbage
		f.glyphs[r] = gl
		return
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	dot := fixed.Point26_6{X: -bounds.Min.X, Y: -bounds.Min.Y}
	dr, maskImg, maskPt, _, _ := f.face.Glyph(dot, r)
	if maskImg != nil {
		draw.Draw(mask, dr, maskImg, maskPt, draw.Src)
	}
	for yy := range h {
		for xx := range w {
			a := mask.AlphaAt(xx, yy).A
			f.pix.SetRGBA(x+xx, y+yy, rgbaPremul(a))
		}
	}
	side := float32(f.packer.width)
	gl.uv0 = lin.V2(float32(x)/side, float32(y)/side)
	gl.uv1 = lin.V2(float32(x+w)/side, float32(y+h)/side)
	gl.size = lin.V2(float32(w)/f.scale, float32(h)/f.scale)
	gl.bearing = lin.V2(fixedToFloat(bounds.Min.X)/f.scale, fixedToFloat(bounds.Min.Y)/f.scale)
	f.glyphs[r] = gl
	f.dirty = true
}

// flush uploads the CPU atlas when glyphs were added.
func (f *Font) flush() error {
	if !f.dirty {
		return nil
	}
	if f.atlas != nil {
		f.atlas.Destroy()
	}
	tex, err := f.g.NewTexture(f.pix, TextureOptions{Linear: true, Data: true})
	if err != nil {
		return err
	}
	tex.sdf = f.sdf
	f.atlas = tex
	f.dirty = false
	return nil
}

// Destroy frees the atlas.
func (f *Font) Destroy() {
	if f.atlas != nil {
		f.atlas.Destroy()
		f.atlas = nil
	}
}

// Measure returns the width and height of text as one line.
func (f *Font) Measure(text string) (w, h float32) {
	var prev rune
	for _, r := range text {
		f.add(r)
		if prev != 0 {
			w += fixedToFloat(f.face.Kern(prev, r)) / f.scale
		}
		w += f.glyphs[r].advance
		prev = r
	}
	return w, f.LineHeight
}

// DrawText draws one line with its top-left corner at (x, y).
func (g *Graphics) DrawText(f *Font, text string, x, y float32, c Color) {
	pen := x
	base := y + f.Ascent
	var prev rune
	for _, r := range text {
		f.add(r)
		if prev != 0 {
			pen += fixedToFloat(f.face.Kern(prev, r)) / f.scale
		}
		gl := f.glyphs[r]
		if !gl.empty {
			g.Draw(f.atlas, Sprite{
				Pos:  lin.V2(pen+gl.bearing.X, base+gl.bearing.Y),
				Size: gl.size, UV0: gl.uv0, UV1: gl.uv1, Color: c,
			})
		}
		pen += gl.advance
		prev = r
	}
	if f.dirty {
		// New glyphs were rasterised this frame; the atlas uploads for the
		// next one. Losing one frame of a rare glyph beats stalling.
		_ = f.flush()
	}
}

// shelfPacker places rectangles left to right in rows of similar height.
type shelfPacker struct {
	width, height int
	pad           int
	x, y, rowH    int
}

func (p *shelfPacker) place(w, h int) (x, y int, ok bool) {
	if p.x+w+p.pad > p.width {
		p.x = 0
		p.y += p.rowH + p.pad
		p.rowH = 0
	}
	if p.y+h+p.pad > p.height || w+p.pad > p.width {
		return 0, 0, false
	}
	x, y = p.x, p.y
	p.x += w + p.pad
	p.rowH = max(p.rowH, h)
	return x, y, true
}
