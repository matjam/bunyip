package gfx

import (
	"fmt"
	"image"
	"math"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Environment is distant light from every direction, for image-based
// lighting: metals reflect it, rough surfaces are tinted by it, and it can
// be drawn as the sky behind the scene. Build one from an equirectangular
// panorama with NewEnvironment or NewEnvironmentHDR and set it on the
// Light; without one the light's procedural Sky does the same job from
// parameters alone.
type Environment struct {
	cube   *render.Image
	set    vk.VkDescriptorSet // for the sky pass
	sh     [9]lin.Vec4        // irradiance as spherical harmonics, scaled for direct use
	mips   int
	scale  float32
	g      *Graphics
	radius float32
}

// EnvironmentOptions tunes an environment.
type EnvironmentOptions struct {
	// Intensity multiplies the environment's light; zero means 1. A photo
	// of an overcast day is around 1; a bright sky panorama may need more.
	Intensity float32
	// Size is the cube map's side in texels; zero means 128. Larger is
	// sharper in mirror-like reflections and slower to prepare.
	Size int
}

const envMips = 6

// NewEnvironment builds an environment from an equirectangular panorama:
// longitude across, latitude down, sRGB colour, any size. It prefilters
// the image for every roughness (a second or so for a large panorama).
func (g *Graphics) NewEnvironment(panorama image.Image, opts EnvironmentOptions) (*Environment, error) {
	b := panorama.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("gfx: environment image has empty bounds")
	}
	src := newRadianceMap(b.Dx(), b.Dy())
	for y := range src.h {
		for x := range src.w {
			r, gg, bb, _ := panorama.At(b.Min.X+x, b.Min.Y+y).RGBA()
			src.set(x, y, srgbToLinear(uint8(r>>8)), srgbToLinear(uint8(gg>>8)), srgbToLinear(uint8(bb>>8)))
		}
	}
	return g.newEnvironment(src, opts)
}

func lerpColor(a, b Color, t float32) Color {
	return Color{a.R + (b.R-a.R)*t, a.G + (b.G-a.G)*t, a.B + (b.B-a.B)*t, 1}
}

// radianceMap is a float RGB equirectangular image.
type radianceMap struct {
	w, h int
	pix  []float32
}

func newRadianceMap(w, h int) *radianceMap {
	return &radianceMap{w: w, h: h, pix: make([]float32, w*h*3)}
}

func (m *radianceMap) set(x, y int, r, g, b float32) {
	i := (y*m.w + x) * 3
	m.pix[i], m.pix[i+1], m.pix[i+2] = r, g, b
}

func (m *radianceMap) at(x, y int) (r, g, b float32) {
	x = ((x % m.w) + m.w) % m.w
	y = min(max(y, 0), m.h-1)
	i := (y*m.w + x) * 3
	return m.pix[i], m.pix[i+1], m.pix[i+2]
}

// sample reads the radiance in a direction (y up) with bilinear filtering.
func (m *radianceMap) sample(d lin.Vec3) (r, g, b float32) {
	u := 0.5 + float32(math.Atan2(float64(d.X), float64(-d.Z)))/(2*math.Pi)
	v := float32(math.Acos(float64(lin.Clamp(d.Y, -1, 1)))) / math.Pi
	fx, fy := u*float32(m.w)-0.5, v*float32(m.h)-0.5
	x0, y0 := int(math.Floor(float64(fx))), int(math.Floor(float64(fy)))
	tx, ty := fx-float32(x0), fy-float32(y0)
	r00, g00, b00 := m.at(x0, y0)
	r10, g10, b10 := m.at(x0+1, y0)
	r01, g01, b01 := m.at(x0, y0+1)
	r11, g11, b11 := m.at(x0+1, y0+1)
	lerp := func(a, b, t float32) float32 { return a + (b-a)*t }
	return lerp(lerp(r00, r10, tx), lerp(r01, r11, tx), ty),
		lerp(lerp(g00, g10, tx), lerp(g01, g11, tx), ty),
		lerp(lerp(b00, b10, tx), lerp(b01, b11, tx), ty)
}

