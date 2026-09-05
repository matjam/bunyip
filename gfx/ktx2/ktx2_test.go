package ktx2

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// testImage draws something with the features a block encoder finds
// hard: smooth gradients, hard edges between unrelated colours, a
// saturated ramp and a soft alpha edge. Every encoder in the package is
// measured against it.
func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w-1), float64(y)/float64(h-1)
			c := color.RGBA{
				R: uint8(255 * fx),
				G: uint8(255 * fy),
				B: uint8(255 * (0.5 + 0.5*math.Sin(8*fx*fy))),
				A: 255,
			}
			// A quarter of the image is a hard checkerboard of two
			// unrelated colours, which is where one pair of endpoints per
			// block is not enough.
			if x > w/2 && y > h/2 {
				if (x/3+y/3)%2 == 0 {
					c = color.RGBA{240, 20, 40, 255}
				} else {
					c = color.RGBA{20, 80, 230, 255}
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// testAlphaImage is testImage with a soft alpha edge running across it,
// for the formats that carry alpha. It is already premultiplied, which
// is what the encoders expect of a colour image.
func testAlphaImage(w, h int) *image.RGBA {
	img := testImage(w, h)
	for y := range h {
		for x := range w {
			a := uint8(min(max(255*float64(x)/float64(w-1)*1.6-80, 0), 255))
			at := y*img.Stride + x*4
			for c := range 3 {
				img.Pix[at+c] = uint8(int(img.Pix[at+c]) * int(a) / 255)
			}
			img.Pix[at+3] = a
		}
	}
	return img
}

// roundTrip encodes an image and decodes level 0 again.
func roundTrip(t *testing.T, src *image.RGBA, opts Options) *image.RGBA {
	t.Helper()
	f, err := Encode(src, opts)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := f.DecodeLevel(0)
	if err != nil {
		t.Fatalf("DecodeLevel: %v", err)
	}
	return got
}

// TestEncodeQuality holds each encoder to a peak signal-to-noise ratio
// on the same test image, so a change that makes one of them worse is
// noticed. The bounds are a little under what the encoders reach today.
func TestEncodeQuality(t *testing.T) {
	const w, h = 128, 128
	src := testImage(w, h)
	alpha := testAlphaImage(w, h)
	cases := []struct {
		name     string
		format   Format
		src      *image.RGBA
		channels int
		wantDB   float64
	}{
		{"BC1", BC1RGBUnorm, src, 3, 30},
		{"BC3", BC3Unorm, alpha, 4, 34},
		{"BC7", BC7Unorm, src, 3, 45},
		{"BC7 with alpha", BC7Unorm, alpha, 4, 36},
		{"BC7 fast", BC7Unorm, src, 3, 31},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := Options{Format: c.format, NoMipmaps: true, Fast: c.name == "BC7 fast"}
			got := roundTrip(t, c.src, opts)
			// The source is compared as it is: a linear format leaves it
			// alone, and the test images are already premultiplied.
			db := PSNR(c.src, got, c.channels)
			t.Logf("%s: %.2f dB", c.name, db)
			if db < c.wantDB {
				t.Errorf("%s round trip is %.2f dB, want at least %.2f", c.name, db, c.wantDB)
			}
		})
	}
}

// TestBC4Exact checks that BC4 and BC5 reproduce a block holding one or
// two values exactly, which is what taking the block's own lowest and
// highest as the endpoints buys.
func TestBC4Exact(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			// Two values a block, so both are endpoints.
			v := uint8(30)
			if x%4 < 2 {
				v = 200
			}
			g := uint8(90)
			if y%4 < 2 {
				g = 15
			}
			src.SetRGBA(x, y, color.RGBA{v, g, 0, 255})
		}
	}
	for _, c := range []struct {
		name     string
		format   Format
		channels int
	}{{"BC4", BC4Unorm, 1}, {"BC5", BC5Unorm, 2}} {
		t.Run(c.name, func(t *testing.T) {
			got := roundTrip(t, src, Options{Format: c.format, NoMipmaps: true})
			for y := range 8 {
				for x := range 8 {
					at := y*src.Stride + x*4
					for ch := range c.channels {
						if got.Pix[at+ch] != src.Pix[at+ch] {
							t.Fatalf("texel (%d,%d) channel %d = %d, want %d",
								x, y, ch, got.Pix[at+ch], src.Pix[at+ch])
						}
					}
				}
			}
		})
	}
}

