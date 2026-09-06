package gfx

import (
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"

	"github.com/matjam/bunyip/lin"
)

// glyphInk measures the painted outline instead of the padded atlas quad.
// Colour/bitmap glyphs use their nonzero-alpha pixel bounds.
func (f *Font) glyphInk(face uint8, gid font.GID, gl glyph) lin.Rect {
	key := glyphKey{face: face, gid: gid}
	if r, ok := f.ink[key]; ok {
		return r
	}
	var r lin.Rect
	ol, has := f.faces[face].face.GlyphDataOutline(uint16(gid))
	if has && !gl.color && len(ol.Segments) > 0 {
		var path Path
		k := f.pxPerEm / f.faces[face].upem / f.scale
		for _, s := range ol.Segments {
			x, y := s.Args[0].X*k, -s.Args[0].Y*k
			switch s.Op {
			case ot.SegmentOpMoveTo:
				path.MoveTo(x, y)
			case ot.SegmentOpLineTo:
				path.LineTo(x, y)
			case ot.SegmentOpQuadTo:
				path.QuadTo(x, y, s.Args[1].X*k, -s.Args[1].Y*k)
			case ot.SegmentOpCubeTo:
				path.CubicTo(x, y, s.Args[1].X*k, -s.Args[1].Y*k, s.Args[2].X*k, -s.Args[2].Y*k)
			}
		}
		r = path.Bounds()
	} else if !gl.empty {
		side := float32(f.packer.width)
		x0, y0 := int(gl.uv0.X*side+0.5), int(gl.uv0.Y*side+0.5)
		x1, y1 := int(gl.uv1.X*side+0.5), int(gl.uv1.Y*side+0.5)
		loX, loY, hiX, hiY := x1, y1, x0, y0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if f.pix.RGBAAt(x, y).A > 0 {
					loX = min(loX, x)
					loY = min(loY, y)
					hiX = max(hiX, x+1)
					hiY = max(hiY, y+1)
				}
			}
		}
		if hiX > loX && hiY > loY {
			kx, ky := gl.size.X/float32(x1-x0), gl.size.Y/float32(y1-y0)
			r = lin.R(gl.bearing.X+float32(loX-x0)*kx, gl.bearing.Y+float32(loY-y0)*ky, float32(hiX-loX)*kx, float32(hiY-loY)*ky)
		}
	}
	if f.ink == nil {
		f.ink = make(map[glyphKey]lin.Rect)
	}
	f.ink[key] = r
	return r
}
