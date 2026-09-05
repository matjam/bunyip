package gfx

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// Texture is an image on the GPU, sampled by sprites, materials and
// shaders. Create it through Graphics; its zero value is not drawable.
// Width and Height are pixel dimensions maintained by the engine and
// must not be assigned by callers. Destroy releases owned GPU storage.
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
	mipmapped     bool // a full mip chain was asked for, so Replace makes one again
	compressed    bool // block-compressed on the GPU, so texels cannot be written into it
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

// NewTexture uploads an image without modifying it. Colour pixels are
// premultiplied in linear light and stored as sRGB, so shaders sample
// premultiplied linear colour. Data textures skip this colour conversion.
// Empty bounds return an error; src must be non-nil. During Draw, the
// upload is recorded before rendering; outside a frame it waits for the GPU.
func (g *Graphics) NewTexture(src image.Image, opts TextureOptions) (*Texture, error) {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("gfx: texture has empty bounds %v", b)
	}
	t, err := g.newTexture(b.Dx(), b.Dy(), texelsOf(src, opts.Data), opts)
	if err == nil {
		g.trackTexture(t, opts)
	}
	return t, err
}

// asRGBA returns src as a tightly packed RGBA image, converting it when
// it is not one already. The caller's image is never written to.
func asRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	rgba, ok := src.(*image.RGBA)
	if !ok || rgba.Stride != b.Dx()*4 {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}
	return rgba
}

// texelsOf returns the bytes to upload for an image: RGBA as it stands
// for data textures, and premultiplied in linear light for colour ones.
func texelsOf(src image.Image, data bool) []byte {
	rgba := asRGBA(src)
	pix := rgba.Pix
	if !data && needsLinearPremultiply(pix) {
		// The caller's image is left as it is.
		pix = append([]byte(nil), pix...)
		linearPremultiply(pix)
	}
	return pix
}

// trackTexture records a texture in the live resource list.
func (g *Graphics) trackTexture(t *Texture, opts TextureOptions) {
	mips := opts.Linear && !opts.NoMipmaps && t.Width > 1 && t.Height > 1
	g.track(t, Resource{Kind: ResourceTexture, Width: t.Width, Height: t.Height,
		Bytes: textureBytes(t.Width, t.Height, mips)})
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
	return &Texture{Width: w, Height: h, img: img, set: set, nearest: !opts.Linear, repeat: opts.Repeat,
		data: opts.Data, mipmapped: opts.Linear && !opts.NoMipmaps, g: g}, nil
}

// Replace swaps the texture's pixels for another image, keeping the
// *Texture the game holds valid: every sprite, material and shader slot
// that names it draws the new image without being told. Use it to
// reload a texture whose file changed on disk; asset.Reloader calls it
// for you. The image may be a different size, and the filtering, edge
// handling, colour handling and mip choice the texture was made with
// are kept. Inside a frame it costs no wait, and the old image is freed
// once the frames that may still draw from it have finished. A render
// texture's image belongs to the render texture, so replacing one is an
// error.
func (t *Texture) Replace(src image.Image) error {
	if t.img == nil || t.destroyed {
		return fmt.Errorf("gfx: replace a destroyed texture")
	}
	if t.external {
		return fmt.Errorf("gfx: a render texture's image cannot be replaced")
	}
	if t.compressed {
		return fmt.Errorf("gfx: a compressed texture takes a KTX2 file; use ReplaceCompressed")
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return fmt.Errorf("gfx: texture has empty bounds %v", b)
	}
	if b.Dx() == t.Width && b.Dy() == t.Height {
		// The same size keeps the image, so nothing that names the
		// texture has to be rebuilt.
		return t.Write(0, 0, src)
	}
	return t.replaceTexels(b.Dx(), b.Dy(), texelsOf(src, t.data), t.format())
}

// format is the image format the texture's colour handling asks for.
func (t *Texture) format() vk.VkFormat {
	if t.data {
		return vk.VK_FORMAT_R8G8B8A8_UNORM
	}
	return vk.VK_FORMAT_R8G8B8A8_SRGB
}

// replaceTexels gives the texture a fresh image of a new size from
// uncompressed texels.
func (t *Texture) replaceTexels(w, h int, pix []byte, format vk.VkFormat) error {
	mips := t.mipmapped && w > 1 && h > 1
	img, err := t.g.uploadTexture(vk.VkExtent2D{Width: uint32(w), Height: uint32(h)}, format, pix, mips)
	if err != nil {
		return err
	}
	return t.swapImage(img, w, h, textureBytes(w, h, mips))
}

// swapImage puts a freshly built image behind the texture and retires
// the old one. The descriptor sets that named the old image go with it,
// so the next draw builds them again from the new view, and the old
// image lives until the frames that may still sample it have finished.
func (t *Texture) swapImage(img *render.Image, w, h, bytes int) error {
	g := t.g
	set, err := g.textureSet(img.View, g.sampler(!t.nearest, t.repeat))
	if err != nil {
		img.Destroy()
		return err
	}
	old, oldSet, oldAlt := t.img, t.set, t.altSet
	// The cached material and image sets name the old view, so they leave
	// the caches now; freeing them is deferred with everything else.
	g.forgetTexture(t)
	t.img, t.set, t.altSet = img, set, 0
	t.Width, t.Height = w, h
	g.track(t, Resource{Kind: ResourceTexture, Width: w, Height: h, Bytes: bytes})
	g.deferDestroy(func() {
		g.descriptors.Free(oldSet)
		if oldAlt != 0 {
			g.descriptors.Free(oldAlt)
		}
		old.Destroy()
	})
	return nil
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
	t, err := g.newTexture(width, height, make([]byte, width*height*4), opts)
	if err == nil {
		g.trackTexture(t, opts)
	}
	return t, err
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
	if t.compressed {
		return fmt.Errorf("gfx: a compressed texture holds blocks, not texels, so it cannot be written into")
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
// render textures and tests, not per frame. A compressed texture holds
// blocks rather than texels, so reading one is an error; decode its
// KTX2 file with gfx/ktx2 instead.
func (t *Texture) Read() (*image.RGBA, error) {
	if t.img == nil || t.destroyed {
		return nil, fmt.Errorf("gfx: read from a destroyed texture")
	}
	if t.compressed {
		return nil, fmt.Errorf("gfx: a compressed texture holds blocks, not texels; decode the KTX2 file with gfx/ktx2 instead")
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
	g.forget(t)
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
