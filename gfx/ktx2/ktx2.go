// Package ktx2 reads and writes KTX2 texture files and encodes and
// decodes the block-compressed formats they carry. It is the offline
// half of the texture pipeline: bunyip-tex turns a PNG or JPEG into a
// KTX2 file holding BC1, BC3, BC4, BC5 or BC7 blocks and the whole mip
// chain, and gfx.NewCompressedTexture uploads that file straight into a
// compressed image with the levels as they are, so nothing is
// compressed or downsampled while a game runs.
//
// # What is written
//
// A file holds one 2D image: a format, a size, and one byte slice per
// mip level, level 0 first. Array layers, cube faces, 3D textures and
// supercompression are neither written nor read, and a file that uses
// them is an error. The data format descriptor is written in its basic
// form so other KTX2 tools accept the file, and it is not read back:
// the format number is what the loader needs.
//
// # Colour
//
// Encode premultiplies a colour image in linear light before
// compressing it, the same way gfx.NewTexture does, so a compressed
// texture blends like an uncompressed one. A format that is not sRGB
// holds data rather than colour, so its texels are encoded as they
// stand and its mip chain is averaged without a gamma step.
//
// # ASTC
//
// ASTC blocks are carried but not encoded or decoded: a file that
// already holds ASTC parses, names its format and uploads on a device
// that samples it, and there is nothing to fall back on where a device
// does not.
package ktx2

import (
	"encoding/binary"
	"fmt"
)

// identifier is the twelve bytes every KTX2 file starts with.
var identifier = [12]byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, '\r', '\n', 0x1A, '\n'}

// headerSize is the fixed header, up to and including the index.
const headerSize = 80

// levelAlign is the offset each level's bytes start at. Sixteen is a
// multiple of every texel block size this package writes, which is what
// the specification asks for.
const levelAlign = 16

// maxDimension bounds the size a parsed file may claim, so a corrupt
// header cannot ask for an unreasonable allocation before the level
// index is checked against the data.
const maxDimension = 1 << 16

// File is one 2D texture: a format, a size and its mip levels, level 0
// first. Levels holds the blocks as the format packs them, ready to
// upload.
type File struct {
	Format        Format
	Width, Height int
	Levels        [][]byte
}

// LevelSize is the size of a mip level in texels, each dimension halved
// per level and never below one.
func (f *File) LevelSize(level int) (w, h int) {
	w, h = f.Width, f.Height
	for range level {
		w, h = max(w/2, 1), max(h/2, 1)
	}
	return w, h
}

// Parse reads a KTX2 file. It rejects the features this package does not
// carry: array layers, cube faces, depth beyond one and
// supercompression.
func Parse(data []byte) (*File, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("ktx2: %d bytes is too short for a header", len(data))
	}
	if [12]byte(data[:12]) != identifier {
		return nil, fmt.Errorf("ktx2: not a KTX2 file")
	}
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(data[at:]) }
	u64 := func(at int) uint64 { return binary.LittleEndian.Uint64(data[at:]) }
	f := &File{Format: Format(u32(12))}
	width, height, depth := u32(20), u32(24), u32(28)
	layers, faces, levels := u32(32), u32(36), u32(40)
	if scheme := u32(44); scheme != 0 {
		return nil, fmt.Errorf("ktx2: supercompression scheme %d is not read", scheme)
	}
	if depth > 1 || layers > 1 || faces != 1 {
		return nil, fmt.Errorf("ktx2: only a single 2D image is read, not depth %d, %d layers, %d faces", depth, layers, faces)
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("ktx2: a %dx%d image", width, height)
	}
	if width > maxDimension || height > maxDimension {
		return nil, fmt.Errorf("ktx2: a %dx%d image is larger than this package reads", width, height)
	}
	if f.Format.BlockBytes() == 0 {
		return nil, fmt.Errorf("ktx2: format %d is not one this package knows", uint32(f.Format))
	}
	f.Width, f.Height = int(width), int(height)
	// A levelCount of zero means the file wants the levels generated,
	// which this package does not do, so it reads as the one level given.
	n := int(max(levels, 1))
	if n > 32 {
		return nil, fmt.Errorf("ktx2: %d mip levels", n)
	}
	index := headerSize
	if len(data) < index+n*24 {
		return nil, fmt.Errorf("ktx2: the level index runs past the end of the file")
	}
	for i := range n {
		at := index + i*24
		off, length := u64(at), u64(at+8)
		if off > uint64(len(data)) || length > uint64(len(data))-off {
			return nil, fmt.Errorf("ktx2: level %d runs past the end of the file", i)
		}
		w, h := f.LevelSize(i)
		if want := f.Format.LevelBytes(w, h); uint64(want) != length {
			return nil, fmt.Errorf("ktx2: level %d is %d bytes, want %d for %dx%d %s", i, length, want, w, h, f.Format)
		}
		f.Levels = append(f.Levels, data[off:off+length])
	}
	return f, nil
}