// cubeDir is the world direction through a cube face texel, in Vulkan's
// face order and orientation.
func cubeDir(face int, u, v float32) lin.Vec3 {
	switch face {
	case 0:
		return lin.V3(1, -v, -u)
	case 1:
		return lin.V3(-1, -v, u)
	case 2:
		return lin.V3(u, 1, v)
	case 3:
		return lin.V3(u, -1, -v)
	case 4:
		return lin.V3(u, -v, 1)
	}
	return lin.V3(-u, -v, -1)
}

func (g *Graphics) newEnvironment(src *radianceMap, opts EnvironmentOptions) (*Environment, error) {
	size := opts.Size
	if size <= 0 {
		size = 128
	}
	size = max(8, size)
	env := &Environment{g: g, mips: envMips, scale: opts.Intensity}
	if env.scale == 0 {
		env.scale = 1
	}
	// Prefilter one level per roughness step: level 0 is the panorama
	// itself, later levels blur it with the GGX lobe of their roughness.
	faces := make([][6][]byte, envMips)
	for level := range envMips {
		side := max(size>>level, 1)
		roughness := float32(level) / float32(envMips-1)
		for face := range 6 {
			pix := make([]byte, side*side*8)
			for y := range side {
				for x := range side {
					u := 2*(float32(x)+0.5)/float32(side) - 1
					v := 2*(float32(y)+0.5)/float32(side) - 1
					n := cubeDir(face, u, v).Norm()
					var r, gg, b float32
					if level == 0 {
						r, gg, b = src.sample(n)
					} else {
						r, gg, b = prefilter(src, n, roughness)
					}
					i := (y*side + x) * 8
					putF16(pix[i:], r)
					putF16(pix[i+2:], gg)
					putF16(pix[i+4:], b)
					putF16(pix[i+6:], 1)
				}
			}
			faces[level][face] = pix
		}
	}
	var err error
	if env.cube, err = g.r.Device.NewCubemapImage(uint32(size), vk.VK_FORMAT_R16G16B16A16_SFLOAT, 8, faces); err != nil {
		return nil, err
	}
	env.sh = irradianceSH(src)
	if env.set, err = g.descriptors.AllocateMany(g.cubeBindings(env.cube)); err != nil {
		env.cube.Destroy()
		return nil, err
	}
	return env, nil
}

// cubeBindings is a cube map at binding 0 with white in the other slots.
func (g *Graphics) cubeBindings(cube *render.Image) []render.SamplerBinding {
	bindings := make([]render.SamplerBinding, 5)
	bindings[0] = render.SamplerBinding{View: cube.View, Sampler: g.linear}
	for i := 1; i < 5; i++ {
		bindings[i] = render.SamplerBinding{View: g.white.img.View, Sampler: g.nearest}
	}
	return bindings
}

// prefilter convolves the panorama with the GGX lobe around n for a
// roughness, by importance sampling.
func prefilter(src *radianceMap, n lin.Vec3, roughness float32) (r, g, b float32) {
	const samples = 64
	a := roughness * roughness
	up := lin.V3(0, 0, 1)
	if abs32(n.Z) > 0.999 {
		up = lin.V3(1, 0, 0)
	}
	tx := up.Cross(n).Norm()
	ty := n.Cross(tx)
	var weight float32
	for i := range samples {
		// Hammersley point, mapped to a GGX half vector.
		e1 := (float32(i) + 0.5) / samples
		e2 := radicalInverse(uint32(i))
		phi := 2 * math.Pi * e1
		cosTheta := float32(math.Sqrt(float64((1 - e2) / (1 + (a*a-1)*e2))))
		sinTheta := float32(math.Sqrt(float64(1 - cosTheta*cosTheta)))
		h := tx.Mul(sinTheta * cos32(phi)).Add(ty.Mul(sinTheta * sin32(phi))).Add(n.Mul(cosTheta))
		l := h.Mul(2 * n.Dot(h)).Sub(n) // reflect n about h (view = normal)
		nl := n.Dot(l)
		if nl <= 0 {
			continue
		}
		sr, sg, sb := src.sample(l)
		r += sr * nl
		g += sg * nl
		b += sb * nl
		weight += nl
	}
	if weight > 0 {
		r, g, b = r/weight, g/weight, b/weight
	}
	return r, g, b
}

