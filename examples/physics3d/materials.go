package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type cubeKind uint8

const (
	brushedMetal cubeKind = iota
	gold
	carPaint
	velvet
	glass
	glowing
	wood
	plastic
)

// Nine weighted slots preserve the simulation's seeded random sequence.
// Wood takes two of the former plastic slots so grain is easy to find.
var cubeKinds = [...]cubeKind{brushedMetal, gold, carPaint, velvet, glass, glowing, wood, wood, plastic}

// materialLibrary shares twelve GPU images across all cubes and the pen.
// Images are generated once during Init; dropping cubes only copies materials.
type materialLibrary struct {
	wood, metal, plastic, concrete gfx.Material
	textures                       []*gfx.Texture
}

func (m *materialLibrary) init(gr *gfx.Graphics) error {
	for _, surface := range []struct {
		name     string
		material *gfx.Material
		pixel    func(float64, float64) (color.RGBA, float64, float64)
		metallic uint8
	}{
		{"wood", &m.wood, woodPixel, 0},
		{"brushed metal", &m.metal, metalPixel, 255},
		{"molded plastic", &m.plastic, plasticPixel, 0},
		{"concrete", &m.concrete, concretePixel, 0},
	} {
		albedo, normal, roughness := surfaceImages(surface.pixel, surface.metallic)
		for _, channel := range []struct {
			name string
			src  *image.RGBA
			dst  **gfx.Texture
			data bool
		}{
			{"albedo", albedo, &surface.material.Texture, false},
			{"normal", normal, &surface.material.NormalTexture, true},
			{"metal/roughness", roughness, &surface.material.MetalRoughTexture, true},
		} {
			texture, err := gr.NewTexture(channel.src, gfx.TextureOptions{Linear: true, Repeat: true, Data: channel.data})
			if err != nil {
				// Graphics owns prior uploads and also releases them if Init fails.
				return fmt.Errorf("physics3d: %s %s texture: %w", surface.name, channel.name, err)
			}
			*channel.dst = texture
			m.textures = append(m.textures, texture)
		}
		// The map carries the absolute roughness, so its factor must be one.
		surface.material.Roughness = 1
	}
	m.wood.Clearcoat, m.wood.ClearcoatRoughness = 0.15, 0.35
	return nil
}

// cubeMaterial retains the optical showcase alongside the textured surfaces.
func (m *materialLibrary) cubeMaterial(kind cubeKind, c gfx.Color) gfx.Material {
	switch kind {
	case brushedMetal:
		mat := m.metal
		mat.BaseColor = c
		return mat
	case gold:
		return gfx.Material{BaseColor: gfx.RGB(255, 200, 90), Metallic: 1, Roughness: 0.1}
	case carPaint:
		return gfx.Material{BaseColor: c, Roughness: 0.5, Clearcoat: 1, ClearcoatRoughness: 0.05}
	case velvet:
		return gfx.Material{BaseColor: c, Roughness: 0.95, Sheen: gfx.RGB(255, 255, 255), SheenRoughness: 0.5}
	case glass:
		return gfx.Material{Roughness: 0.05, Transmission: 1, IOR: 1.5, Thickness: 1, AttenuationColor: c, AttenuationDistance: 2}
	case glowing:
		return gfx.Material{BaseColor: c, Roughness: 0.6, Emissive: 1.2}
	case wood:
		return m.wood
	default:
		mat := m.plastic
		mat.BaseColor = c
		mat.UVTransform = lin.Scale2(2, 2)
		return mat
	}
}

