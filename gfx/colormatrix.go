package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// ColorMatrix recolours sprites: the straight colour goes through M and
// gains Offset before the alpha is put back. Build one with the
// constructors, compose with Mul, and set it with SetColorMatrix or
// ColorMatrixed; the standard sprite shader applies it. It is laid out
// as the shader's uniform block.
type ColorMatrix struct {
	M      lin.Mat4
	Offset lin.Vec4
}

// ColorIdentity leaves colours alone.
func ColorIdentity() ColorMatrix { return ColorMatrix{M: lin.Identity()} }

// Luminance weights in linear light.
const (
	lumR = 0.2126
	lumG = 0.7152
	lumB = 0.0722
)

// Saturation scales colourfulness: 0 is greyscale, 1 unchanged, above 1
// more vivid.
func Saturation(s float32) ColorMatrix {
	var m lin.Mat4
	set := func(row, col int, v float32) { m[col*4+row] = v }
	for row, w := range []float32{lumR, lumG, lumB} {
		set(row, 0, (1-s)*lumR)
		set(row, 1, (1-s)*lumG)
		set(row, 2, (1-s)*lumB)
		_ = w
	}
	set(0, 0, (1-s)*lumR+s)
	set(1, 1, (1-s)*lumG+s)
	set(2, 2, (1-s)*lumB+s)
	set(3, 3, 1)
	return ColorMatrix{M: m}
}

// Grayscale drops all colour.
func Grayscale() ColorMatrix { return Saturation(0) }

// HueRotate turns every hue by angle radians around the grey axis.
func HueRotate(angle float32) ColorMatrix {
	c, s := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	var m lin.Mat4
	set := func(row, col int, v float32) { m[col*4+row] = v }
	// The classic hue rotation, with luminance preserved.
	set(0, 0, lumR+c*(1-lumR)+s*(-lumR))
	set(0, 1, lumG+c*(-lumG)+s*(-lumG))
	set(0, 2, lumB+c*(-lumB)+s*(1-lumB))
	set(1, 0, lumR+c*(-lumR)+s*0.143)
	set(1, 1, lumG+c*(1-lumG)+s*0.140)
	set(1, 2, lumB+c*(-lumB)+s*(-0.283))
	set(2, 0, lumR+c*(-lumR)+s*(-(1-lumR)))
	set(2, 1, lumG+c*(-lumG)+s*lumG)
	set(2, 2, lumB+c*(1-lumB)+s*lumB)
	set(3, 3, 1)
	return ColorMatrix{M: m}
}

// Brightness scales colours: below 1 darkens, above 1 brightens.
func Brightness(b float32) ColorMatrix { return ColorMatrix{M: lin.Scale(lin.V3(b, b, b))} }

// Contrast stretches colours about mid grey: 0 is flat grey, 1 unchanged.
func Contrast(c float32) ColorMatrix {
	return ColorMatrix{M: lin.Scale(lin.V3(c, c, c)), Offset: lin.V4(0.5*(1-c), 0.5*(1-c), 0.5*(1-c), 0)}
}

// Invert makes a negative.
func Invert() ColorMatrix {
	return ColorMatrix{M: lin.Scale(lin.V3(-1, -1, -1)), Offset: lin.V4(1, 1, 1, 0)}
}

// Sepia gives an old photograph's brown.
func Sepia() ColorMatrix {
	var m lin.Mat4
	rows := [3][3]float32{{0.393, 0.769, 0.189}, {0.349, 0.686, 0.168}, {0.272, 0.534, 0.131}}
	for r := range 3 {
		for c := range 3 {
			m[c*4+r] = rows[r][c]
		}
	}
	m[15] = 1
	return ColorMatrix{M: m}
}

// Tint scales each channel by a colour, like a sprite tint but after
// the matrix stack.
func Tint(c Color) ColorMatrix { return ColorMatrix{M: lin.Scale(lin.V3(c.R, c.G, c.B))} }

// Mul composes matrices so that m.Mul(n) applies n first, then m.
func (m ColorMatrix) Mul(n ColorMatrix) ColorMatrix {
	off := m.M.MulVec4(lin.V4(n.Offset.X, n.Offset.Y, n.Offset.Z, 0))
	return ColorMatrix{M: m.M.Mul(n.M), Offset: lin.V4(off.X+m.Offset.X, off.Y+m.Offset.Y, off.Z+m.Offset.Z, 0)}
}

