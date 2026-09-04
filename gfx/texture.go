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
	altSet        vk.VkDescriptorSet // the other filtering, made on first use
	sdf           bool               // drawn through the distance-field pipeline
	nearest       bool
	repeat        bool
	external      bool // image owned elsewhere (render textures)
	data          bool // sampled without gamma decoding; pixels upload as given
	destroyed     bool // Destroy was called; the image lives until the frame retires it
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
	pix := rgba.Pix
	if !opts.Data && needsLinearPremultiply(pix) {
		// The caller's image is left as it is.
		pix = append([]byte(nil), pix...)
		linearPremultiply(pix)
	}
	return g.newTexture(b.Dx(), b.Dy(), pix, opts)
}

// needsLinearPremultiply reports whether any texel is translucent, which
// is the only case linearPremultiply changes.
func needsLinearPremultiply(pix []byte) bool {
	for i := 3; i < len(pix); i += 4 {
		if a := pix[i]; a != 0 && a != 255 {
			return true
		}
	}
	return false
}

// linearPremultiply rewrites premultiplied sRGB bytes, which is what
// image.RGBA holds, so that an sRGB sampler decodes them to linear
// premultiplied colour. Go premultiplies in sRGB space (a*c); the sampler
// decodes that to linear(a*c), which is darker than the a*linear(c) the
// blend needs, so a half-transparent white texel would read as grey.
// Each translucent texel is unpremultiplied, decoded, premultiplied in
// linear light and encoded again; opaque and clear texels are unchanged.
func linearPremultiply(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 || a == 255 {
			continue
		}
		af := float32(a) / 255
		for k := range 3 {
			straight := min(float32(pix[i+k])/af, 255)
			pix[i+k] = linearToSRGB8(srgbToLinear(uint8(straight+0.5)) * af)
		}
	}
}

func (g *Graphics) newTexture(w, h int, pix []byte, opts TextureOptions) (*Texture, error) {
	extent := vk.VkExtent2D{Width: uint32(w), Height: uint32(h)}
	format := vk.VkFormat(vk.VK_FORMAT_R8G8B8A8_SRGB)
	if opts.Data {
		format = vk.VK_FORMAT_R8G8B8A8_UNORM
	}
	mips := opts.Linear && !opts.NoMipmaps && w > 1 && h > 1
	img, err := g.uploadTexture(extent, format, pix, mips)
	if err != nil {
		return nil, err
	}
	sampler := g.sampler(opts.Linear, opts.Repeat)
	set, err := g.textureSet(img.View, sampler)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	return &Texture{Width: w, Height: h, img: img, set: set, nearest: !opts.Linear, repeat: opts.Repeat, data: opts.Data, g: g}, nil
}

// uploadTexture creates a sampled image and fills it. Inside a frame the
// copy is recorded into the frame's command buffer from the staging
// arena, before any pass, so a draw later in the same frame sees the
// pixels; outside one it goes through a one-shot submission that waits.
func (g *Graphics) uploadTexture(extent vk.VkExtent2D, format vk.VkFormat, pix []byte, mips bool) (*render.Image, error) {
	if g.frame == nil {
		return g.r.Device.NewTextureImage(extent, format, pix, mips)
	}
	img, err := g.r.Device.NewSampledImage(extent, format, mips)
	if err != nil {
		return nil, err
	}
	staging, offset, err := g.stage(pix)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	render.RecordImageUpload(g.frame.CB, img, staging, offset)
	return img, nil
}

// setFor returns the descriptor set sampling the texture with a filter:
// its own set for the default, or one made on first use for the other
// filtering.
func (t *Texture) setFor(f Filter) vk.VkDescriptorSet {
	if f == FilterDefault || (f == FilterNearest) == t.nearest {
		return t.set
	}
	if t.altSet == 0 {
		set, err := t.g.textureSet(t.img.View, t.g.sampler(f == FilterLinear, t.repeat))
		if err != nil {
			return t.set
		}
		t.altSet = set
	}
	return t.altSet
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
// texture, and rebuilds the mip chain. Inside a frame (between the
// engine's Begin and End, which is where Update and Draw run) the copy
// is recorded into the frame and costs no wait, so video and painting
// can write every frame; outside one it waits for the GPU first.
func (t *Texture) Write(x, y int, src image.Image) error {
	if t.img == nil || t.destroyed {
		return fmt.Errorf("gfx: write to a destroyed texture")
	}
	b := src.Bounds()
	r := image.Rect(x, y, x+b.Dx(), y+b.Dy()).Intersect(image.Rect(0, 0, t.Width, t.Height))
	if r.Empty() {
		return nil
	}
	rgba := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min.Add(image.Pt(r.Min.X-x, r.Min.Y-y)), draw.Src)
	if !t.data {
		linearPremultiply(rgba.Pix)
	}
	g := t.g
	if fr := g.frame; fr != nil {
		staging, offset, err := g.stage(rgba.Pix)
		if err != nil {
			return err
		}
		render.RecordImageWrite(fr.CB, t.img, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), staging, offset)
		return nil
	}
	if err := g.r.Device.WaitIdle(); err != nil {
		return err
	}
	return g.r.Device.WriteImage(t.img, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), rgba.Pix)
}

// Read copies the texture's pixels back from the GPU, premultiplied as
// they are stored. It waits for the GPU first; use it for screenshots of
// render textures and tests, not per frame.
func (t *Texture) Read() (*image.RGBA, error) {
	if t.img == nil || t.destroyed {
		return nil, fmt.Errorf("gfx: read from a destroyed texture")
	}
	if err := t.g.r.Device.WaitIdle(); err != nil {
		return nil, err
	}
	return t.g.r.Device.ReadImage(t.img)
}

// Destroy frees the texture. Called inside a frame it costs no wait: the
// image and its descriptor sets go on the frame slot's retire list and
// are freed once that frame has finished, so sprites and meshes already
// queued this frame still draw with it.
func (t *Texture) Destroy() {
	if t.img == nil || t.destroyed {
		return
	}
	t.destroyed = true
	g := t.g
	// The cached material sets that name this texture leave the cache
	// now, so no later frame can bind one; freeing them is deferred with
	// everything else.
	g.forgetTexture(t)
	g.deferDestroy(func() {
		g.descriptors.Free(t.set)
		if t.altSet != 0 {
			g.descriptors.Free(t.altSet)
			t.altSet = 0
		}
		if !t.external {
			t.img.Destroy()
		}
		t.img, t.set = nil, 0
	})
}
