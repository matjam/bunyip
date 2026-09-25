package gfx

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// prefilterReference is the prefilter as it was before the GGX samples
// were mapped once per level, kept to check the faster one against.
func prefilterReference(sample radianceSampler, n lin.Vec3, roughness float32) (r, g, b float32) {
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
		e1 := (float32(i) + 0.5) / samples
		e2 := radicalInverse(uint32(i))
		phi := 2 * math.Pi * e1
		cosTheta := float32(math.Sqrt(float64((1 - e2) / (1 + (a*a-1)*e2))))
		sinTheta := float32(math.Sqrt(float64(1 - cosTheta*cosTheta)))
		h := tx.Mul(sinTheta * cos32(phi)).Add(ty.Mul(sinTheta * sin32(phi))).Add(n.Mul(cosTheta))
		l := h.Mul(2 * n.Dot(h)).Sub(n)
		nl := n.Dot(l)
		if nl <= 0 {
			continue
		}
		sr, sg, sb := sample(l)
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

// prefilterCubeReference fills the cube one texel after another, the way
// newEnvironmentFrom did before it shared the rows out.
func prefilterCubeReference(sample radianceSampler, size, mips int) [][6][]byte {
	faces := make([][6][]byte, mips)
	for level := range mips {
		side := max(size>>level, 1)
		roughness := float32(level) / float32(max(mips-1, 1))
		for face := range 6 {
			pix := make([]byte, side*side*8)
			for y := range side {
				for x := range side {
					u := 2*(float32(x)+0.5)/float32(side) - 1
					v := 2*(float32(y)+0.5)/float32(side) - 1
					n := cubeDir(face, u, v).Norm()
					var r, gg, b float32
					if level == 0 {
						r, gg, b = sample(n)
					} else {
						r, gg, b = prefilterReference(sample, n, roughness)
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
	return faces
}

// TestPrefilterCubeMatchesReference prefilters an HDR panorama of noise
// and hot spots with the parallel prefilter and the serial reference and
// requires the same bytes in every level of every face.
func TestPrefilterCubeMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	src := newRadianceMap(256, 128)
	for i := range src.pix {
		src.pix[i] = rng.Float32() * 2
		if rng.IntN(500) == 0 {
			src.pix[i] = 200 // a sun or a lamp
		}
	}
	for _, size := range []int{8, 32, 64} {
		mips := envLevels(size)
		got := prefilterCube(src.sample, size, mips)
		want := prefilterCubeReference(src.sample, size, mips)
		for level := range mips {
			for face := range 6 {
				g, w := got[level][face], want[level][face]
				for i := range w {
					if g[i] != w[i] {
						t.Fatalf("size %d level %d face %d byte %d is %d, want %d", size, level, face, i, g[i], w[i])
					}
				}
			}
		}
	}
}

// customImage is an image type panoramaRadiance has no fast path for.
type customImage struct{ *image.RGBA }

// TestPanoramaRadianceFastPaths converts panoramas of every image type
// with a fast path and checks each texel is what At gives.
func TestPanoramaRadianceFastPaths(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	const w, h = 37, 19
	rect := image.Rect(3, 5, 3+w, 5+h)
	rgba := image.NewRGBA(rect)
	nrgba := image.NewNRGBA(rect)
	rgba64 := image.NewRGBA64(rect)
	gray := image.NewGray(rect)
	pal := image.NewPaletted(rect, color.Palette{color.RGBA{1, 2, 3, 255}, color.NRGBA{200, 100, 50, 128}, color.Gray{77}})
	ycc := image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			v := rng.Uint64()
			a := uint8(v >> 24)
			rgba.SetRGBA(x, y, color.RGBA{min(uint8(v), a), min(uint8(v>>8), a), min(uint8(v>>16), a), a})
			nrgba.SetNRGBA(x, y, color.NRGBA{uint8(v), uint8(v >> 8), uint8(v >> 16), a})
			rgba64.SetRGBA64(x, y, color.RGBA64{uint16(v), uint16(v >> 16), uint16(v >> 32), 0xffff})
			gray.SetGray(x, y, color.Gray{uint8(v >> 40)})
			pal.SetColorIndex(x, y, uint8(v%3))
		}
	}
	for i := range ycc.Y {
		ycc.Y[i] = uint8(rng.Uint32())
	}
	for i := range ycc.Cb {
		ycc.Cb[i], ycc.Cr[i] = uint8(rng.Uint32()), uint8(rng.Uint32())
	}
	// A sub-image starts part way into its parent's pixels.
	sub := rgba.SubImage(image.Rect(10, 8, 30, 20)).(*image.RGBA)
	for name, img := range map[string]image.Image{
		"rgba": rgba, "nrgba": nrgba, "rgba64": rgba64, "gray": gray, "paletted": pal,
		"ycbcr": ycc, "sub-image": sub, "other": customImage{rgba},
	} {
		got := panoramaRadiance(img)
		b := img.Bounds()
		for y := range b.Dy() {
			for x := range b.Dx() {
				r, g, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				want := [3]float32{srgbToLinear(uint8(r >> 8)), srgbToLinear(uint8(g >> 8)), srgbToLinear(uint8(bb >> 8))}
				i := (y*b.Dx() + x) * 3
				if [3]float32(got.pix[i:i+3]) != want {
					t.Fatalf("%s: texel (%d, %d) is %v, want %v", name, x, y, got.pix[i:i+3], want)
				}
			}
		}
	}
}
