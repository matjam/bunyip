package gfx

import (
	"fmt"
	"image"
	"math"

	"github.com/go-text/typesetting/font"
	xdraw "golang.org/x/image/draw"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/lin"
)

const maxOutlineSpread = 64
const maxOutlinePageSide = 2048
const maxOutlineRasterSide = 2048

type outlineGlyph struct {
	page  *outlinePage
	image glyph
}

// Pages grow by adding glyphs without moving existing UVs. Spread buckets
// make changing a width within a bucket an ordinary shader parameter change.
type outlinePage struct {
	font    *Font
	spread  int
	packer  shelfPacker
	pix     *image.RGBA
	texture *Texture
	dirty   image.Rectangle
	glyphs  map[glyphKey]glyph
}

func (f *Font) outlinedGlyph(face uint8, gid font.GID, original glyph, width, sizeScale float32) (outlineGlyph, error) {
	needed := width * float32(sdfEmPixels) / (f.Size * sizeScale)
	if needed+2 > maxOutlineSpread {
		return outlineGlyph{}, fmt.Errorf("gfx: text outline width %g exceeds the maximum %g view units at this size", width, float32(maxOutlineSpread-2)*f.Size*sizeScale/sdfEmPixels)
	}
	spread := 8
	for float32(spread) < needed+2 {
		spread *= 2
	}
	key := glyphKey{face: face, gid: gid}
	for _, p := range f.outlinePages {
		if p.spread >= spread {
			if gl, ok := p.glyphs[key]; ok {
				return outlineGlyph{page: p, image: gl}, nil
			}
		}
	}
	if f.g.textOutlineShader == nil {
		s, err := f.g.NewShader(shaders.TextOutlineFrag)
		if err != nil {
			return outlineGlyph{}, fmt.Errorf("gfx: text outline shader: %w", err)
		}
		f.g.textOutlineShader = s
	}
	mask, ox, oy, err := f.outlineMask(face, gid, original, spread)
	if err != nil {
		return outlineGlyph{}, err
	}
	mw, mh := mask.Rect.Dx(), mask.Rect.Dy()
	w, h := (mw+sdfOversample-1)/sdfOversample, (mh+sdfOversample-1)/sdfOversample
	var page *outlinePage
	x, y := 0, 0
	for _, p := range f.outlinePages {
		if p.spread != spread {
			continue
		}
		packer := p.packer
		if px, py, ok := packer.place(w, h); ok {
			page, x, y = p, px, py
			p.packer = packer
			break
		}
	}
	if page == nil {
		if len(f.outlinePages) >= f.outlineBudget {
			return outlineGlyph{}, fmt.Errorf("gfx: outline atlas page budget (%d) exhausted; increase FontOptions.OutlinePages", f.outlineBudget)
		}
		limit := min(maxOutlinePageSide, int(f.g.r.Device.Limits().MaxImageDimension2D))
		side := min(f.packer.width, limit)
		for side < max(w, h)+2 && side < limit {
			side = min(max(side*2, 1), limit)
		}
		if max(w, h)+2 > side {
			return outlineGlyph{}, fmt.Errorf("gfx: outlined glyph %d by %d exceeds atlas page limit %d", w, h, limit)
		}
		page = &outlinePage{font: f, spread: spread, packer: shelfPacker{width: side, height: side, pad: 1}, pix: image.NewRGBA(image.Rect(0, 0, side, side)), glyphs: map[glyphKey]glyph{}}
		x, y, _ = page.packer.place(w, h)
		f.outlinePages = append(f.outlinePages, page)
	}
	field := distanceField(mask, spread*sdfOversample)
	for yy := range h {
		for xx := range w {
			var sum float64
			n := 0
			for sy := range sdfOversample {
				for sx := range sdfOversample {
					px, py := xx*sdfOversample+sx, yy*sdfOversample+sy
					if px < mw && py < mh {
						sum += field[py*mw+px]
						n++
					}
				}
			}
			page.pix.SetRGBA(x+xx, y+yy, rgbaPremul(uint8(math.Round(sum/float64(n)*255))))
		}
	}
	scale := float32(sdfEmPixels*sdfOversample) / f.Size
	side := float32(page.packer.width)
	gl := glyph{uv0: lin.V2(float32(x)/side, float32(y)/side), uv1: lin.V2(float32(x+w)/side, float32(y+h)/side), bearing: lin.V2(float32(ox)/scale, float32(oy)/scale), size: lin.V2(float32(w*sdfOversample)/scale, float32(h*sdfOversample)/scale)}
	page.glyphs[key] = gl
	page.dirty = page.dirty.Union(image.Rect(x, y, x+w, y+h))
	return outlineGlyph{page: page, image: gl}, nil
}

