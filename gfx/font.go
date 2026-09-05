package gfx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // bitmap emoji strikes
	_ "image/png"
	"math"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/matjam/bunyip/lin"
)

// Font is an OpenType face (with optional fallbacks) rasterised into a
// glyph atlas at one size. Text is shaped with HarfBuzz, so kerning,
// ligatures, mark placement, Arabic joining and right-to-left order all
// come out right; glyphs are rendered from the font's outlines at the
// framebuffer's pixel density and drawn in view units, so text is crisp
// on high-DPI displays. Create fonts with NewFont or NewSDFFont and call
// Destroy when finished. The atlas has a fixed capacity: glyphs that do
// not fit are omitted, so choose FontOptions.AtlasSize for the character
// set and raster size the game needs.
type Font struct {
	Size       float32 // em size in view units
	LineHeight float32 // baseline to baseline
	Ascent     float32 // baseline to the top of the tallest glyph
	Descent    float32 // baseline to the bottom of the deepest glyph, positive

	faces     []*fontFace // the main face first, then fallbacks
	features  []shaping.FontFeature
	atlas     *Texture
	glyphs    map[glyphKey]glyph
	scale     float32 // atlas pixels per view unit
	pxPerEm   float32 // face pixels per em, the shaping size
	packer    shelfPacker
	pix       *image.RGBA
	dirty     bool
	dirtyRect image.Rectangle // the atlas pixels changed since the last flush
	sdf       bool
	g         *Graphics
	shaper    shaping.HarfbuzzShaper
	seg       shaping.Segmenter
	wrapper   shaping.LineWrapper
	runs      genCache[runKey, []shaping.Output]
	lines     genCache[lineKey, []shaping.Line]
	blocks    genCache[blockKey, []Glyph]
	measures  genCache[measureKey, lin.Vec2]
	scratch   textScratch
	rast      *vector.Rasterizer
}

// fontFace is one parsed face and the metrics it contributes.
type fontFace struct {
	face *font.Face
	upem float32
}

type glyphKey struct {
	face uint8
	gid  font.GID
}

type glyph struct {
	uv0, uv1 lin.Vec2
	size     lin.Vec2 // in view units
	bearing  lin.Vec2 // offset from the glyph origin to its top-left, view units
	empty    bool
	color    bool // a colour bitmap, drawn untinted
}

// FontOptions tunes a font.
type FontOptions struct {
	AtlasSize int       // texture side in pixels; default 1024
	Preload   []rune    // glyphs rendered up front; ASCII is always included
	Ranges    [][2]rune // inclusive ranges rendered up front

	// Fallbacks are further TTF/OTF fonts consulted, in order, for runs of
	// text the main font has no glyphs for: a CJK or Arabic font behind a
	// Latin one, for example.
	Fallbacks [][]byte
	// Features turns OpenType features on ("smcp", "frac", "ss01") or off
	// ("-liga", "-kern") for all text drawn with the font.
	Features []string
	// Variations sets variable font axes, such as "wght": 650 or "wdth": 90.
	Variations map[string]float32
}

// NewFont parses TTF/OTF bytes and prepares an atlas for size view units.
func (g *Graphics) NewFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	scale := g.pixelScale()
	return g.newFont(ttf, size, opts, scale, size*scale, false)
}

