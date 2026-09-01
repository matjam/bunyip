package gfx

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// Texture is an image on the GPU, sampled by sprites.
type Texture struct {
	Width, Height int
	img           *render.Image
	set           vk.VkDescriptorSet
	sdf           bool // drawn through the distance-field pipeline
	nearest       bool
	g             *Graphics
}

// TextureOptions selects sampling and colour handling.
type TextureOptions struct {
	Linear bool // bilinear filtering; the default is nearest, for pixel art
	// Data marks pixels that are not sRGB colour (masks, glyph coverage,
	// lookup tables); they are sampled without gamma decoding.
	Data bool
}

// NewTexture uploads an image. Pixels are converted to premultiplied RGBA
// and treated as sRGB, so shaders see linear light.
func (g *Graphics) NewTexture(src image.Image, opts TextureOptions) (*Texture, error) {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("gfx: texture has empty bounds %v", b)
	}
	rgba, ok := src.(*image.RGBA)
	if !ok || rgba.Stride != b.Dx()*4 {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}
	return g.newTexture(b.Dx(), b.Dy(), rgba.Pix, opts)
}

func (g *Graphics) newTexture(w, h int, pix []byte, opts TextureOptions) (*Texture, error) {
	extent := vk.VkExtent2D{Width: uint32(w), Height: uint32(h)}
	format := vk.VkFormat(vk.VK_FORMAT_R8G8B8A8_SRGB)
	if opts.Data {
		format = vk.VK_FORMAT_R8G8B8A8_UNORM
	}
	img, err := g.R.Device.NewTextureImage(extent, format, pix)
	if err != nil {
		return nil, err
	}
	sampler := g.nearest
	if opts.Linear {
		sampler = g.linear
	}
	set, err := g.descriptors.Allocate(img.View, sampler)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	return &Texture{Width: w, Height: h, img: img, set: set, nearest: !opts.Linear, g: g}, nil
}

// Destroy frees the texture. It must not be in use by a frame in flight.
func (t *Texture) Destroy() {
	if t.img == nil {
		return
	}
	_ = t.g.R.Device.WaitIdle()
	t.g.forgetTexture(t)
	t.g.descriptors.Free(t.set)
	t.img.Destroy()
	t.img = nil
}