// TestFlatBlocks checks how close each colour encoder comes on a block
// of one colour, which is the common case in a texture of flat regions
// and where an error would show as banding. BC7 is exact in colour; the
// formats with five and six bit endpoints are exact only when the
// colour survives that quantisation, so they get a step of each channel.
func TestFlatBlocks(t *testing.T) {
	want := color.RGBA{0x40, 0x80, 0xC0, 255}
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			src.SetRGBA(x, y, want)
		}
	}
	got := roundTrip(t, src, Options{Format: BC7Unorm, NoMipmaps: true})
	if c := got.RGBAAt(3, 3); c.R != want.R || c.G != want.G || c.B != want.B {
		t.Errorf("BC7 flat block came back %v, want %v in colour", c, want)
	}
	// Mode 6 quantises alpha with the colour under one shared parity bit,
	// and 255 needs that bit set, so a fully opaque block may come back a
	// step under. One part in 255 of the background showing through is
	// not visible, and forcing the bit would cost a step of colour.
	if c := got.RGBAAt(3, 3); c.A < 254 {
		t.Errorf("BC7 flat block alpha came back %d, want 254 or 255", c.A)
	}
	// BC3's colour half is a BC1 block, so both take the same bound.
	for _, f := range []Format{BC1RGBUnorm, BC3Unorm} {
		t.Run(f.String(), func(t *testing.T) {
			c := roundTrip(t, src, Options{Format: f, NoMipmaps: true}).RGBAAt(3, 3)
			if abs(int(c.R)-int(want.R)) > 8 || abs(int(c.G)-int(want.G)) > 4 || abs(int(c.B)-int(want.B)) > 8 {
				t.Errorf("flat block came back %v, want within a quantisation step of %v", c, want)
			}
		})
	}
}

// TestBC7TwoSubsets checks that a block holding two clusters of colour,
// which one pair of endpoints cannot fit, is written in the two-subset
// mode and comes back all but exact. This is what the partition tables
// buy, and the fast path that skips them is much worse on such a block.
func TestBC7TwoSubsets(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			var c color.RGBA
			switch {
			case x < 2 && y%2 == 0:
				c = color.RGBA{40, 8, 8, 255}
			case x < 2:
				c = color.RGBA{200, 8, 8, 255}
			case y%2 == 0:
				c = color.RGBA{8, 8, 60, 255}
			default:
				c = color.RGBA{8, 8, 220, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	full, err := Encode(src, Options{Format: BC7Unorm, NoMipmaps: true})
	if err != nil {
		t.Fatal(err)
	}
	if m := bc7ModeOf(full.Levels[0]); m != 1 {
		t.Errorf("the block was written in mode %d, want mode 1", m)
	}
	got, err := full.DecodeLevel(0)
	if err != nil {
		t.Fatal(err)
	}
	db := PSNR(src, got, 3)
	if db < 50 {
		t.Errorf("two clusters round trip at %.2f dB, want at least 50", db)
	}
	fast := roundTrip(t, src, Options{Format: BC7Unorm, NoMipmaps: true, Fast: true})
	if fastDB := PSNR(src, fast, 3); fastDB > db-10 {
		t.Errorf("the one-subset mode reached %.2f dB against %.2f, so the partitions bought nothing", fastDB, db)
	}
}

// bc7ModeOf reads the mode a block was written in: the count of zero
// bits before the first one.
func bc7ModeOf(data []byte) int {
	var r bc7Bits
	copy(r.b[:], data)
	mode := 0
	for mode < 8 && r.read(1) == 0 {
		mode++
	}
	return mode
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestMipChain checks that the chain runs down to one texel, that each
// level is the size and the byte count the format says, and that a mip
// of a flat image keeps its colour, which is what averaging in linear
// light is for.
func TestMipChain(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			src.SetRGBA(x, y, color.RGBA{200, 100, 50, 255})
		}
	}
	f, err := Encode(src, Options{Format: BC7SRGB})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Levels) != 7 { // 64x32 down to 1x1
		t.Fatalf("%d levels, want 7", len(f.Levels))
	}
	for i := range f.Levels {
		w, h := f.LevelSize(i)
		if want := f.Format.LevelBytes(w, h); len(f.Levels[i]) != want {
			t.Errorf("level %d is %d bytes for %dx%d, want %d", i, len(f.Levels[i]), w, h, want)
		}
		img, err := f.DecodeLevel(i)
		if err != nil {
			t.Fatalf("level %d: %v", i, err)
		}
		c := img.RGBAAt(0, 0)
		if abs(int(c.R)-200) > 3 || abs(int(c.G)-100) > 3 || abs(int(c.B)-50) > 3 {
			t.Errorf("level %d is %v, want near (200,100,50)", i, c)
		}
	}
}

// TestMipHalvesInLinearLight checks that a black and white checker
// averages to mid grey in linear light, which reads as about 188 in
// sRGB. Averaging the bytes instead would give 128 and a chain that
// darkens as it goes.
func TestMipHalvesInLinearLight(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	src.SetRGBA(1, 1, color.RGBA{255, 255, 255, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 0, 0, 255})
	src.SetRGBA(0, 1, color.RGBA{0, 0, 0, 255})
	got := downsample(src, true)
	if v := got.RGBAAt(0, 0).R; v < 180 || v > 195 {
		t.Errorf("an sRGB checker averaged to %d, want about 188", v)
	}
	if v := downsample(src, false).RGBAAt(0, 0).R; v < 126 || v > 130 {
		t.Errorf("a linear checker averaged to %d, want 128", v)
	}
}

