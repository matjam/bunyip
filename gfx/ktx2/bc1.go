package ktx2

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
)

// errShort names a buffer that is too small for the blocks it should
// hold.
func errShort(got, want int) error {
	return fmt.Errorf("ktx2: %d bytes of block data, want %d", got, want)
}

// BC1 packs a 4 by 4 block into eight bytes: two RGB565 endpoints and
// sixteen two-bit indices along the line between them. This encoder
// always writes the four-colour mode, so every texel decodes opaque and
// the one bit of alpha the format can carry is not used; a texture with
// soft edges wants BC3 or BC7 instead.

// rgb565 packs an 8-bit colour into the 16-bit word BC1 stores.
func rgb565(r, g, b uint8) uint16 {
	return uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
}

// expand565 unpacks a stored endpoint the way the hardware does,
// replicating the top bits into the low ones so white stays white.
func expand565(c uint16) texel {
	r := uint8(c >> 11 & 0x1F)
	g := uint8(c >> 5 & 0x3F)
	b := uint8(c & 0x1F)
	return texel{r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2, 255}
}

// bc1Mode says how a colour block's endpoints choose its palette, which
// differs between the formats that carry one.
type bc1Mode uint8

const (
	// bc1Punchthrough is BC1's RGBA forms: endpoints in the other order
	// give three colours and a transparent fourth entry.
	bc1Punchthrough bc1Mode = iota
	// bc1Opaque is BC1's RGB forms, where that fourth entry is black.
	bc1Opaque
	// bc1Always is the colour half of BC2 and BC3, which is always in
	// the four-colour mode whatever the endpoints say.
	bc1Always
)

// bc1Palette is the four colours a pair of endpoints stands for. With c0
// above c1 the two interpolants sit a third and two thirds along the
// line; otherwise one sits halfway and the fourth entry is fixed.
func bc1Palette(c0, c1 uint16, mode bc1Mode) [4]texel {
	var p [4]texel
	p[0], p[1] = expand565(c0), expand565(c1)
	if c0 > c1 || mode == bc1Always {
		for i := range 3 {
			p[2][i] = uint8((2*int(p[0][i]) + int(p[1][i])) / 3)
			p[3][i] = uint8((int(p[0][i]) + 2*int(p[1][i])) / 3)
		}
		p[2][3], p[3][3] = 255, 255
		return p
	}
	for i := range 3 {
		p[2][i] = uint8((int(p[0][i]) + int(p[1][i])) / 2)
	}
	p[2][3] = 255
	p[3] = texel{0, 0, 0, 0}
	if mode == bc1Opaque {
		p[3][3] = 255
	}
	return p
}

// principalAxis is the direction the block's colours vary along most,
// found by running the power method on their covariance. It is the line
// the endpoints are picked from, so a block of one hue lands its
// endpoints on that hue rather than on a corner of the colour cube.
func principalAxis(b block, channels int) ([4]float64, [4]float64) {
	var mean [4]float64
	for _, t := range b {
		for i := range channels {
			mean[i] += float64(t[i])
		}
	}
	for i := range channels {
		mean[i] /= 16
	}
	var cov [4][4]float64
	for _, t := range b {
		var d [4]float64
		for i := range channels {
			d[i] = float64(t[i]) - mean[i]
		}
		for i := range channels {
			for j := range channels {
				cov[i][j] += d[i] * d[j]
			}
		}
	}
	// Start from the channel that varies most, which is never orthogonal
	// to the axis being looked for.
	axis := [4]float64{}
	best := -1.0
	for i := range channels {
		if cov[i][i] > best {
			best, axis = cov[i][i], [4]float64{}
			axis[i] = 1
		}
	}
	for range 8 {
		var next [4]float64
		for i := range channels {
			for j := range channels {
				next[i] += cov[i][j] * axis[j]
			}
		}
		norm := 0.0
		for i := range channels {
			norm += next[i] * next[i]
		}
		if norm < 1e-9 {
			break // a block of one colour has no axis
		}
		norm = 1 / math.Sqrt(norm)
		for i := range channels {
			axis[i] = next[i] * norm
		}
	}
	return mean, axis
}

// bc1Endpoints picks the two colours to interpolate between: the ends of
// the block's spread along its principal axis, pulled in slightly so the
// interpolants cover the middle rather than the extremes.
func bc1Endpoints(b block) (texel, texel) {
	mean, axis := principalAxis(b, 3)
	lo, hi := 1e30, -1e30
	for _, t := range b {
		d := 0.0
		for i := range 3 {
			d += (float64(t[i]) - mean[i]) * axis[i]
		}
		lo, hi = min(lo, d), max(hi, d)
	}
	var a, c texel
	for i := range 3 {
		a[i] = clamp8(mean[i] + hi*axis[i])
		c[i] = clamp8(mean[i] + lo*axis[i])
	}
	a[3], c[3] = 255, 255
	return a, c
}

