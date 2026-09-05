package ktx2

import (
	"fmt"
	"image"
	"image/draw"
	"math"
)

// Options says how Encode compresses an image.
type Options struct {
	// Format is the block format to write. Zero means BC7SRGB, which
	// suits colour that has to hold up close; BC1 is a quarter of the
	// size and drops alpha, BC3 keeps alpha at half of BC7's size, and
	// BC4 and BC5 are for one and two channels of data.
	Format Format
	// NoMipmaps writes level 0 alone. The default is the whole chain
	// down to one texel, averaged in linear light for an sRGB format, so
	// a distant surface does not shimmer and nothing is downsampled while
	// the game runs.
	NoMipmaps bool
	// Fast keeps the BC7 encoder to its single-subset mode instead of
	// also searching the sixty-four two-subset partitions. It is several
	// times quicker and a little worse on blocks that straddle an edge.
	Fast bool
}

// Encode compresses an image into a KTX2 file ready for
// gfx.NewCompressedTexture. A colour image, meaning one in an sRGB
// format, is premultiplied in linear light first, the same way
// gfx.NewTexture does it, so a compressed texture blends like an
// uncompressed one; a format that is not sRGB holds data rather than
// colour and its texels are encoded as they stand.
func Encode(src image.Image, opts Options) (*File, error) {
	format := opts.Format
	if format == Undefined {
		format = BC7SRGB
	}
	if format.ASTC() {
		return nil, fmt.Errorf("ktx2: %s is carried but not encoded", format)
	}
	if format.BlockBytes() == 0 {
		return nil, fmt.Errorf("ktx2: format %d cannot be written", uint32(format))
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("ktx2: an image with bounds %v", b)
	}
	level := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(level, level.Bounds(), src, b.Min, draw.Src)
	if format.SRGB() {
		// Go premultiplies in sRGB space, which an sRGB sampler would
		// decode too dark, so the texels are rewritten to premultiply in
		// linear light.
		linearPremultiply(level.Pix)
	}
	f := &File{Format: format, Width: b.Dx(), Height: b.Dy()}
	for {
		data, err := encodeLevel(level, format, opts.Fast)
		if err != nil {
			return nil, err
		}
		f.Levels = append(f.Levels, data)
		w, h := level.Rect.Dx(), level.Rect.Dy()
		if opts.NoMipmaps || (w == 1 && h == 1) {
			break
		}
		level = downsample(level, format.SRGB())
	}
	return f, nil
}

// encodeLevel compresses one mip level's texels.
func encodeLevel(level *image.RGBA, format Format, fast bool) ([]byte, error) {
	switch format {
	case R8G8B8A8Unorm, R8G8B8A8SRGB:
		return append([]byte(nil), level.Pix...), nil
	case BC1RGBUnorm, BC1RGBSRGB, BC1RGBAUnorm, BC1RGBASRGB:
		return encodeBC1(level), nil
	case BC3Unorm, BC3SRGB:
		return encodeBC3(level), nil
	case BC4Unorm:
		return encodeBC4(level, 0), nil
	case BC5Unorm:
		return encodeBC5(level), nil
	case BC7Unorm, BC7SRGB:
		return encodeBC7(level, fast), nil
	}
	return nil, fmt.Errorf("ktx2: format %s cannot be written", format)
}

// DecodeLevel expands one mip level back to RGBA, for a device that
// cannot sample the format and for checking what an encoder produced.
// BC4 puts its one channel in red and BC5 its two in red and green, in
// both cases with the rest of the texel left at zero and alpha opaque.
func (f *File) DecodeLevel(level int) (*image.RGBA, error) {
	if level < 0 || level >= len(f.Levels) {
		return nil, fmt.Errorf("ktx2: no level %d in a file with %d", level, len(f.Levels))
	}
	w, h := f.LevelSize(level)
	data := f.Levels[level]
	switch f.Format {
	case R8G8B8A8Unorm, R8G8B8A8SRGB:
		if len(data) < w*h*4 {
			return nil, errShort(len(data), w*h*4)
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, data)
		return img, nil
	case BC1RGBUnorm, BC1RGBSRGB:
		return decodeBC1(data, w, h, bc1Opaque)
	case BC1RGBAUnorm, BC1RGBASRGB:
		return decodeBC1(data, w, h, bc1Punchthrough)
	case BC3Unorm, BC3SRGB:
		return decodeBC3(data, w, h)
	case BC4Unorm:
		return decodeBC4(data, w, h)
	case BC5Unorm:
		return decodeBC5(data, w, h)
	case BC7Unorm, BC7SRGB:
		return decodeBC7(data, w, h)
	}
	return nil, fmt.Errorf("ktx2: %s cannot be decoded here", f.Format)
}

