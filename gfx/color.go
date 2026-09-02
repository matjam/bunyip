// Package gfx is what a game draws with: textures and a sprite batch,
// vector paths and shaped text for 2D; meshes, materials, lights, cameras
// and post-processing for 3D. Coordinates for 2D are in the units set by
// SetView, +Y down, origin top-left.
//
// Conventions. Option and material fields follow "zero means the
// default": a zero Roughness is 0.6, a zero IOR is 1.5, a zero Color where
// a tint is expected is white. A field whose zero must mean something of
// its own is named for that zero (NoMipmaps, NoDepthTest, Sky.Vacuum), so
// an empty struct is always a sensible starting point. Sizes and positions
// are float32 view units unless a name says pixels; rectangles are
// lin.Rect. Every GPU resource has a Destroy that must not run while a
// frame using it is in flight.
//
// Engine plumbing. New, Begin, End, Resize, SetTime and Destroy on
// Graphics are called by the engine loop; a game under bunyip.Run never
// needs them.
package gfx

import "github.com/matjam/bunyip/lin"

// Color is a straight (non-premultiplied) RGBA colour in linear light,
// each channel 0..1.
type Color struct{ R, G, B, A float32 }

var (
	White       = Color{1, 1, 1, 1}
	Black       = Color{0, 0, 0, 1}
	Transparent = Color{}
)

// RGB makes an opaque colour from 8-bit sRGB channels.
func RGB(r, g, b uint8) Color { return RGBA(r, g, b, 255) }

// RGBA makes a colour from 8-bit sRGB channels and straight alpha.
func RGBA(r, g, b, a uint8) Color {
	return Color{srgbToLinear(r), srgbToLinear(g), srgbToLinear(b), float32(a) / 255}
}

// WithAlpha returns the colour with a different alpha.
func (c Color) WithAlpha(a float32) Color { return Color{c.R, c.G, c.B, a} }

// Lerp interpolates from c (t 0) to d (t 1), channel by channel.
func (c Color) Lerp(d Color, t float32) Color {
	return Color{c.R + (d.R-c.R)*t, c.G + (d.G-c.G)*t, c.B + (d.B-c.B)*t, c.A + (d.A-c.A)*t}
}

// Mul tints c by d, channel by channel: a sprite's colour times a
// team colour.
func (c Color) Mul(d Color) Color { return Color{c.R * d.R, c.G * d.G, c.B * d.B, c.A * d.A} }

// Scale brightens or darkens the colour, leaving alpha alone; values
// above 1 make emissive colours for bloom.
func (c Color) Scale(s float32) Color { return Color{c.R * s, c.G * s, c.B * s, c.A} }

// Premultiplied returns the colour with RGB scaled by alpha, the form the
// blend modes and DrawTriangles vertices use.
func (c Color) Premultiplied() Color { return Color{c.R * c.A, c.G * c.A, c.B * c.A, c.A} }

// Vec4 returns the channels as a vector, for shader uniforms.
func (c Color) Vec4() lin.Vec4 { return lin.V4(c.R, c.G, c.B, c.A) }

// HSV returns the hue in degrees (0..360), saturation and value (0..1) of
// the colour in linear light.
func (c Color) HSV() (h, s, v float32) {
	hi, lo := max(c.R, c.G, c.B), min(c.R, c.G, c.B)
	v = hi
	d := hi - lo
	if hi > 0 {
		s = d / hi
	}
	if d == 0 {
		return 0, s, v
	}
	switch hi {
	case c.R:
		h = (c.G - c.B) / d
		if h < 0 {
			h += 6
		}
	case c.G:
		h = (c.B-c.R)/d + 2
	default:
		h = (c.R-c.G)/d + 4
	}
	return h * 60, s, v
}

// FromHSV makes an opaque colour in linear light from a hue in degrees,
// saturation and value in 0..1: the easy way to pick distinct team or
// debug colours.
func FromHSV(h, s, v float32) Color {
	h = h / 60
	for h < 0 {
		h += 6
	}
	for h >= 6 {
		h -= 6
	}
	i := int(h)
	f := h - float32(i)
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	switch i {
	case 0:
		return Color{v, t, p, 1}
	case 1:
		return Color{q, v, p, 1}
	case 2:
		return Color{p, v, t, 1}
	case 3:
		return Color{p, q, v, 1}
	case 4:
		return Color{t, p, v, 1}
	}
	return Color{v, p, q, 1}
}

// premultiplied returns the colour with RGB scaled by alpha, which is what
// the blend mode expects.
func (c Color) premultiplied() [4]float32 {
	return [4]float32{c.R * c.A, c.G * c.A, c.B * c.A, c.A}
}

var srgbTable = func() (t [256]float32) {
	for i := range t {
		v := float64(i) / 255
		if v <= 0.04045 {
			t[i] = float32(v / 12.92)
		} else {
			t[i] = float32(pow((v+0.055)/1.055, 2.4))
		}
	}
	return
}()

func srgbToLinear(v uint8) float32 { return srgbTable[v] }
