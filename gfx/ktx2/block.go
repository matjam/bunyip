package ktx2

import "image"

// texel is one RGBA texel as the encoders see it, already premultiplied
// where the format holds colour.
type texel [4]uint8

// block is the sixteen texels of one 4 by 4 block, in raster order. A
// block that runs past the edge of the image repeats the last row and
// column, so the padding matches its neighbours and costs the encoder
// nothing.
type block [16]texel

// blockAt gathers the block whose top-left texel is at (bx, by).
func blockAt(src *image.RGBA, bx, by int) block {
	var b block
	w, h := src.Rect.Dx(), src.Rect.Dy()
	for y := range 4 {
		sy := min(by+y, h-1)
		for x := range 4 {
			sx := min(bx+x, w-1)
			at := sy*src.Stride + sx*4
			b[y*4+x] = texel{src.Pix[at], src.Pix[at+1], src.Pix[at+2], src.Pix[at+3]}
		}
	}
	return b
}

// putBlock writes a decoded block back into an image, clipped to it.
func putBlock(dst *image.RGBA, bx, by int, b block) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := range 4 {
		dy := by + y
		if dy >= h {
			break
		}
		for x := range 4 {
			dx := bx + x
			if dx >= w {
				break
			}
			at := dy*dst.Stride + dx*4
			t := b[y*4+x]
			dst.Pix[at], dst.Pix[at+1], dst.Pix[at+2], dst.Pix[at+3] = t[0], t[1], t[2], t[3]
		}
	}
}

// encodeBlocks walks an image block by block and hands each to encode,
// which writes one block's bytes into out.
func encodeBlocks(src *image.RGBA, blockBytes int, encode func(b block, out []byte)) []byte {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	bx, by := (w+3)/4, (h+3)/4
	out := make([]byte, bx*by*blockBytes)
	at := 0
	for y := range by {
		for x := range bx {
			encode(blockAt(src, x*4, y*4), out[at:at+blockBytes])
			at += blockBytes
		}
	}
	return out
}

// decodeBlocks walks a format's blocks and hands each to decode, which
// returns the sixteen texels it holds.
func decodeBlocks(data []byte, w, h, blockBytes int, decode func(in []byte) block) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	bx, by := (w+3)/4, (h+3)/4
	if len(data) < bx*by*blockBytes {
		return nil, errShort(len(data), bx*by*blockBytes)
	}
	at := 0
	for y := range by {
		for x := range bx {
			putBlock(dst, x*4, y*4, decode(data[at:at+blockBytes]))
			at += blockBytes
		}
	}
	return dst, nil
}

// clamp8 rounds a float to a byte.
func clamp8(v float64) uint8 {
	return uint8(min(max(v+0.5, 0), 255))
}

// sqDist is the squared distance between two colours over the channels
// a format carries.
func sqDist(a, b texel, channels int) int {
	sum := 0
	for i := range channels {
		d := int(a[i]) - int(b[i])
		sum += d * d
	}
	return sum
}
