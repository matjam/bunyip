package gfx

import (
	"image/color"

	"github.com/matjam/bunyip/lin"
)

// rgbaPremul is white coverage a as a premultiplied pixel.
func rgbaPremul(a uint8) color.RGBA { return color.RGBA{a, a, a, a} }

// rgbaBytes encodes a premultiplied colour in linear light as an atlas
// pixel. The glyph atlas is sampled as data, so the bytes hold linear
// values rather than sRGB ones.
func rgbaBytes(r, g, b, a float32) color.RGBA {
	q := func(v float32) uint8 { return uint8(lin.Clamp(v, 0, 1)*255 + 0.5) }
	return color.RGBA{q(r), q(g), q(b), q(a)}
}
