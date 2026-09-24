package gfx

import (
	"fmt"

	"github.com/matjam/bunyip/gfx/ktx2"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// NewCompressedTexture uploads a KTX2 file written by bunyip-tex. Its
// blocks and its mip levels go to the GPU as they stand, so a game pays
// nothing at load time for compression or for mip generation, and the
// texture takes a quarter to an eighth of the memory an uncompressed one
// would.
//
// The file's format decides whether sampling decodes from sRGB, so
// TextureOptions.Data is ignored; Linear and Repeat choose the sampler
// as usual, and NoMipmaps uploads level 0 alone. Where the device cannot
// sample the format, which some MoltenVK configurations cannot for the
// BC formats, level zero is decoded on the processor into a plain RGBA
// texture and any requested mipmaps are generated at load. This fallback
// supports the formats and BC7 modes the ktx2 CPU decoder implements;
// unsupported formats such as ASTC return an error on such a device.
func (g *Graphics) NewCompressedTexture(data []byte, opts TextureOptions) (*Texture, error) {
	f, err := ktx2.Parse(data)
	if err != nil {
		return nil, err
	}
	if !g.supportsKTX2(f) {
		return g.decodeCompressed(f, opts)
	}
	t := &Texture{
		Width: f.Width, Height: f.Height, nearest: !opts.Linear, repeat: opts.Repeat,
		data: !f.Format.SRGB(), compressed: true, g: g,
	}
	img, levels, err := g.uploadKTX2(f, opts)
	if err != nil {
		return nil, err
	}
	t.mipmapped = levels > 1
	set, err := g.textureSet(img.View, g.sampler(opts.Linear, opts.Repeat))
	if err != nil {
		img.Destroy()
		return nil, err
	}
	t.img, t.set = img, set
	g.track(t, Resource{Kind: ResourceTexture, Width: f.Width, Height: f.Height, Bytes: ktxBytes(f, levels)})
	return t, nil
}

// ReplaceCompressed swaps a compressed texture's blocks for those of
// another KTX2 file, keeping the *Texture the game holds, the way
// Replace does for an image. asset.Reloader calls it when a .ktx2 file
// changes on disk.
func (t *Texture) ReplaceCompressed(data []byte) error {
	if t.img == nil || t.destroyed {
		return fmt.Errorf("gfx: replace a destroyed texture")
	}
	if t.external {
		return fmt.Errorf("gfx: a render texture's image cannot be replaced")
	}
	f, err := ktx2.Parse(data)
	if err != nil {
		return err
	}
	g := t.g
	opts := TextureOptions{Linear: !t.nearest, Repeat: t.repeat, NoMipmaps: !t.mipmapped}
	if !g.supportsKTX2(f) {
		// The device decodes on the processor, so the texture behind the
		// pointer holds plain texels whether or not it did before.
		img, err := f.DecodeLevel(0)
		if err != nil {
			return err
		}
		t.compressed, t.data = false, !f.Format.SRGB()
		return t.replaceTexels(img.Rect.Dx(), img.Rect.Dy(), img.Pix, t.format())
	}
	newImg, levels, err := g.uploadKTX2(f, opts)
	if err != nil {
		return err
	}
	t.compressed, t.data, t.mipmapped = true, !f.Format.SRGB(), levels > 1
	return t.swapImage(newImg, f.Width, f.Height, ktxBytes(f, levels))
}

// supportsKTX2 reports whether the device can sample the file's format.
func (g *Graphics) supportsKTX2(f *ktx2.File) bool {
	return g.r.Device.SupportsFormat(vk.VkFormat(f.Format))
}

// uploadKTX2 creates the image and fills every level it should carry.
// Inside a frame the copies are recorded into the frame's command
// buffer from the staging arena, before any pass, so a draw later in the
// same frame samples them; outside one they go into the device's upload
// batch, which is submitted ahead of the next frame and costs no wait.
// Either way the levels are written straight into the staging memory.
func (g *Graphics) uploadKTX2(f *ktx2.File, opts TextureOptions) (*render.Image, int, error) {
	n := len(f.Levels)
	if opts.NoMipmaps {
		n = 1
	}
	// The levels are packed into one staging allocation, each starting on
	// a multiple of the block size the driver needs for a copy offset.
	const align = 16
	var size vk.VkDeviceSize
	levels := make([]render.LevelCopy, n)
	for i := range n {
		size = (size + align - 1) / align * align
		w, h := f.LevelSize(i)
		levels[i] = render.LevelCopy{Offset: size, Width: uint32(w), Height: uint32(h)}
		size += vk.VkDeviceSize(len(f.Levels[i]))
	}
	if size == 0 {
		return nil, 0, fmt.Errorf("gfx: KTX2 file has no texel data")
	}
	extent := vk.VkExtent2D{Width: uint32(f.Width), Height: uint32(f.Height)}
	img, err := g.r.Device.NewLevelledImage(extent, vk.VkFormat(f.Format), uint32(n))
	if err != nil {
		return nil, 0, err
	}
	var (
		staging *render.Buffer
		offset  vk.VkDeviceSize
		dst     []byte
		cb      vk.VkCommandBuffer
	)
	if fr := g.frame; fr != nil {
		staging, offset, dst, err = g.staging.Reserve(fr.Slot, size)
		cb = fr.CB
	} else if staging, offset, dst, err = g.r.Device.StageUpload(size); err == nil {
		cb, err = g.r.Device.UploadCommands()
	}
	if err != nil {
		img.Destroy()
		return nil, 0, err
	}
	for i := range levels {
		lv := &levels[i]
		// The gaps between levels are padding; clear them so the
		// staging holds no stale bytes.
		end := vk.VkDeviceSize(len(dst))
		if i+1 < len(levels) {
			end = levels[i+1].Offset
		}
		k := copy(dst[lv.Offset:], f.Levels[i])
		clear(dst[lv.Offset+vk.VkDeviceSize(k) : end])
		// The arena's own alignment is a multiple of the block size, so
		// shifting every level by the allocation's offset keeps them
		// aligned.
		lv.Offset += offset
	}
	render.RecordLevelsUpload(cb, img, staging, levels)
	if g.frame == nil {
		img.NoteUpload()
	}
	return img, n, nil
}

// decodeCompressed reads a KTX2 file on the processor and uploads it as
// a plain RGBA texture, for a device that cannot sample the format. Only
// level 0 is decoded; the mip chain is rebuilt by the driver when the
// texture asks for one, which is what an uncompressed texture does
// anyway.
func (g *Graphics) decodeCompressed(f *ktx2.File, opts TextureOptions) (*Texture, error) {
	if !f.Decodable() {
		return nil, fmt.Errorf("gfx: the device cannot sample %s and this build cannot decode it", f.Format)
	}
	img, err := f.DecodeLevel(0)
	if err != nil {
		return nil, err
	}
	opts.Data = !f.Format.SRGB()
	// The file's texels are already premultiplied, so they upload as they
	// stand whichever colour handling the format asks for.
	t, err := g.newTexture(img.Rect.Dx(), img.Rect.Dy(), img.Pix, opts)
	if err != nil {
		return nil, err
	}
	g.trackTexture(t, opts)
	return t, nil
}

// ktxBytes estimates the GPU memory a compressed texture holds: the
// levels it uploaded, at the format's own rate.
func ktxBytes(f *ktx2.File, levels int) int {
	total := 0
	for i := range levels {
		w, h := f.LevelSize(i)
		total += f.Format.LevelBytes(w, h)
	}
	return total
}