// newFont parses the faces and rasterises the preload set. pxPerEm is the
// pixel size glyphs are shaped and rasterised at; scale is atlas pixels
// per view unit.
func (g *Graphics) newFont(ttf []byte, size float32, opts FontOptions, scale, pxPerEm float32, sdf bool) (*Font, error) {
	if size <= 0 {
		return nil, fmt.Errorf("gfx: font size must be positive")
	}
	side := opts.AtlasSize
	if side <= 0 {
		side = 1024
	}
	f := &Font{
		Size:    size,
		glyphs:  map[glyphKey]glyph{},
		scale:   scale,
		pxPerEm: pxPerEm,
		sdf:     sdf,
		packer:  shelfPacker{width: side, height: side, pad: 1},
		pix:     image.NewRGBA(image.Rect(0, 0, side, side)),
		g:       g,
	}
	// A cached block is one glyph per character, so the glyph cache is
	// bounded by the glyphs it holds rather than by how many blocks.
	f.blocks.weigh = func(glyphs []Glyph) int { return len(glyphs) + 1 }
	f.blocks.limit = textBlockGlyphs
	for i, data := range append([][]byte{ttf}, opts.Fallbacks...) {
		face, err := font.ParseTTF(bytes.NewReader(data))
		if err != nil {
			// A collection (.ttc) holds several faces; take the first.
			if faces, errC := font.ParseTTC(bytes.NewReader(data)); errC == nil && len(faces) > 0 {
				face, err = faces[0], nil
			}
		}
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("gfx: parse font: %w", err)
			}
			return nil, fmt.Errorf("gfx: parse fallback font %d: %w", i, err)
		}
		// Bitmap fonts pick a strike by pixel size.
		ppem := uint16(min(max(pxPerEm+0.5, 1), 65535))
		face.SetPpem(ppem, ppem)
		if len(opts.Variations) > 0 {
			var vars []font.Variation
			for tag, v := range opts.Variations {
				if len(tag) == 4 {
					vars = append(vars, font.Variation{Tag: ot.MustNewTag(tag), Value: v})
				}
			}
			face.SetVariations(vars)
		}
		f.faces = append(f.faces, &fontFace{face: face, upem: float32(face.Upem())})
	}
	for _, feat := range opts.Features {
		value := uint32(1)
		tag := feat
		if strings.HasPrefix(tag, "-") {
			value, tag = 0, tag[1:]
		}
		if len(tag) == 4 {
			f.features = append(f.features, shaping.FontFeature{Tag: ot.MustNewTag(tag), Value: value})
		}
	}
	main := f.faces[0]
	k := pxPerEm / main.upem / scale // view units per font unit
	if ext, ok := main.face.FontHExtents(); ok {
		f.Ascent = ext.Ascender * k
		f.Descent = -ext.Descender * k
		f.LineHeight = (ext.Ascender - ext.Descender + ext.LineGap) * k
	} else {
		f.Ascent = size * 0.8
		f.Descent = size * 0.2
		f.LineHeight = size * 1.2
	}
	for r := rune(32); r < 127; r++ {
		f.preload(r)
	}
	for _, r := range opts.Preload {
		f.preload(r)
	}
	for _, rg := range opts.Ranges {
		for r := rg[0]; r <= rg[1]; r++ {
			f.preload(r)
		}
	}
	if err := f.flush(); err != nil {
		return nil, err
	}
	g.track(f, Resource{Kind: ResourceFont, Width: side, Height: side, Bytes: side * side * 4})
	return f, nil
}

// preload rasterises the nominal glyph of a rune from the first face
// that has it.
func (f *Font) preload(r rune) {
	for i, ff := range f.faces {
		if gid, ok := ff.face.NominalGlyph(r); ok {
			f.glyph(uint8(i), gid)
			return
		}
	}
}

// fontmap picks faces for shaping: the main face when it has the rune,
// otherwise the first fallback that does. It implements shaping.Fontmap
// without exposing the shaping library on Font.
type fontmap struct{ f *Font }

func (m fontmap) ResolveFace(r rune) *font.Face {
	f := m.f
	if _, ok := f.faces[0].face.NominalGlyph(r); ok {
		return f.faces[0].face
	}
	for _, ff := range f.faces[1:] {
		if _, ok := ff.face.NominalGlyph(r); ok {
			return ff.face
		}
	}
	return f.faces[0].face
}

// faceIndex finds which of the font's faces a shaped run used.
func (f *Font) faceIndex(face *font.Face) uint8 {
	for i, ff := range f.faces {
		if ff.face == face {
			return uint8(i)
		}
	}
	return 0
}

// pixelScale is framebuffer pixels per view unit along X.
func (g *Graphics) pixelScale() float32 {
	if g.main.viewW <= 0 {
		return 1
	}
	return float32(g.mainExtent().Width) / g.main.viewW
}

func fixedToFloat(v fixed.Int26_6) float32 { return float32(v) / 64 }

// glyph returns a glyph's atlas entry, rasterising it on first use.
func (f *Font) glyph(face uint8, gid font.GID) glyph {
	key := glyphKey{face, gid}
	if gl, ok := f.glyphs[key]; ok {
		return gl
	}
	var gl glyph
	if f.sdf {
		gl = f.addSDF(face, gid)
	} else {
		gl = f.add(face, gid)
	}
	f.glyphs[key] = gl
	return gl
}

// outline flattens a glyph's outline into the rasteriser at k pixels per
// font unit, shifted by (dx, dy) pixels, y down. It reports the outline's
// pixel bounds when the rasteriser is nil.
func (f *Font) outline(face uint8, gid font.GID, k, dx, dy float32, r *vector.Rasterizer) (minX, minY, maxX, maxY float32, ok bool) {
	b, ok := f.walkOutline(face, gid, affine{a: k, d: -k, tx: dx, ty: dy}, r)
	return b.minX, b.minY, b.maxX, b.maxY, ok
}