// bc1Indices picks the nearest palette entry for each texel and returns
// them packed with the error they cost.
func bc1Indices(b block, p [4]texel, entries int) (uint32, int) {
	var bits uint32
	total := 0
	for i, t := range b {
		best, bestErr := 0, 1<<30
		for k := range entries {
			if e := sqDist(t, p[k], 3); e < bestErr {
				best, bestErr = k, e
			}
		}
		bits |= uint32(best) << (2 * i)
		total += bestErr
	}
	return bits, total
}

// bc1Refit solves for the endpoints that best fit the texels given the
// indices already chosen, which pulls them off the extremes and onto the
// line the block's colours actually lie on.
func bc1Refit(b block, bits uint32) (texel, texel) {
	// The weight of the second endpoint for each index in four-colour
	// mode: the endpoints themselves, then a third and two thirds.
	weights := [4]float64{0, 1, 1.0 / 3, 2.0 / 3}
	var sa, sb, sc float64
	var x, y [3]float64
	for i, t := range b {
		w := weights[bits>>(2*i)&3]
		sa += (1 - w) * (1 - w)
		sb += (1 - w) * w
		sc += w * w
		for k := range 3 {
			x[k] += (1 - w) * float64(t[k])
			y[k] += w * float64(t[k])
		}
	}
	det := sa*sc - sb*sb
	if det > -1e-9 && det < 1e-9 {
		return bc1Endpoints(b) // every texel took the same index
	}
	var e0, e1 texel
	for k := range 3 {
		e0[k] = clamp8((sc*x[k] - sb*y[k]) / det)
		e1[k] = clamp8((sa*y[k] - sb*x[k]) / det)
	}
	e0[3], e1[3] = 255, 255
	return e0, e1
}

// encodeBC1Block writes one colour block. It fits the endpoints to the
// block's principal axis, refines them twice against the indices they
// produce, and keeps whichever attempt cost least.
func encodeBC1Block(b block, out []byte) {
	e0, e1 := bc1Endpoints(b)
	bestC0, bestC1, bestBits, bestErr := uint16(0), uint16(0), uint32(0), 1<<30
	try := func(e0, e1 texel) {
		c0 := rgb565(e0[0], e0[1], e0[2])
		c1 := rgb565(e1[0], e1[1], e1[2])
		if c0 == c1 {
			// One colour: index 0 reproduces it exactly whichever mode
			// the decoder reads the block in.
			p := bc1Palette(c0, c1, bc1Always)
			bits, err := bc1Indices(b, p, 1)
			if err < bestErr {
				bestC0, bestC1, bestBits, bestErr = c0, c1, bits, err
			}
			return
		}
		if c0 < c1 {
			c0, c1 = c1, c0
		}
		p := bc1Palette(c0, c1, bc1Always)
		bits, err := bc1Indices(b, p, 4)
		if err < bestErr {
			bestC0, bestC1, bestBits, bestErr = c0, c1, bits, err
		}
	}
	try(e0, e1)
	for range 2 {
		if bestErr == 0 {
			break
		}
		e0, e1 = bc1Refit(b, bestBits)
		try(e0, e1)
	}
	binary.LittleEndian.PutUint16(out[0:], bestC0)
	binary.LittleEndian.PutUint16(out[2:], bestC1)
	binary.LittleEndian.PutUint32(out[4:], bestBits)
}

// decodeBC1Block unpacks one colour block in the mode the format that
// carries it uses.
func decodeBC1Block(in []byte, mode bc1Mode) block {
	c0 := binary.LittleEndian.Uint16(in[0:])
	c1 := binary.LittleEndian.Uint16(in[2:])
	bits := binary.LittleEndian.Uint32(in[4:])
	p := bc1Palette(c0, c1, mode)
	var b block
	for i := range 16 {
		b[i] = p[bits>>(2*i)&3]
	}
	return b
}

// encodeBC1 compresses an image's colour into eight bytes a block.
func encodeBC1(src *image.RGBA) []byte {
	return encodeBlocks(src, 8, encodeBC1Block)
}

// decodeBC1 expands a BC1 image in the mode its format asks for.
func decodeBC1(data []byte, w, h int, mode bc1Mode) (*image.RGBA, error) {
	return decodeBlocks(data, w, h, 8, func(in []byte) block { return decodeBC1Block(in, mode) })
}
