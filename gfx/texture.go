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
	repeat        bool
	external      bool // image owned elsewhere (render textures)
	g             *Graphics
}

// TextureOptions selects sampling and colour handling.
type TextureOptions struct {
	Linear bool // bilinear filtering; the default is nearest, for pixel art
	// Data marks pixels that are not sRGB colour (masks, glyph coverage,
	// lookup tables); they are sampled without gamma decoding.
	Data bool
	// NoMipmaps keeps a single level; linear textures get a full mip chain
	// by default so distant surfaces do not shimmer.
	NoMipmaps bool
	// Repeat tiles the texture instead of clamping at the edges.
	Repeat bool
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
	mips := opts.Linear && !opts.NoMipmaps && w > 1 && h > 1
	img, err := g.r.Device.NewTextureImage(extent, format, pix, mips)
	if err != nil {
		return nil, err
	}
	sampler := g.sampler(opts.Linear, opts.Repeat)
	set, err := g.textureSet(img.View, sampler)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	return &Texture{Width: w, Height: h, img: img, set: set, nearest: !opts.Linear, repeat: opts.Repeat, g: g}, nil
}

// NewBlankTexture makes a transparent texture of a size, to be filled by
// Write: a canvas to paint on, a video frame, a procedural map.
func (g *Graphics) NewBlankTexture(width, height int, opts TextureOptions) (*Texture, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("gfx: blank texture needs a positive size")
	}
	return g.newTexture(width, height, make([]byte, width*height*4), opts)
}

// Write replaces the pixels under src placed at (x, y), clipped to the
// texture, and rebuilds the mip chain. It waits for the GPU to finish
// with the texture first, so keep it to loading screens and occasional
// updates rather than every frame.
func (t *Texture) Write(x, y int, src image.Image) error {
	if t.img == nil {
		return fmt.Errorf("gfx: write to a destroyed texture")
	}
	b := src.Bounds()
	r := image.Rect(x, y, x+b.Dx(), y+b.Dy()).Intersect(image.Rect(0, 0, t.Width, t.Height))
	if r.Empty() {
		return nil
	}
	rgba := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min.Add(image.Pt(r.Min.X-x, r.Min.Y-y)), draw.Src)
	if err := t.g.r.Device.WaitIdle(); err != nil {
		return err
	}
	return t.g.r.Device.WriteImage(t.img, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), rgba.Pix)
}

// Read copies the texture's pixels back from the GPU, premultiplied as
// they are stored. It waits for the GPU first; use it for screenshots of
// render textures and tests, not per frame.
func (t *Texture) Read() (*image.RGBA, error) {
	if t.img == nil {
		return nil, fmt.Errorf("gfx: read from a destroyed texture")
	}
	if err := t.g.r.Device.WaitIdle(); err != nil {
		return nil, err
	}
	return t.g.r.Device.ReadImage(t.img)
}

// Destroy frees the texture. It must not be in use by a frame in flight.
func (t *Texture) Destroy() {
	if t.img == nil {
		return
	}
	_ = t.g.r.Device.WaitIdle()
	t.g.forgetTexture(t)
	t.g.descriptors.Free(t.set)
	if !t.external {
		t.img.Destroy()
	}
	t.img = nil
}
