package ktx2

import "image"

// BC4 packs one channel of a 4 by 4 block into eight bytes: two 8-bit
// endpoints and sixteen three-bit indices. With the first endpoint above
// the second the palette is the two endpoints and six interpolants; with
// it below, four interpolants plus a hard zero and a hard one. BC5 is
// two BC4 blocks, red then green, and BC3's alpha half is a BC4 block on
// the alpha channel.
//
// The endpoints are the channel's highest and lowest value in the block,
// so both are reproduced exactly and a block of one or two values is
// lossless. Only the values between them are approximated.

// bc4Palette is the eight values a pair of endpoints stands for.
func bc4Palette(e0, e1 uint8) [8]uint8 {
	var p [8]uint8
	p[0], p[1] = e0, e1
	a, b := int(e0), int(e1)
	if e0 > e1 {
		for i := 2; i < 8; i++ {
			p[i] = uint8(((8-i)*a + (i-1)*b) / 7)
		}
		return p
	}
	for i := 2; i < 6; i++ {
		p[i] = uint8(((6-i)*a + (i-1)*b) / 5)
	}
	p[6], p[7] = 0, 255
	return p
}

// encodeBC4Block writes one channel of a block. channel picks which of
// the four a texel carries.
func encodeBC4Block(b block, channel int, out []byte) {
	lo, hi := b[0][channel], b[0][channel]
	for _, t := range b {
		lo, hi = min(lo, t[channel]), max(hi, t[channel])
	}
	// The eight-value mode needs the first endpoint above the second; a
	// block of one value takes the six-value mode, whose index 0 is that
	// value exactly.
	e0, e1 := hi, lo
	p := bc4Palette(e0, e1)
	out[0], out[1] = e0, e1
	var bits uint64
	for i, t := range b {
		v := int(t[channel])
		best, bestErr := 0, 1<<30
		for k := range 8 {
			d := int(p[k]) - v
			if d < 0 {
				d = -d
			}
			if d < bestErr {
				best, bestErr = k, d
			}
		}
		bits |= uint64(best) << (3 * i)
	}
	for i := range 6 {
		out[2+i] = byte(bits >> (8 * i))
	}
}

// decodeBC4Block reads one channel of a block and returns its sixteen
// values.
func decodeBC4Block(in []byte) [16]uint8 {
	p := bc4Palette(in[0], in[1])
	var bits uint64
	for i := range 6 {
		bits |= uint64(in[2+i]) << (8 * i)
	}
	var out [16]uint8
	for i := range 16 {
		out[i] = p[bits>>(3*i)&7]
	}
	return out
}

// encodeBC4 compresses one channel of an image.
func encodeBC4(src *image.RGBA, channel int) []byte {
	return encodeBlocks(src, 8, func(b block, out []byte) { encodeBC4Block(b, channel, out) })
}

// decodeBC4 expands a BC4 image, putting the one channel in red and
// leaving green and blue at zero so a reader sees the mask it stored.
func decodeBC4(data []byte, w, h int) (*image.RGBA, error) {
	img, err := decodeBlocks(data, w, h, 8, func(in []byte) block {
		vals := decodeBC4Block(in)
		var b block
		for i := range 16 {
			b[i] = texel{vals[i], 0, 0, 255}
		}
		return b
	})
	return img, err
}

// encodeBC5 compresses the red and green channels into sixteen bytes a
// block, red first.
func encodeBC5(src *image.RGBA) []byte {
	return encodeBlocks(src, 16, func(b block, out []byte) {
		encodeBC4Block(b, 0, out[0:8])
		encodeBC4Block(b, 1, out[8:16])
	})
}

// decodeBC5 expands a BC5 image into red and green, leaving blue at
// zero and alpha opaque.
func decodeBC5(data []byte, w, h int) (*image.RGBA, error) {
	img, err := decodeBlocks(data, w, h, 16, func(in []byte) block {
		r := decodeBC4Block(in[0:8])
		g := decodeBC4Block(in[8:16])
		var b block
		for i := range 16 {
			b[i] = texel{r[i], g[i], 0, 255}
		}
		return b
	})
	return img, err
}

// encodeBC3 compresses colour and alpha: a BC4 block on alpha, then a
// BC1 colour block.
func encodeBC3(src *image.RGBA) []byte {
	return encodeBlocks(src, 16, func(b block, out []byte) {
		encodeBC4Block(b, 3, out[0:8])
		encodeBC1Block(b, out[8:16])
	})
}

// decodeBC3 expands a BC3 image. Its colour half is always in the
// four-colour mode, whatever the endpoints say, which is what the format
// requires.
func decodeBC3(data []byte, w, h int) (*image.RGBA, error) {
	img, err := decodeBlocks(data, w, h, 16, func(in []byte) block {
		alpha := decodeBC4Block(in[0:8])
		b := decodeBC1Block(in[8:16], bc1Always)
		for i := range 16 {
			b[i][3] = alpha[i]
		}
		return b
	})
	return img, err
}
