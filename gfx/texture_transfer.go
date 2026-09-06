package gfx

import (
	"fmt"
	"image"
	"io"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// WritePNG reads the completed texture and writes PNG to a borrowed writer.
// It has the same format and frame-timing restrictions as Read.
func (t *Texture) WritePNG(w io.Writer) error {
	if w == nil {
		return fmt.Errorf("gfx: nil PNG writer")
	}
	img, err := t.Read()
	if err != nil {
		return err
	}
	return writePNG(w, img)
}

// SavePNG reads the completed texture, then creates or truncates path and closes
// the output file. It has the same format and frame-timing restrictions as Read.
func (t *Texture) SavePNG(path string) error {
	img, err := t.Read()
	if err != nil {
		return err
	}
	return savePNG(path, img)
}

// CopyFrom copies srcRect from src to dst on this texture's GPU image, preserving
// the stored bytes and regenerating this texture's mip chain. A zero rectangle
// selects the whole source. Both regions must fit; there is no clipping, scaling,
// blending or colour conversion. Source and destination need identical formats
// and Graphics ownership. Compressed textures, destroyed textures, overlapping
// self-copies and render-texture destinations are rejected.
//
// Within a frame this records before all queued drawing, like Write. A source
// render texture already queued through DrawTo is rejected because its new
// pixels will not exist until the frame renders. Otherwise it copies the last
// completed source image, plus any preceding texture uploads in the frame.
// Outside a frame it waits for the GPU. Non-overlapping self-copies are supported.
func (t *Texture) CopyFrom(src *Texture, srcRect image.Rectangle, dst image.Point) error {
	if t == nil || src == nil || t.img == nil || src.img == nil || t.destroyed || src.destroyed {
		return fmt.Errorf("gfx: texture copy needs live source and destination")
	}
	if t.g != src.g {
		return fmt.Errorf("gfx: texture copy needs the same Graphics owner")
	}
	if t.external {
		return fmt.Errorf("gfx: cannot copy into a render texture; use DrawTo")
	}
	if t.compressed || src.compressed {
		return fmt.Errorf("gfx: cannot copy compressed texture regions")
	}
	if t.img.Format != src.img.Format {
		return fmt.Errorf("gfx: texture copy formats differ")
	}
	if srcRect == (image.Rectangle{}) {
		srcRect = image.Rect(0, 0, src.Width, src.Height)
	}
	if srcRect.Empty() || !srcRect.In(image.Rect(0, 0, src.Width, src.Height)) {
		return fmt.Errorf("gfx: texture copy source region is out of bounds")
	}
	if dst.X < 0 || dst.Y < 0 || dst.X > t.Width || dst.Y > t.Height || srcRect.Dx() > t.Width-dst.X || srcRect.Dy() > t.Height-dst.Y {
		return fmt.Errorf("gfx: texture copy destination region is out of bounds")
	}
	destRect := image.Rectangle{Min: dst, Max: dst.Add(srcRect.Size())}
	if t.img == src.img && srcRect.Overlaps(destRect) {
		return fmt.Errorf("gfx: texture self-copy regions overlap")
	}
	g := t.g
	if g.frame != nil && src.external {
		for _, sf := range g.subFrames {
			if sf.rt.tex != nil && sf.rt.tex.img == src.img {
				return fmt.Errorf("gfx: cannot copy a render texture queued in the current frame")
			}
		}
	}
	if g.frame == nil {
		if err := g.r.Device.WaitIdle(); err != nil {
			return err
		}
	}
	return g.setup(func(cb vk.VkCommandBuffer) { render.RecordImageCopy(cb, src.img, t.img, srcRect, dst) })
}