// walkOutline flattens a glyph's outline through a transform into the
// rasteriser, or measures it alone when the rasteriser is nil. The
// transform maps font units, y up, to the pixels of the glyph being
// rendered, y down. The bounds are of the control points, so a curve is
// held loosely.
func (f *Font) walkOutline(face uint8, gid font.GID, m affine, r *vector.Rasterizer) (box, bool) {
	// The library takes a glyph index in its table form here.
	ol, has := f.faces[face].face.GlyphDataOutline(uint16(gid))
	if !has || len(ol.Segments) == 0 {
		return box{}, false
	}
	b := emptyBox
	pt := func(p ot.SegmentPoint) (float32, float32) {
		x, y := m.apply(p.X, p.Y)
		b.add(x, y)
		return x, y
	}
	for _, s := range ol.Segments {
		switch s.Op {
		case ot.SegmentOpMoveTo:
			x, y := pt(s.Args[0])
			if r != nil {
				r.MoveTo(x, y)
			}
		case ot.SegmentOpLineTo:
			x, y := pt(s.Args[0])
			if r != nil {
				r.LineTo(x, y)
			}
		case ot.SegmentOpQuadTo:
			cx, cy := pt(s.Args[0])
			x, y := pt(s.Args[1])
			if r != nil {
				r.QuadTo(cx, cy, x, y)
			}
		case ot.SegmentOpCubeTo:
			c1x, c1y := pt(s.Args[0])
			c2x, c2y := pt(s.Args[1])
			x, y := pt(s.Args[2])
			if r != nil {
				r.CubeTo(c1x, c1y, c2x, c2y, x, y)
			}
		}
	}
	if r != nil {
		r.ClosePath()
	}
	return b, true
}

// rasterise renders a glyph's coverage at k pixels per font unit,
// returning the mask and the pixel offset of its top-left from the
// glyph origin (on the baseline).
func (f *Font) rasterise(face uint8, gid font.GID, k float32, pad int) (mask *image.Alpha, ox, oy int, ok bool) {
	minX, minY, maxX, maxY, has := f.outline(face, gid, k, 0, 0, nil)
	if !has || maxX <= minX || maxY <= minY {
		return nil, 0, 0, false
	}
	ox = int(math.Floor(float64(minX))) - pad
	oy = int(math.Floor(float64(minY))) - pad
	w := int(math.Ceil(float64(maxX))) - ox + pad + 1
	h := int(math.Ceil(float64(maxY))) - oy + pad + 1
	if f.rast == nil || f.rast.Bounds().Dx() < w || f.rast.Bounds().Dy() < h {
		f.rast = vector.NewRasterizer(max(w, 64), max(h, 64))
	}
	f.rast.Reset(w, h)
	f.outline(face, gid, k, float32(-ox), float32(-oy), f.rast)
	mask = image.NewAlpha(image.Rect(0, 0, w, h))
	f.rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask, ox, oy, true
}

// add rasterises one glyph into the CPU atlas at the font's size. A
// glyph the font describes in colour, as COLR layers, an SVG document or
// a bitmap strike, is drawn in its own colours; anything else, and
// anything in those forms this cannot draw, falls back to the outline.
func (f *Font) add(face uint8, gid font.GID) glyph {
	switch data := f.faces[face].face.GlyphData(gid).(type) {
	case font.GlyphColor:
		if gl, ok := f.addCOLR(face, gid, data); ok {
			return gl
		}
	case font.GlyphSVG:
		if gl, ok := f.addSVG(face, gid, data); ok {
			return gl
		}
	case font.GlyphBitmap:
		if data.Format == font.PNG || data.Format == font.JPG {
			return f.addBitmap(face, gid, data)
		}
	}
	k := f.pxPerEm / f.faces[face].upem
	mask, ox, oy, ok := f.rasterise(face, gid, k, 0)
	if !ok {
		return glyph{empty: true}
	}
	w, h := mask.Rect.Dx(), mask.Rect.Dy()
	x, y, placed := f.packer.place(w, h)
	if !placed {
		return glyph{empty: true} // atlas full; drawn as nothing rather than garbage
	}
	for yy := range h {
		for xx := range w {
			f.pix.SetRGBA(x+xx, y+yy, rgbaPremul(mask.Pix[yy*mask.Stride+xx]))
		}
	}
	side := float32(f.packer.width)
	f.touched(x, y, w, h)
	return glyph{
		uv0:     lin.V2(float32(x)/side, float32(y)/side),
		uv1:     lin.V2(float32(x+w)/side, float32(y+h)/side),
		size:    lin.V2(float32(w)/f.scale, float32(h)/f.scale),
		bearing: lin.V2(float32(ox)/f.scale, float32(oy)/f.scale),
	}
}

