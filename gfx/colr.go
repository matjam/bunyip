package gfx

import (
	"image"
	"math"
	"slices"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/vector"

	"github.com/matjam/bunyip/lin"
)

// COLR glyphs are drawn from the font's paint graph: version 0 layers,
// which are outlines filled with palette colours, and version 1 paints,
// which add gradients, transforms and compositing. The result goes in
// the atlas as one colour glyph, so a layered emoji costs the same to
// draw as a letter.

const (
	colrMaxDepth  = 64 // paint tables deep, against a font that loops
	colrMaxLayers = 4096
)

// colrPainter draws one glyph's paint graph onto a canvas.
type colrPainter struct {
	f     *Font
	face  uint8
	colr  *tables.COLR1
	pal   []tables.ColorRecord
	k     float32 // pixels per font unit
	toPix affine  // font units, y up, to canvas pixels, y down
	w, h  int
	rast  *vector.Rasterizer
	alpha *image.Alpha
	seen  []tables.GlyphID // base glyphs being painted, against a loop
}

// addCOLR draws a glyph the font describes as COLR layers and puts it in
// the atlas. It reports false when the font has no palette or the paint
// graph covers nothing, so the caller can fall back to the outline.
func (f *Font) addCOLR(face uint8, gid font.GID, data font.GlyphColor) (glyph, bool) {
	ff := f.faces[face]
	colr, cpal := ff.face.COLR, ff.face.CPAL
	if colr == nil || len(cpal) == 0 || len(cpal[0]) == 0 || data.Paint == nil {
		return glyph{}, false
	}
	p := &colrPainter{f: f, face: face, colr: colr, pal: cpal[0], k: f.pxPerEm / ff.upem}
	// A clip box, when the font gives one, is the glyph's extent; without
	// one the extent is what the layers cover.
	b, clipped := clipBox(colr.ClipList, uint16(gid))
	if !clipped {
		b = p.bounds(data.Paint, identityAffine, 0)
	}
	if !b.valid() {
		return glyph{}, false
	}
	ox := int(math.Floor(float64(b.minX * p.k)))
	oy := int(math.Floor(float64(-b.maxY * p.k)))
	p.w = int(math.Ceil(float64(b.maxX*p.k))) - ox + 1
	p.h = int(math.Ceil(float64(-b.minY*p.k))) - oy + 1
	if p.w <= 0 || p.h <= 0 || p.w > f.packer.width || p.h > f.packer.height {
		return glyph{}, false
	}
	p.toPix = affine{a: p.k, d: -p.k, tx: float32(-ox), ty: float32(-oy)}
	p.rast = vector.NewRasterizer(p.w, p.h)
	p.alpha = image.NewAlpha(image.Rect(0, 0, p.w, p.h))
	canvas := newColorCanvas(p.w, p.h)
	var root []float32
	if clipped {
		// A gradient painted straight into the clip box needs a mask to
		// fill; layers make their own from their outlines.
		root = make([]float32, p.w*p.h)
		for i := range root {
			root[i] = 1
		}
	}
	p.paint(canvas, root, identityAffine, data.Paint)
	return f.addCanvas(canvas, ox, oy)
}

// clipBox returns a glyph's clip box from the COLR table, in font units.
func clipBox(list tables.ClipList, gid tables.GlyphID) (box, bool) {
	cb, ok := list.Search(gid)
	if !ok {
		return box{}, false
	}
	switch c := cb.(type) {
	case tables.ClipBoxFormat1:
		return box{float32(c.XMin), float32(c.YMin), float32(c.XMax), float32(c.YMax)}, true
	case tables.ClipBoxFormat2:
		return box{float32(c.XMin), float32(c.YMin), float32(c.XMax), float32(c.YMax)}, true
	}
	return box{}, false
}

// f214 reads a 2.14 fixed point number.
func f214(v tables.Fixed214) float32 { return float32(v) / (1 << 14) }

// f214Angle reads an angle stored as 2.14 fixed point, where 1.0 is a
// half turn counter-clockwise.
func f214Angle(v tables.Fixed214) float32 { return f214(v) * math.Pi }