// Bytes encodes the file. The levels are written smallest first, which
// is the order the specification recommends, with the level index in
// level order as it requires.
func (f *File) Bytes() ([]byte, error) {
	if len(f.Levels) == 0 {
		return nil, fmt.Errorf("ktx2: no levels to write")
	}
	if f.Width <= 0 || f.Height <= 0 {
		return nil, fmt.Errorf("ktx2: a %dx%d image", f.Width, f.Height)
	}
	if f.Format.BlockBytes() == 0 {
		return nil, fmt.Errorf("ktx2: format %s cannot be written", f.Format)
	}
	for i, lv := range f.Levels {
		w, h := f.LevelSize(i)
		if want := f.Format.LevelBytes(w, h); len(lv) != want {
			return nil, fmt.Errorf("ktx2: level %d is %d bytes, want %d for %dx%d %s", i, len(lv), want, w, h, f.Format)
		}
	}
	dfd := f.descriptor()
	kvd := keyValues(f.Format)
	n := len(f.Levels)
	out := make([]byte, headerSize+n*24)
	copy(out, identifier[:])
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(out[at:], v) }
	put32(12, uint32(f.Format))
	put32(16, 1) // typeSize: one for a block format
	put32(20, uint32(f.Width))
	put32(24, uint32(f.Height))
	put32(28, 0) // pixelDepth: zero for a 2D image
	put32(32, 0) // layerCount: zero for a texture that is not an array
	put32(36, 1) // faceCount
	put32(40, uint32(n))
	put32(44, 0) // supercompressionScheme: none

	dfdOffset := len(out)
	out = append(out, dfd...)
	kvdOffset := len(out)
	out = append(out, kvd...)
	put32(48, uint32(dfdOffset))
	put32(52, uint32(len(dfd)))
	put32(56, uint32(kvdOffset))
	put32(60, uint32(len(kvd)))
	// sgdByteOffset and sgdByteLength stay zero: no supercompression.

	// The largest level last, so a reader that streams the small ones
	// first can start drawing before the whole file has arrived.
	for i := n - 1; i >= 0; i-- {
		for len(out)%levelAlign != 0 {
			out = append(out, 0)
		}
		off := len(out)
		out = append(out, f.Levels[i]...)
		at := headerSize + i*24
		binary.LittleEndian.PutUint64(out[at:], uint64(off))
		binary.LittleEndian.PutUint64(out[at+8:], uint64(len(f.Levels[i])))
		binary.LittleEndian.PutUint64(out[at+16:], uint64(len(f.Levels[i])))
	}
	return out, nil
}

// Khronos data format constants, for the basic descriptor block.
const (
	dfModelBC1A = 128 // the colour models run BC1..BC7 from here
	dfPrimBT709 = 1
	dfTransLin  = 1
	dfTransSRGB = 2
	dfFlagAlpha = 1 // the alpha channel is premultiplied
)