// surfaceImages samples a periodic surface into albedo, tangent-space normals
// and glTF G-roughness/B-metallic maps. Wrapped height differences keep the
// normal map continuous at tile boundaries; linear filtering supplies mipmaps.
func surfaceImages(pixel func(float64, float64) (color.RGBA, float64, float64), metallic uint8) (*image.RGBA, *image.RGBA, *image.RGBA) {
	const size = 256
	bounds := image.Rect(0, 0, size, size)
	albedo, normal, roughness := image.NewRGBA(bounds), image.NewRGBA(bounds), image.NewRGBA(bounds)
	heights := make([]float64, size*size)
	for y := range size {
		for x := range size {
			c, h, r := pixel(float64(x)/size, float64(y)/size)
			albedo.SetRGBA(x, y, c)
			heights[y*size+x] = h
			roughness.SetRGBA(x, y, color.RGBA{R: 255, G: channelByte(r), B: metallic, A: 255})
		}
	}
	for y := range size {
		for x := range size {
			dx := (heights[y*size+(x+1)%size] - heights[y*size+(x+size-1)%size]) * 2
			dy := (heights[((y+1)%size)*size+x] - heights[((y+size-1)%size)*size+x]) * 2
			length := math.Sqrt(dx*dx + dy*dy + 1)
			normal.SetRGBA(x, y, color.RGBA{R: channelByte(0.5 - dx/length*0.5), G: channelByte(0.5 - dy/length*0.5), B: channelByte(0.5 + 0.5/length), A: 255})
		}
	}
	return albedo, normal, roughness
}

func woodPixel(u, v float64) (color.RGBA, float64, float64) {
	warp := 0.10*math.Sin(2*math.Pi*v) + 0.025*math.Sin(4*math.Pi*v)
	grain := 0.5 + 0.5*math.Sin(2*math.Pi*(u*7+warp))
	fiber := 0.5 + 0.5*math.Sin(2*math.Pi*(u*43+warp*5))
	pore := tileNoise(u, v, 96, 8)
	tone := 0.25 + 0.55*grain + 0.12*fiber + 0.08*pore
	return color.RGBA{R: uint8(92 + 108*tone), G: uint8(43 + 96*tone), B: uint8(18 + 56*tone), A: 255},
		0.25*grain + 0.05*fiber + 0.04*pore, 0.48 + 0.22*(1-grain)
}

func metalPixel(u, v float64) (color.RGBA, float64, float64) {
	brush := tileNoise(u, v, 128, 4)
	streak := tileNoise(u, v, 32, 2)
	scratch := math.Pow(0.5+0.5*math.Sin(2*math.Pi*(u*59+0.02*math.Sin(2*math.Pi*v))), 16)
	tone := 0.73 + 0.16*brush + 0.08*streak - 0.10*scratch
	b := channelByte(tone)
	return color.RGBA{R: b, G: b, B: b, A: 255}, 0.14*brush - 0.08*scratch, 0.23 + 0.26*brush + 0.10*scratch
}

func plasticPixel(u, v float64) (color.RGBA, float64, float64) {
	grain := tileNoise(u, v, 64, 64)
	b := channelByte(0.94 + 0.06*grain)
	return color.RGBA{R: b, G: b, B: b, A: 255}, 0.10 * grain, 0.57 + 0.17*grain
}

func concretePixel(u, v float64) (color.RGBA, float64, float64) {
	mottle := tileNoise(u, v, 5, 5)
	aggregate := tileNoise(u, v, 32, 32)
	pores := math.Max(0, tileNoise(u, v, 96, 96)-0.65) * 2
	b := channelByte(0.53 + 0.08*mottle + 0.05*aggregate - 0.06*pores)
	return color.RGBA{R: b, G: b, B: b + 5, A: 255}, 0.14*aggregate - 0.22*pores, 0.80 + 0.16*aggregate
}

// tileNoise interpolates a wrapping integer lattice. Its local hash leaves
// the physics RNG untouched, and smooth interpolation avoids pixel speckle.
func tileNoise(u, v float64, nx, ny int) float64 {
	x, y := u*float64(nx), v*float64(ny)
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	x, y = x-math.Floor(x), y-math.Floor(y)
	x, y = x*x*(3-2*x), y*y*(3-2*y)
	hash := func(a, b int) float64 {
		h := uint32((a%nx+nx)%nx)*0x8da6b343 ^ uint32((b%ny+ny)%ny)*0xd8163841 ^ 0xcb1ab31f
		h ^= h >> 13
		h *= 0x85ebca6b
		h ^= h >> 16
		return float64(h&0xffff) / 65535
	}
	a, b := hash(ix, iy), hash(ix+1, iy)
	c, d := hash(ix, iy+1), hash(ix+1, iy+1)
	return (a+(b-a)*x)*(1-y) + (c+(d-c)*x)*y
}

func channelByte(v float64) uint8 {
	return uint8(math.Max(0, math.Min(1, v))*255 + 0.5)
}