// paletteColor returns a palette entry in linear light, scaled by an
// alpha. The index for the text colour draws white, because a colour
// glyph is drawn untinted.
func (p *colrPainter) paletteColor(index uint16, alpha float32) rgba {
	if index == 0xFFFF {
		return rgba{1, 1, 1, alpha}
	}
	if int(index) >= len(p.pal) {
		return rgba{}
	}
	c := p.pal[index]
	return rgba{
		r: srgbToLinear(c.Red), g: srgbToLinear(c.Green), b: srgbToLinear(c.Blue),
		a: float32(c.Alpha) / 255 * alpha,
	}
}

// bounds returns the font-unit box the paint graph covers, which is the
// box of the outlines its glyph paints fill.
func (p *colrPainter) bounds(pt tables.PaintTable, m affine, depth int) box {
	if depth > colrMaxDepth {
		return emptyBox
	}
	b := emptyBox
	switch t := pt.(type) {
	case tables.PaintColrLayersResolved:
		for _, layer := range t {
			if lb, ok := p.f.walkOutline(p.face, font.GID(layer.GlyphID), m, nil); ok {
				b = b.union(lb)
			}
		}
	case tables.PaintColrLayers:
		layers, err := p.colr.LayerList.Resolve(t)
		if err != nil {
			return b
		}
		for _, l := range layers {
			b = b.union(p.bounds(l, m, depth+1))
		}
	case tables.PaintGlyph:
		if lb, ok := p.f.walkOutline(p.face, font.GID(t.GlyphID), m, nil); ok {
			b = b.union(lb)
		}
	case tables.PaintColrGlyph:
		if sub, ok := p.colr.Search(t.GlyphID); ok && !slices.Contains(p.seen, t.GlyphID) {
			p.seen = append(p.seen, t.GlyphID)
			b = b.union(p.bounds(sub, m, depth+1))
			p.seen = p.seen[:len(p.seen)-1]
		}
	case tables.PaintComposite:
		b = p.bounds(t.SourcePaint, m, depth+1).union(p.bounds(t.BackdropPaint, m, depth+1))
	default:
		if child, xf, ok := p.child(pt); ok {
			b = p.bounds(child, m.mul(xf), depth+1)
		}
	}
	return b
}

