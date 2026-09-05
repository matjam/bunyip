package ktx2

import "fmt"

// Format is the Vulkan format number a KTX2 file names for its texel
// data. Naming a format permits container parsing and GPU upload; it
// does not imply CPU codec support. Encode supports RGBA8, BC1, BC3,
// unsigned BC4/BC5 and BC7. DecodeLevel supports the same formats, with
// BC7 limited to modes 1 and 6. Other named formats, including ASTC,
// require a device that can sample them directly.
type Format uint32

// The formats this package names. The numbers are Vulkan's, which is
// what a KTX2 header carries, so gfx hands one straight to the driver.
const (
	Undefined Format = 0

	R8G8B8A8Unorm Format = 37
	R8G8B8A8SRGB  Format = 43

	// BC1 is four bits a texel: three colour channels and one bit of
	// alpha in the RGBA forms. The encoder writes the opaque four-colour
	// mode, so the RGBA forms decode with every texel opaque.
	BC1RGBUnorm  Format = 131
	BC1RGBSRGB   Format = 132
	BC1RGBAUnorm Format = 133
	BC1RGBASRGB  Format = 134

	BC2Unorm Format = 135
	BC2SRGB  Format = 136

	// BC3 is eight bits a texel: a BC1 colour block and an eight-value
	// alpha block, which is the format for sprites with soft edges.
	BC3Unorm Format = 137
	BC3SRGB  Format = 138

	// BC4 is one channel at four bits a texel and BC5 is two at eight,
	// for masks, height fields and tangent-space normal maps.
	BC4Unorm Format = 139
	BC4SNorm Format = 140
	BC5Unorm Format = 141
	BC5SNorm Format = 142

	BC6HUfloat Format = 143
	BC6HSfloat Format = 144

	// BC7 is eight bits a texel with alpha, and is the best of these for
	// colour that has to hold up close.
	BC7Unorm Format = 145
	BC7SRGB  Format = 146

	// The ASTC formats, 4x4 through 12x12. The package passes these
	// through: it neither encodes nor decodes them.
	ASTC4x4Unorm   Format = 157
	ASTC4x4SRGB    Format = 158
	ASTC12x12Unorm Format = 183
	ASTC12x12SRGB  Format = 184
)

// astcBlocks is the texel block of each ASTC format pair, in the order
// the format numbers run from ASTC4x4Unorm.
var astcBlocks = [14][2]int{
	{4, 4}, {5, 4}, {5, 5}, {6, 5}, {6, 6}, {8, 5}, {8, 6},
	{8, 8}, {10, 5}, {10, 6}, {10, 8}, {10, 10}, {12, 10}, {12, 12},
}

// ASTC reports whether the format is one of the ASTC block formats,
// which this package carries but does not encode or decode.
func (f Format) ASTC() bool { return f >= ASTC4x4Unorm && f <= ASTC12x12SRGB }

// BlockSize is the texels one block covers, 4 by 4 for every BC format
// and 1 by 1 for the uncompressed ones.
func (f Format) BlockSize() (w, h int) {
	switch {
	case f.ASTC():
		b := astcBlocks[(f-ASTC4x4Unorm)/2]
		return b[0], b[1]
	case f >= BC1RGBUnorm && f <= BC7SRGB:
		return 4, 4
	}
	return 1, 1
}

// BlockBytes is how many bytes one block holds.
func (f Format) BlockBytes() int {
	switch f {
	case R8G8B8A8Unorm, R8G8B8A8SRGB:
		return 4
	case BC1RGBUnorm, BC1RGBSRGB, BC1RGBAUnorm, BC1RGBASRGB, BC4Unorm, BC4SNorm:
		return 8
	}
	if f.ASTC() || (f >= BC2Unorm && f <= BC7SRGB) {
		return 16
	}
	return 0
}

// Compressed reports whether the format stores blocks rather than
// texels.
func (f Format) Compressed() bool {
	w, h := f.BlockSize()
	return w > 1 || h > 1
}

// SRGB reports whether sampling the format decodes it from sRGB to
// linear light, which is what a colour texture wants and a mask or a
// normal map does not.
func (f Format) SRGB() bool {
	switch f {
	case R8G8B8A8SRGB, BC1RGBSRGB, BC1RGBASRGB, BC2SRGB, BC3SRGB, BC7SRGB:
		return true
	}
	return f.ASTC() && (f-ASTC4x4Unorm)%2 == 1
}

// LevelBytes is how many bytes a mip level of a size takes in the
// format, with the size rounded up to whole blocks.
func (f Format) LevelBytes(w, h int) int {
	bw, bh := f.BlockSize()
	if bw == 0 || bh == 0 {
		return 0
	}
	return ((w + bw - 1) / bw) * ((h + bh - 1) / bh) * f.BlockBytes()
}

// String names the format the way the Vulkan constant does, without the
// prefix.
func (f Format) String() string {
	names := map[Format]string{
		Undefined: "undefined", R8G8B8A8Unorm: "R8G8B8A8_UNORM", R8G8B8A8SRGB: "R8G8B8A8_SRGB",
		BC1RGBUnorm: "BC1_RGB_UNORM", BC1RGBSRGB: "BC1_RGB_SRGB",
		BC1RGBAUnorm: "BC1_RGBA_UNORM", BC1RGBASRGB: "BC1_RGBA_SRGB",
		BC2Unorm: "BC2_UNORM", BC2SRGB: "BC2_SRGB", BC3Unorm: "BC3_UNORM", BC3SRGB: "BC3_SRGB",
		BC4Unorm: "BC4_UNORM", BC4SNorm: "BC4_SNORM", BC5Unorm: "BC5_UNORM", BC5SNorm: "BC5_SNORM",
		BC6HUfloat: "BC6H_UFLOAT", BC6HSfloat: "BC6H_SFLOAT",
		BC7Unorm: "BC7_UNORM", BC7SRGB: "BC7_SRGB",
	}
	if n, ok := names[f]; ok {
		return n
	}
	if f.ASTC() {
		b := astcBlocks[(f-ASTC4x4Unorm)/2]
		kind := "UNORM"
		if f.SRGB() {
			kind = "SRGB"
		}
		return fmt.Sprintf("ASTC_%dx%d_%s", b[0], b[1], kind)
	}
	return fmt.Sprintf("Format(%d)", uint32(f))
}

// Named turns a name such as "bc7" or "bc1" into the format's sRGB or
// linear pair, for a command-line flag. The known names are bc1, bc3,
// bc4, bc5 and bc7; bc4 and bc5 hold no colour, so they are always
// linear.
func Named(name string, linear bool) (Format, error) {
	pair := func(unorm, srgb Format) Format {
		if linear {
			return unorm
		}
		return srgb
	}
	switch name {
	case "bc1":
		return pair(BC1RGBUnorm, BC1RGBSRGB), nil
	case "bc3":
		return pair(BC3Unorm, BC3SRGB), nil
	case "bc4":
		return BC4Unorm, nil
	case "bc5":
		return BC5Unorm, nil
	case "bc7":
		return pair(BC7Unorm, BC7SRGB), nil
	case "rgba":
		return pair(R8G8B8A8Unorm, R8G8B8A8SRGB), nil
	}
	return Undefined, fmt.Errorf("ktx2: no format %q; use bc1, bc3, bc4, bc5, bc7 or rgba", name)
}