// descriptor writes the basic data format descriptor. KTX2 requires one
// and other tools read it, so it names the colour model, the primaries,
// the transfer function, the texel block and one sample covering the
// block. This package itself reads the format number instead.
func (f *File) descriptor() []byte {
	model, samples := f.blockModel()
	blockSize := 24 + 16*len(samples)
	out := make([]byte, 4+blockSize)
	binary.LittleEndian.PutUint32(out, uint32(len(out)))
	b := out[4:]
	binary.LittleEndian.PutUint32(b[0:], 0)                       // vendorId 0, descriptorType 0
	binary.LittleEndian.PutUint32(b[4:], 2|uint32(blockSize)<<16) // versionNumber 2, descriptorBlockSize
	b[8] = model
	b[9] = dfPrimBT709
	b[10] = dfTransLin
	if f.Format.SRGB() {
		b[10] = dfTransSRGB
	}
	b[11] = dfFlagAlpha
	bw, bh := f.Format.BlockSize()
	b[12], b[13], b[14], b[15] = byte(bw-1), byte(bh-1), 0, 0
	b[16] = byte(f.Format.BlockBytes()) // bytesPlane0; the other planes stay zero
	for i, s := range samples {
		at := 24 + i*16
		binary.LittleEndian.PutUint16(b[at:], s.bitOffset)
		b[at+2] = s.bitLength - 1
		b[at+3] = s.channel
		binary.LittleEndian.PutUint32(b[at+8:], s.lower)
		binary.LittleEndian.PutUint32(b[at+12:], s.upper)
	}
	return out
}

// dfSample is one sample in the basic descriptor: which bits of a block
// it covers and what they mean.
type dfSample struct {
	bitOffset uint16
	bitLength uint8
	channel   uint8
	lower     uint32
	upper     uint32
}

// blockModel is the colour model and samples the descriptor names for a
// format. A block format describes its whole block with one sample per
// piece of it, which is what the Khronos data format specification asks
// for.
func (f *File) blockModel() (byte, []dfSample) {
	whole := func(bits uint8, channel uint8) []dfSample {
		return []dfSample{{bitOffset: 0, bitLength: bits, channel: channel, upper: 0xFFFFFFFF}}
	}
	switch f.Format {
	case BC1RGBUnorm, BC1RGBSRGB, BC1RGBAUnorm, BC1RGBASRGB:
		return dfModelBC1A, whole(64, 0)
	case BC2Unorm, BC2SRGB:
		return dfModelBC1A + 1, []dfSample{
			{bitOffset: 0, bitLength: 64, channel: 15, upper: 0xFFFFFFFF}, // alpha
			{bitOffset: 64, bitLength: 64, channel: 0, upper: 0xFFFFFFFF},
		}
	case BC3Unorm, BC3SRGB:
		return dfModelBC1A + 2, []dfSample{
			{bitOffset: 0, bitLength: 64, channel: 15, upper: 0xFFFFFFFF},
			{bitOffset: 64, bitLength: 64, channel: 0, upper: 0xFFFFFFFF},
		}
	case BC4Unorm, BC4SNorm:
		return dfModelBC1A + 3, whole(64, 0)
	case BC5Unorm, BC5SNorm:
		return dfModelBC1A + 4, []dfSample{
			{bitOffset: 0, bitLength: 64, channel: 0, upper: 0xFFFFFFFF},
			{bitOffset: 64, bitLength: 64, channel: 1, upper: 0xFFFFFFFF},
		}
	case BC7Unorm, BC7SRGB:
		return dfModelBC1A + 6, whole(128, 0)
	}
	// The uncompressed forms and anything else fall back to an RGBA
	// model with one sample a channel.
	var out []dfSample
	for i := range 4 {
		out = append(out, dfSample{bitOffset: uint16(i * 8), bitLength: 8, channel: byte(i), upper: 255})
	}
	return 1, out // KHR_DF_MODEL_RGBSDA
}

// keyValues writes the key and value data: who wrote the file and which
// way up its rows run. Entries are sorted by key and padded to four
// bytes, as the specification requires.
func keyValues(f Format) []byte {
	type kv struct{ key, value string }
	entries := []kv{
		{"KTXorientation", "rd"},
		{"KTXwriter", "bunyip ktx2"},
	}
	var out []byte
	for _, e := range entries {
		body := append(append([]byte(e.key), 0), append([]byte(e.value), 0)...)
		var head [4]byte
		binary.LittleEndian.PutUint32(head[:], uint32(len(body)))
		out = append(out, head[:]...)
		out = append(out, body...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}