// Decodable reports whether DecodeLevel can expand the file's format,
// which is what gfx asks before falling back to software on a device
// that cannot sample it.
func (f *File) Decodable() bool {
	switch f.Format {
	case R8G8B8A8Unorm, R8G8B8A8SRGB,
		BC1RGBUnorm, BC1RGBSRGB, BC1RGBAUnorm, BC1RGBASRGB,
		BC3Unorm, BC3SRGB, BC4Unorm, BC5Unorm, BC7Unorm, BC7SRGB:
		return true
	}
	return false
}

// downsample halves an image, averaging each 2 by 2 group. A colour
// image is averaged in linear light, because averaging sRGB bytes
// darkens an edge; alpha is linear either way, so it is averaged as it
// stands. An odd dimension drops to the floor and the last row or column
// is averaged with itself.
func downsample(src *image.RGBA, srgb bool) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	nw, nh := max(w/2, 1), max(h/2, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := range nh {
		for x := range nw {
			var sum [4]float64
			for dy := range 2 {
				for dx := range 2 {
					sx, sy := min(x*2+dx, w-1), min(y*2+dy, h-1)
					at := sy*src.Stride + sx*4
					for c := range 3 {
						v := float64(src.Pix[at+c]) / 255
						if srgb {
							v = srgbToLinear(v)
						}
						sum[c] += v
					}
					sum[3] += float64(src.Pix[at+3]) / 255
				}
			}
			at := y*dst.Stride + x*4
			for c := range 3 {
				v := sum[c] / 4
				if srgb {
					v = linearToSRGB(v)
				}
				dst.Pix[at+c] = clamp8(v * 255)
			}
			dst.Pix[at+3] = clamp8(sum[3] / 4 * 255)
		}
	}
	return dst
}

// srgbToLinear and linearToSRGB are the sRGB transfer function and its
// inverse, on values from zero to one.
func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// linearPremultiply rewrites the premultiplied sRGB bytes image.RGBA
// holds so that an sRGB sampler decodes them to premultiplied linear
// colour. It is the same step gfx.NewTexture takes, and for the same
// reason: Go premultiplies as a*c in sRGB space, which the sampler
// decodes to linear(a*c) rather than the a*linear(c) a blend needs, so a
// half-transparent white texel would read as grey. Opaque and clear
// texels are unchanged.
func linearPremultiply(pix []byte) {
	for i := 0; i+3 < len(pix); i += 4 {
		a := pix[i+3]
		if a == 0 || a == 255 {
			continue
		}
		af := float64(a) / 255
		for k := range 3 {
			straight := min(float64(pix[i+k])/255/af, 1)
			pix[i+k] = clamp8(linearToSRGB(srgbToLinear(straight)*af) * 255)
		}
	}
}

// PSNR is the peak signal-to-noise ratio between two images of the same
// size, in decibels over the channels given: three for colour alone,
// four to include alpha. It is what bunyip-tex reports and what the
// tests hold an encoder to. Identical images have no noise, which it
// reports as positive infinity.
func PSNR(a, b *image.RGBA, channels int) float64 {
	if a.Rect != b.Rect {
		return 0
	}
	var sum float64
	var n int
	w, h := a.Rect.Dx(), a.Rect.Dy()
	for y := range h {
		for x := range w {
			ai, bi := y*a.Stride+x*4, y*b.Stride+x*4
			for c := range channels {
				d := float64(a.Pix[ai+c]) - float64(b.Pix[bi+c])
				sum += d * d
				n++
			}
		}
	}
	if n == 0 || sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(n)))
}