// radicalInverse is the Van der Corput sequence in base 2.
func radicalInverse(bits uint32) float32 {
	bits = (bits << 16) | (bits >> 16)
	bits = ((bits & 0x55555555) << 1) | ((bits & 0xAAAAAAAA) >> 1)
	bits = ((bits & 0x33333333) << 2) | ((bits & 0xCCCCCCCC) >> 2)
	bits = ((bits & 0x0F0F0F0F) << 4) | ((bits & 0xF0F0F0F0) >> 4)
	bits = ((bits & 0x00FF00FF) << 8) | ((bits & 0xFF00FF00) >> 8)
	return float32(bits) * 2.3283064365386963e-10
}

// irradianceSH projects the panorama onto nine spherical harmonics and
// folds in the cosine-lobe convolution and the 1/π of a Lambertian
// surface, so the shader's sum is the diffuse radiance for an albedo of 1.
func irradianceSH(src *radianceMap) [9]lin.Vec4 {
	return shProject(src.sample, 128, 64)
}

// shProject integrates a radiance function over the sphere on a w by h
// longitude-latitude grid into nine spherical harmonics, convolved with
// the cosine lobe so the shader sums them straight into irradiance.
func shProject(sample func(lin.Vec3) (r, g, b float32), w, h int) [9]lin.Vec4 {
	var sh [9][3]float64
	for y := range h {
		theta := math.Pi * (float64(y) + 0.5) / float64(h)
		for x := range w {
			phi := 2 * math.Pi * (float64(x) + 0.5) / float64(w)
			d := lin.V3(float32(math.Sin(theta)*math.Sin(phi)), float32(math.Cos(theta)), -float32(math.Sin(theta)*math.Cos(phi)))
			r, g, b := sample(d)
			dOmega := (2 * math.Pi / float64(w)) * (math.Pi / float64(h)) * math.Sin(theta)
			basis := shBasis(d)
			for i := range 9 {
				sh[i][0] += float64(r) * basis[i] * dOmega
				sh[i][1] += float64(g) * basis[i] * dOmega
				sh[i][2] += float64(b) * basis[i] * dOmega
			}
		}
	}
	// Cosine lobe: A0 = π, A1 = 2π/3, A2 = π/4; divided by π for Lambert.
	scale := [9]float64{1, 2.0 / 3, 2.0 / 3, 2.0 / 3, 0.25, 0.25, 0.25, 0.25, 0.25}
	var out [9]lin.Vec4
	for i := range 9 {
		out[i] = lin.V4(float32(sh[i][0]*scale[i]), float32(sh[i][1]*scale[i]), float32(sh[i][2]*scale[i]), 0)
	}
	return out
}

// shBasis evaluates the nine real spherical harmonics at a direction.
func shBasis(d lin.Vec3) [9]float64 {
	x, y, z := float64(d.X), float64(d.Y), float64(d.Z)
	return [9]float64{
		0.282095,
		0.488603 * y, 0.488603 * z, 0.488603 * x,
		1.092548 * x * y, 1.092548 * y * z, 0.315392 * (3*z*z - 1), 1.092548 * x * z, 0.546274 * (x*x - y*y),
	}
}

// putF16 writes a float32 as a half float.
func putF16(dst []byte, f float32) {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xff) - 127 + 15
	mant := bits & 0x7fffff
	var h uint16
	switch {
	case exp <= 0:
		h = sign // flush tiny values to zero
	case exp >= 31:
		h = sign | 0x7bff // clamp to the largest finite half
	default:
		h = sign | uint16(exp<<10) | uint16(mant>>13)
	}
	dst[0], dst[1] = byte(h), byte(h>>8)
}

// Destroy frees the environment. It must not be in use by a frame in flight.
func (env *Environment) Destroy() {
	if env == nil || env.cube == nil {
		return
	}
	_ = env.g.r.Device.WaitIdle()
	env.g.forgetEnvironment(env)
	env.g.descriptors.Free(env.set)
	env.cube.Destroy()
	env.cube = nil
}