// TestContainerRoundTrip writes a file and reads it back, checking that
// every level survives byte for byte along with the format and size.
func TestContainerRoundTrip(t *testing.T) {
	src := testImage(37, 19) // not a multiple of the block size
	f, err := Encode(src, Options{Format: BC7SRGB})
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Format != f.Format || back.Width != 37 || back.Height != 19 {
		t.Fatalf("read back %s %dx%d, want %s 37x19", back.Format, back.Width, back.Height, f.Format)
	}
	if len(back.Levels) != len(f.Levels) {
		t.Fatalf("%d levels read, want %d", len(back.Levels), len(f.Levels))
	}
	for i := range f.Levels {
		if string(back.Levels[i]) != string(f.Levels[i]) {
			t.Errorf("level %d differs after a round trip", i)
		}
		// Every level's bytes must start where the driver can copy from.
		off := offsetOf(data, back.Levels[i])
		if off%levelAlign != 0 {
			t.Errorf("level %d starts at %d, which is not a multiple of %d", i, off, levelAlign)
		}
	}
}

// offsetOf is where a sub-slice of data begins.
func offsetOf(data, sub []byte) int { return cap(data) - cap(sub) }

// TestParseRejects checks that the reader turns away files it cannot
// carry rather than reading past the end of one.
func TestParseRejects(t *testing.T) {
	good, err := Encode(testImage(8, 8), Options{Format: BC1RGBSRGB})
	if err != nil {
		t.Fatal(err)
	}
	data, err := good.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("the file this package wrote did not parse: %v", err)
	}
	cases := []struct {
		name string
		edit func(b []byte)
	}{
		{"bad identifier", func(b []byte) { b[1] = 'X' }},
		{"supercompressed", func(b []byte) { b[44] = 1 }},
		{"a cube map", func(b []byte) { b[36] = 6 }},
		{"an array", func(b []byte) { b[32] = 2 }},
		{"an unknown format", func(b []byte) { b[12] = 200 }},
		{"a level past the end", func(b []byte) { b[80] = 0xFF; b[81] = 0xFF }},
		{"a level of the wrong size", func(b []byte) { b[88] = 0xF0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := append([]byte(nil), data...)
			c.edit(bad)
			if _, err := Parse(bad); err == nil {
				t.Error("parsed a file it should have turned away")
			}
		})
	}
	for n := range headerSize {
		if _, err := Parse(data[:n]); err == nil {
			t.Fatalf("parsed a %d-byte file", n)
		}
	}
}

// TestNamed checks the format names the command line takes.
func TestNamed(t *testing.T) {
	cases := []struct {
		name   string
		linear bool
		want   Format
	}{
		{"bc1", false, BC1RGBSRGB},
		{"bc1", true, BC1RGBUnorm},
		{"bc7", false, BC7SRGB},
		{"bc7", true, BC7Unorm},
		{"bc4", false, BC4Unorm}, // no colour, so never sRGB
		{"bc5", true, BC5Unorm},
	}
	for _, c := range cases {
		got, err := Named(c.name, c.linear)
		if err != nil || got != c.want {
			t.Errorf("Named(%q, %v) = %v, %v; want %v", c.name, c.linear, got, err, c.want)
		}
	}
	if _, err := Named("etc2", false); err == nil {
		t.Error("Named took a format it does not write")
	}
}

// TestFormatSizes checks the block geometry each format reports, which
// is what the level sizes and the upload are built on.
func TestFormatSizes(t *testing.T) {
	cases := []struct {
		f          Format
		bw, bh, bb int
		srgb       bool
	}{
		{BC1RGBSRGB, 4, 4, 8, true},
		{BC3Unorm, 4, 4, 16, false},
		{BC4Unorm, 4, 4, 8, false},
		{BC5Unorm, 4, 4, 16, false},
		{BC7SRGB, 4, 4, 16, true},
		{R8G8B8A8Unorm, 1, 1, 4, false},
		{ASTC4x4SRGB, 4, 4, 16, true},
		{ASTC12x12Unorm, 12, 12, 16, false},
	}
	for _, c := range cases {
		bw, bh := c.f.BlockSize()
		if bw != c.bw || bh != c.bh || c.f.BlockBytes() != c.bb || c.f.SRGB() != c.srgb {
			t.Errorf("%s: block %dx%d %d bytes srgb=%v, want %dx%d %d srgb=%v",
				c.f, bw, bh, c.f.BlockBytes(), c.f.SRGB(), c.bw, c.bh, c.bb, c.srgb)
		}
	}
	// A 5 by 5 image is two blocks each way whatever is left over.
	if got := BC7SRGB.LevelBytes(5, 5); got != 4*16 {
		t.Errorf("a 5x5 BC7 level is %d bytes, want 64", got)
	}
}