// addBitmap puts a colour bitmap glyph (an emoji from an sbix or CBDT
// strike) into the atlas, scaled to the font's size and sat on the em
// box; it draws untinted.
func (f *Font) addBitmap(face uint8, gid font.GID, bm font.GlyphBitmap) glyph {
	src, _, err := image.Decode(bytes.NewReader(bm.Data))
	if err != nil {
		return glyph{empty: true}
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return glyph{empty: true}
	}
	// Strikes are drawn at about one em high; scale to our pixel size.
	k := f.pxPerEm / float32(b.Dy())
	w := max(1, int(float32(b.Dx())*k+0.5))
	h := max(1, int(float32(b.Dy())*k+0.5))
	x, y, placed := f.packer.place(w, h)
	if !placed {
		return glyph{empty: true}
	}
	dst := f.pix.SubImage(image.Rect(x, y, x+w, y+h)).(*image.RGBA)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	// The atlas is sampled as data, so store linear light: undo the
	// premultiplication, decode sRGB, premultiply again.
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			i := f.pix.PixOffset(xx, yy)
			p := f.pix.Pix[i : i+4 : i+4]
			a := float32(p[3]) / 255
			if a == 0 {
				continue
			}
			for k := range 3 {
				straight := float32(p[k]) / 255 / a
				p[k] = uint8(lin.Clamp(srgbToLinear(uint8(lin.Clamp(straight, 0, 1)*255+0.5))*a, 0, 1)*255 + 0.5)
			}
		}
	}
	// Sit the image in the line box, centred on the em's vertical middle.
	box := (f.Ascent + f.Descent) * f.scale
	oy := -f.Ascent*f.scale + (box-float32(h))/2
	side := float32(f.packer.width)
	f.touched(x, y, w, h)
	return glyph{
		uv0:     lin.V2(float32(x)/side, float32(y)/side),
		uv1:     lin.V2(float32(x+w)/side, float32(y+h)/side),
		size:    lin.V2(float32(w)/f.scale, float32(h)/f.scale),
		bearing: lin.V2(0, oy/f.scale),
		color:   true,
	}
}

// flush uploads the CPU atlas when glyphs were added.
// touched marks atlas pixels as changed since the last flush.
func (f *Font) touched(x, y, w, h int) {
	f.dirty = true
	f.dirtyRect = f.dirtyRect.Union(image.Rect(x, y, x+w, y+h))
}

// flush sends the glyphs rasterised since the last flush to the atlas.
// The atlas texture is made once and then written in place, inside the
// frame, so a glyph first drawn this frame appears this frame and the
// texture is never replaced or retired.
func (f *Font) flush() error {
	if !f.dirty {
		return nil
	}
	if f.atlas == nil {
		// No mip chain: a padded atlas sampled at lower mips bleeds
		// neighbouring glyphs into each other.
		tex, err := f.g.NewTexture(f.pix, TextureOptions{Linear: true, Data: true, NoMipmaps: true})
		if err != nil {
			return err
		}
		tex.sdf = f.sdf
		f.atlas = tex
		// The font's own entry in the resource list covers the atlas, so
		// the atlas is not listed as a texture of its own as well.
		f.g.forget(tex)
	} else if r := f.dirtyRect.Intersect(f.pix.Bounds()); !r.Empty() {
		if err := f.atlas.Write(r.Min.X, r.Min.Y, f.pix.SubImage(r)); err != nil {
			return err
		}
	}
	f.dirty = false
	f.dirtyRect = image.Rectangle{}
	return nil
}

// Texture returns the glyph atlas, for drawing glyphs from Shape by
// hand. The atlas is written in place as glyphs are added, so the
// texture stays the same for the life of the font.
func (f *Font) Texture() *Texture { return f.atlas }

// Destroy frees the atlas.
func (f *Font) Destroy() {
	f.g.forget(f)
	if f.atlas != nil {
		f.atlas.Destroy()
		f.atlas = nil
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
