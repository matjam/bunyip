// Package gfx is what a game draws with: textures, a sprite batch for 2D,
// and (to come) meshes, materials and cameras for 3D. Coordinates for 2D
// are in the units set by SetView, +Y down, origin top-left.
package gfx

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