// Apply runs a colour through the matrix on the CPU, for previews.
func (m ColorMatrix) Apply(c Color) Color {
	v := m.M.MulVec4(lin.V4(c.R, c.G, c.B, 1))
	return Color{lin.Clamp(v.X+m.Offset.X, 0, 1), lin.Clamp(v.Y+m.Offset.Y, 0, 1), lin.Clamp(v.Z+m.Offset.Z, 0, 1), c.A}
}

// SetColorMatrix recolours later sprite, text and shape drawing in the
// current queue through the matrix; nil restores plain colours. It is
// reset at the start of each frame, and a game's own SetShader takes
// precedence over it.
func (g *Graphics) SetColorMatrix(m *ColorMatrix) {
	q := g.cur
	q.colorMatrix = m
	if m != nil {
		g.recordDrawError(g.matrixShader.SetUniforms(m))
	}
}

// Light2D is a point light for lit sprites: a position in the same units
// as sprites, a height above their plane and a radius where it fades out.
type Light2D struct {
	Pos    lin.Vec2
	Height float32 // zero means 40
	Radius float32 // zero means 300
	Color  Color   // zero means white
	// Shadows makes the occluders added with AddOccluder2D block this
	// light, so a wall between it and a sprite darkens the sprite. It
	// costs one polar shadow map a frame, built on the CPU.
	Shadows bool
	// Softness is the width of the shadow's soft edge in view units at
	// the shadowed point; zero means 8. It has no effect without
	// Shadows.
	Softness float32
}

// maxLights2D is how many point lights one frame's lit sprites can take.
const maxLights2D = 8

// lights2D is the lit shader's uniform block.
type lights2D struct {
	Ambient lin.Vec4
	Pos     [maxLights2D]lin.Vec4
	Color   [maxLights2D]lin.Vec4
	// Shadow is x one when the light casts shadows, y the light's row in
	// the shadow strip, z the softness in view units, w how many
	// directions a row holds.
	Shadow [maxLights2D]lin.Vec4
}

// SetLights2D sets the ambient light and up to eight point lights that
// DrawLit sprites in the current queue are lit by, for this frame.
// Lights with Shadows are blocked by the frame's AddOccluder2D
// occluders. Lights past the eighth are dropped.
func (g *Graphics) SetLights2D(ambient Color, lights ...Light2D) {
	q := g.cur
	n := min(len(lights), maxLights2D)
	q.lights = lights2D{Ambient: lin.V4(ambient.R, ambient.G, ambient.B, float32(n))}
	q.shadows = false
	for i, l := range lights[:n] {
		h, r, c := l.Height, l.Radius, l.Color
		if h == 0 {
			h = 40
		}
		if r == 0 {
			r = 300
		}
		if c == (Color{}) {
			c = White
		}
		q.lights.Pos[i] = lin.V4(l.Pos.X, l.Pos.Y, h, r)
		q.lights.Color[i] = lin.V4(c.R, c.G, c.B, 1)
		if l.Shadows {
			soft := l.Softness
			if soft == 0 {
				soft = 8
			}
			// The row's own line of the strip, sampled at its centre.
			q.lights.Shadow[i] = lin.V4(1, (float32(i)+0.5)/maxLights2D, soft, shadowAngles2D)
			q.shadows = true
		}
	}
	q.lightsDirty = true
}

// DrawLit draws a sprite lit by the SetLights2D lights through a
// tangent-space normal map (a texture made with TextureOptions.Data),
// so a flat sprite catches light from the side a torch is on. Lights
// that cast shadows are blocked by the occluders AddOccluder2D added
// this frame.
func (g *Graphics) DrawLit(tex, normal *Texture, s Sprite) {
	g.requireTextureOwner(tex)
	g.requireTextureOwner(normal)
	q := g.cur
	if q.lightsDirty {
		g.recordDrawError(g.litShader.SetUniforms(&q.lights))
		q.lightsDirty = false
	}
	g.litShader.SetImage(0, normal)
	g.litShader.SetImage(1, q.shadowTex)
	g.Shaded(g.litShader, func() { g.Draw(tex, s) })
}