func (f *Font) outlineMask(face uint8, gid font.GID, original glyph, spread int) (*image.Alpha, int, int, error) {
	scale := float32(sdfEmPixels*sdfOversample) / f.Size
	k := float32(sdfEmPixels*sdfOversample) / f.faces[face].upem
	pad := spread * sdfOversample
	x0, y0, x1, y1, ok := f.outline(face, gid, k, 0, 0, nil)
	if ok && !original.color {
		if x1-x0+float32(2*pad+2) > maxOutlineRasterSide || y1-y0+float32(2*pad+2) > maxOutlineRasterSide {
			return nil, 0, 0, fmt.Errorf("gfx: outlined glyph exceeds %d-pixel raster limit", maxOutlineRasterSide)
		}
		mask, ox, oy, ok := f.rasterise(face, gid, k, pad)
		if !ok {
			return nil, 0, 0, fmt.Errorf("gfx: cannot rasterize outlined glyph")
		}
		return mask, ox, oy, nil
	}
	// Colour emoji and bitmap-only faces use alpha coverage, retaining their
	// original colour image for the fill drawn above this outline.
	w, h := max(1, int(math.Ceil(float64(original.size.X*scale)))), max(1, int(math.Ceil(float64(original.size.Y*scale))))
	if w+2*pad > maxOutlineRasterSide || h+2*pad > maxOutlineRasterSide {
		return nil, 0, 0, fmt.Errorf("gfx: outlined bitmap glyph exceeds %d-pixel raster limit", maxOutlineRasterSide)
	}
	side := float32(f.packer.width)
	source := image.Rect(int(original.uv0.X*side+0.5), int(original.uv0.Y*side+0.5), int(original.uv1.X*side+0.5), int(original.uv1.Y*side+0.5))
	mask := image.NewAlpha(image.Rect(0, 0, w+2*pad, h+2*pad))
	xdraw.CatmullRom.Scale(mask, image.Rect(pad, pad, pad+w, pad+h), f.pix, source, xdraw.Src, nil)
	return mask, int(math.Floor(float64(original.bearing.X*scale))) - pad, int(math.Floor(float64(original.bearing.Y*scale))) - pad, nil
}

func (p *outlinePage) flush() error {
	if p.dirty.Empty() {
		return nil
	}
	if p.texture == nil {
		tex, err := p.font.g.NewTexture(p.pix, TextureOptions{Linear: true, Data: true, NoMipmaps: true})
		if err != nil {
			return err
		}
		p.texture = tex
		p.font.g.forget(tex)
		if entry := p.font.g.res.live[p.font]; entry != nil {
			entry.res.Bytes += textureBytes(p.pix.Bounds().Dx(), p.pix.Bounds().Dy(), false)
		}
	} else {
		r := p.dirty
		if err := p.texture.Write(r.Min.X, r.Min.Y, p.pix.SubImage(r)); err != nil {
			return err
		}
	}
	p.dirty = image.Rectangle{}
	return nil
}

// Explicit vec4 slots match text_outline.wgsl's uniform block without relying
// on implicit Go padding. Keep this shape when public shader packing evolves.
type outlineUniforms struct{ Color, Parameters lin.Vec4 }