// child returns the paint a transform wraps and the transform it
// applies, for the paint tables that only move their child.
func (p *colrPainter) child(pt tables.PaintTable) (tables.PaintTable, affine, bool) {
	switch t := pt.(type) {
	case tables.PaintTransform:
		return t.Paint, fromAffine2x3(t.Transform.Xx, t.Transform.Yx, t.Transform.Xy, t.Transform.Yy, t.Transform.Dx, t.Transform.Dy), true
	case tables.PaintVarTransform:
		return t.Paint, fromAffine2x3(t.Transform.Xx, t.Transform.Yx, t.Transform.Xy, t.Transform.Yy, t.Transform.Dx, t.Transform.Dy), true
	case tables.PaintTranslate:
		return t.Paint, translateAffine(float32(t.Dx), float32(t.Dy)), true
	case tables.PaintVarTranslate:
		return t.Paint, translateAffine(float32(t.Dx), float32(t.Dy)), true
	case tables.PaintScale:
		return t.Paint, scaleAffine(f214(t.ScaleX), f214(t.ScaleY)), true
	case tables.PaintVarScale:
		return t.Paint, scaleAffine(f214(t.ScaleX), f214(t.ScaleY)), true
	case tables.PaintScaleAroundCenter:
		return t.Paint, around(scaleAffine(f214(t.ScaleX), f214(t.ScaleY)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintVarScaleAroundCenter:
		return t.Paint, around(scaleAffine(f214(t.ScaleX), f214(t.ScaleY)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintScaleUniform:
		return t.Paint, scaleAffine(f214(t.Scale), f214(t.Scale)), true
	case tables.PaintVarScaleUniform:
		return t.Paint, scaleAffine(f214(t.Scale), f214(t.Scale)), true
	case tables.PaintScaleUniformAroundCenter:
		return t.Paint, around(scaleAffine(f214(t.Scale), f214(t.Scale)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintVarScaleUniformAroundCenter:
		return t.Paint, around(scaleAffine(f214(t.Scale), f214(t.Scale)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintRotate:
		return t.Paint, rotateAffine(f214Angle(t.Angle)), true
	case tables.PaintVarRotate:
		return t.Paint, rotateAffine(f214Angle(t.Angle)), true
	case tables.PaintRotateAroundCenter:
		return t.Paint, around(rotateAffine(f214Angle(t.Angle)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintVarRotateAroundCenter:
		return t.Paint, around(rotateAffine(f214Angle(t.Angle)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintSkew:
		return t.Paint, skewAffine(-f214Angle(t.XSkewAngle), f214Angle(t.YSkewAngle)), true
	case tables.PaintVarSkew:
		return t.Paint, skewAffine(-f214Angle(t.XSkewAngle), f214Angle(t.YSkewAngle)), true
	case tables.PaintSkewAroundCenter:
		return t.Paint, around(skewAffine(-f214Angle(t.XSkewAngle), f214Angle(t.YSkewAngle)), float32(t.CenterX), float32(t.CenterY)), true
	case tables.PaintVarSkewAroundCenter:
		return t.Paint, around(skewAffine(-f214Angle(t.XSkewAngle), f214Angle(t.YSkewAngle)), float32(t.CenterX), float32(t.CenterY)), true
	}
	return nil, identityAffine, false
}

// fromAffine2x3 reads an OpenType Affine2x3.
func fromAffine2x3(xx, yx, xy, yy, dx, dy float32) affine {
	return affine{a: xx, b: yx, c: xy, d: yy, tx: dx, ty: dy}
}

// paint draws one paint table onto the canvas, clipped to a coverage
// mask. m is the transform from the paint's own space to font units.
func (p *colrPainter) paint(dst *colorCanvas, clip []float32, m affine, pt tables.PaintTable) {
	p.paintAt(dst, clip, m, pt, 0)
}

func (p *colrPainter) paintAt(dst *colorCanvas, clip []float32, m affine, pt tables.PaintTable, depth int) {
	if depth > colrMaxDepth {
		return
	}
	switch t := pt.(type) {
	case tables.PaintColrLayersResolved: // version 0: outlines and palette colours
		for i, layer := range t {
			if i >= colrMaxLayers {
				break
			}
			if mask := p.mask(layer.GlyphID, m, clip); mask != nil {
				dst.fill(mask, solid(p.paletteColor(layer.PaletteIndex, 1)))
			}
		}
	case tables.PaintColrLayers:
		layers, err := p.colr.LayerList.Resolve(t)
		if err != nil {
			return
		}
		for _, l := range layers {
			p.paintAt(dst, clip, m, l, depth+1)
		}
	case tables.PaintGlyph:
		if mask := p.mask(t.GlyphID, m, clip); mask != nil {
			p.paintAt(dst, mask, m, t.Paint, depth+1)
		}
	case tables.PaintColrGlyph:
		if sub, ok := p.colr.Search(t.GlyphID); ok && !slices.Contains(p.seen, t.GlyphID) {
			p.seen = append(p.seen, t.GlyphID)
			p.paintAt(dst, clip, m, sub, depth+1)
			p.seen = p.seen[:len(p.seen)-1]
		}
	case tables.PaintSolid:
		if clip != nil {
			dst.fill(clip, solid(p.paletteColor(t.PaletteIndex, f214(t.Alpha))))
		}
	case tables.PaintVarSolid:
		if clip != nil {
			dst.fill(clip, solid(p.paletteColor(t.PaletteIndex, f214(t.Alpha))))
		}
	case tables.PaintLinearGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			x0, y0, x1, y1 := linearEnds(float32(t.X0), float32(t.Y0), float32(t.X1), float32(t.Y1), float32(t.X2), float32(t.Y2))
			return linearGradient(line, x0, y0, x1, y1, inv)
		}, p.colorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintVarLinearGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			x0, y0, x1, y1 := linearEnds(float32(t.X0), float32(t.Y0), float32(t.X1), float32(t.Y1), float32(t.X2), float32(t.Y2))
			return linearGradient(line, x0, y0, x1, y1, inv)
		}, p.varColorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintRadialGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			return radialGradient(line, float32(t.X0), float32(t.Y0), float32(t.Radius0), float32(t.X1), float32(t.Y1), float32(t.Radius1), inv)
		}, p.colorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintVarRadialGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			return radialGradient(line, float32(t.X0), float32(t.Y0), float32(t.Radius0), float32(t.X1), float32(t.Y1), float32(t.Radius1), inv)
		}, p.varColorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintSweepGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			return sweepGradient(line, float32(t.CenterX), float32(t.CenterY), f214Angle(t.StartAngle), f214Angle(t.EndAngle), inv)
		}, p.colorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintVarSweepGradient:
		p.gradient(dst, clip, m, func(line colorLine, inv affine) func(x, y float32) rgba {
			return sweepGradient(line, float32(t.CenterX), float32(t.CenterY), f214Angle(t.StartAngle), f214Angle(t.EndAngle), inv)
		}, p.varColorLine(t.ColorLine.ColorStops, t.ColorLine.Extend))
	case tables.PaintComposite:
		// The two sides are drawn on their own so that the mode sees only
		// them, then the result goes onto the canvas source over.
		backdrop := newColorCanvas(p.w, p.h)
		source := newColorCanvas(p.w, p.h)
		p.paintAt(backdrop, clip, m, t.BackdropPaint, depth+1)
		p.paintAt(source, clip, m, t.SourcePaint, depth+1)
		backdrop.composite(source, t.CompositeMode)
		dst.over(backdrop)
	default:
		if child, xf, ok := p.child(pt); ok {
			p.paintAt(dst, clip, m.mul(xf), child, depth+1)
		}
	}
}

// gradient fills the clip with a gradient whose geometry is in the
// paint's own space.
func (p *colrPainter) gradient(dst *colorCanvas, clip []float32, m affine, make func(colorLine, affine) func(x, y float32) rgba, line colorLine) {
	if clip == nil || len(line.stops) == 0 {
		return
	}
	inv, ok := p.toPix.mul(m).invert()
	if !ok {
		return
	}
	dst.fill(clip, make(line, inv))
}

// linearEnds turns a COLR linear gradient's three points into the two
// ends of a plain gradient: the end point projected onto the line
// through the start perpendicular to the rotation point.
func linearEnds(x0, y0, x1, y1, x2, y2 float32) (float32, float32, float32, float32) {
	// The perpendicular of p0p2 is the direction the colour line runs in.
	px, py := -(y2 - y0), x2-x0
	den := px*px + py*py
	if den == 0 {
		return x0, y0, x1, y1
	}
	t := ((x1-x0)*px + (y1-y0)*py) / den
	return x0, y0, x0 + px*t, y0 + py*t
}

// colorLine reads a COLR colour line.
func (p *colrPainter) colorLine(stops []tables.ColorStop, extend tables.Extend) colorLine {
	out := colorLine{extend: extendMode(extend), stops: make([]colorStop, 0, len(stops))}
	for _, s := range stops {
		out.stops = append(out.stops, colorStop{offset: f214(s.StopOffset), color: p.paletteColor(s.PaletteIndex, f214(s.Alpha))})
	}
	slices.SortStableFunc(out.stops, func(a, b colorStop) int {
		switch {
		case a.offset < b.offset:
			return -1
		case a.offset > b.offset:
			return 1
		}
		return 0
	})
	return out
}

// varColorLine reads a colour line that carries variation indices, whose
// deltas are not applied.
func (p *colrPainter) varColorLine(stops []tables.VarColorStop, extend tables.Extend) colorLine {
	plain := make([]tables.ColorStop, len(stops))
	for i, s := range stops {
		plain[i] = tables.ColorStop{StopOffset: s.StopOffset, PaletteIndex: s.PaletteIndex, Alpha: s.Alpha}
	}
	return p.colorLine(plain, extend)
}

// mask rasterises a glyph's outline as coverage, narrowed by the clip it
// is drawn inside.
func (p *colrPainter) mask(gid tables.GlyphID, m affine, clip []float32) []float32 {
	p.rast.Reset(p.w, p.h)
	if _, ok := p.f.walkOutline(p.face, font.GID(gid), p.toPix.mul(m), p.rast); !ok {
		return nil
	}
	clear(p.alpha.Pix)
	p.rast.Draw(p.alpha, p.alpha.Bounds(), image.Opaque, image.Point{})
	out := make([]float32, p.w*p.h)
	any := false
	for y := range p.h {
		row := p.alpha.Pix[y*p.alpha.Stride:]
		for x := range p.w {
			v := float32(row[x]) / 255
			if clip != nil {
				v *= clip[y*p.w+x]
			}
			out[y*p.w+x] = v
			any = any || v > 0
		}
	}
	if !any {
		return nil
	}
	return out
}

// composite combines a source canvas into this one under a COLR
// composite mode. The Porter-Duff modes and the separable blend modes
// are drawn as the specification gives them; the three non-separable
// modes, which mix hue, saturation, colour and luminosity, draw as
// source over.
func (c *colorCanvas) composite(src *colorCanvas, mode tables.CompositeMode) {
	blend := separableBlend(mode)
	for i := 0; i < len(c.pix); i += 4 {
		as, ab := src.pix[i+3], c.pix[i+3]
		if as == 0 && ab == 0 {
			continue
		}
		var fs, fb float32 // how much of the source and the backdrop survive
		switch mode {
		case tables.CompositeClear:
			fs, fb = 0, 0
		case tables.CompositeSrc:
			fs, fb = 1, 0
		case tables.CompositeDest:
			fs, fb = 0, 1
		case tables.CompositeSrcOver:
			fs, fb = 1, 1-as
		case tables.CompositeDestOver:
			fs, fb = 1-ab, 1
		case tables.CompositeSrcIn:
			fs, fb = ab, 0
		case tables.CompositeDestIn:
			fs, fb = 0, as
		case tables.CompositeSrcOut:
			fs, fb = 1-ab, 0
		case tables.CompositeDestOut:
			fs, fb = 0, 1-as
		case tables.CompositeSrcAtop:
			fs, fb = ab, 1-as
		case tables.CompositeDestAtop:
			fs, fb = 1-ab, as
		case tables.CompositeXor:
			fs, fb = 1-ab, 1-as
		case tables.CompositePlus:
			fs, fb = 1, 1
		default:
			fs, fb = 1, 1-as
		}
		for k := range 4 {
			v := src.pix[i+k]*fs + c.pix[i+k]*fb
			if blend != nil && k < 3 && as > 0 && ab > 0 {
				// The blended part replaces the overlap of the two.
				cs, cb := src.pix[i+k]/as, c.pix[i+k]/ab
				v = (1-ab)*src.pix[i+k] + (1-as)*c.pix[i+k] + as*ab*blend(cs, cb)
			}
			c.pix[i+k] = lin.Clamp(v, 0, 1)
		}
		if blend != nil {
			c.pix[i+3] = lin.Clamp(as+ab-as*ab, 0, 1)
		}
	}
}

// separableBlend returns the blend function of a separable blend mode,
// which works on one channel at a time, or nil for the Porter-Duff modes
// and the non-separable ones.
func separableBlend(mode tables.CompositeMode) func(cs, cb float32) float32 {
	switch mode {
	case tables.CompositeScreen:
		return func(cs, cb float32) float32 { return cs + cb - cs*cb }
	case tables.CompositeOverlay:
		return func(cs, cb float32) float32 { return hardLight(cb, cs) }
	case tables.CompositeDarken:
		return func(cs, cb float32) float32 { return min(cs, cb) }
	case tables.CompositeLighten:
		return func(cs, cb float32) float32 { return max(cs, cb) }
	case tables.CompositeColorDodge:
		return func(cs, cb float32) float32 {
			switch {
			case cb <= 0:
				return 0
			case cs >= 1:
				return 1
			}
			return min(1, cb/(1-cs))
		}
	case tables.CompositeColorBurn:
		return func(cs, cb float32) float32 {
			switch {
			case cb >= 1:
				return 1
			case cs <= 0:
				return 0
			}
			return 1 - min(1, (1-cb)/cs)
		}
	case tables.CompositeHardLight:
		return hardLight
	case tables.CompositeSoftLight:
		return softLight
	case tables.CompositeDifference:
		return func(cs, cb float32) float32 { return abs32(cb - cs) }
	case tables.CompositeExclusion:
		return func(cs, cb float32) float32 { return cs + cb - 2*cs*cb }
	case tables.CompositeMultiply:
		return func(cs, cb float32) float32 { return cs * cb }
	}
	return nil
}

func hardLight(cs, cb float32) float32 {
	if cs <= 0.5 {
		return 2 * cs * cb
	}
	return 1 - 2*(1-cs)*(1-cb)
}

func softLight(cs, cb float32) float32 {
	var d float32
	switch {
	case cb <= 0.25:
		d = ((16*cb-12)*cb + 4) * cb
	default:
		d = float32(math.Sqrt(float64(cb)))
	}
	if cs <= 0.5 {
		return cb - (1-2*cs)*cb*(1-cb)
	}
	return cb + (2*cs-1)*(d-cb)
}
