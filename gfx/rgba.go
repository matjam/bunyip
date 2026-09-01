package gfx

import "image/color"

// rgbaPremul is white coverage a as a premultiplied pixel.
func rgbaPremul(a uint8) color.RGBA { return color.RGBA{a, a, a, a} }
