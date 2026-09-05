package gfx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// aseWriter builds an Aseprite file in memory, so the fixture is written
// here rather than committed as a binary.
type aseWriter struct {
	buf bytes.Buffer
}

func (w *aseWriter) u8(v uint8)   { w.buf.WriteByte(v) }
func (w *aseWriter) u16(v uint16) { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *aseWriter) u32(v uint32) { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *aseWriter) i16(v int16)  { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *aseWriter) i32(v int32)  { binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *aseWriter) str(s string) { w.u16(uint16(len(s))); w.buf.WriteString(s) }
func (w *aseWriter) zeros(n int)  { w.buf.Write(make([]byte, n)) }

// header writes the 128-byte file header.
func (w *aseWriter) header(frames, width, height, depth int) {
	w.u32(0) // the file size, which the reader ignores
	w.u16(aseMagic)
	w.u16(uint16(frames))
	w.u16(uint16(width))
	w.u16(uint16(height))
	w.u16(uint16(depth))
	w.u32(1) // flags: layer opacity is valid
	w.u16(100)
	w.zeros(8)
	w.u8(0) // the transparent palette index
	w.zeros(3)
	w.u16(256)
	w.u8(1)
	w.u8(1)
	w.zeros(8 + 84)
}

// frame writes a frame block whose chunks are already encoded.
func (w *aseWriter) frame(durationMS int, chunks [][]byte) {
	size := 16
	for _, c := range chunks {
		size += len(c)
	}
	w.u32(uint32(size))
	w.u16(aseFrameMagic)
	w.u16(0) // the old chunk count, superseded below
	w.u16(uint16(durationMS))
	w.zeros(2)
	w.u32(uint32(len(chunks)))
	for _, c := range chunks {
		w.buf.Write(c)
	}
}

// chunk wraps a body in a chunk header.
func chunk(kind int, body []byte) []byte {
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(len(body)+6))
	binary.Write(&out, binary.LittleEndian, uint16(kind))
	out.Write(body)
	return out.Bytes()
}

func layerChunk(name string, flags, kind, level int, opacity uint8) []byte {
	var w aseWriter
	w.u16(uint16(flags))
	w.u16(uint16(kind))
	w.u16(uint16(level))
	w.u16(0)
	w.u16(0)
	w.u16(0) // normal
	w.u8(opacity)
	w.zeros(3)
	w.str(name)
	return chunk(aseChunkLayer, w.buf.Bytes())
}

// celChunk writes a cel: kind 0 raw, 2 zlib compressed.
func celChunk(layer, x, y int, opacity uint8, kind, cw, ch int, pix []byte) []byte {
	var w aseWriter
	w.u16(uint16(layer))
	w.i16(int16(x))
	w.i16(int16(y))
	w.u8(opacity)
	w.u16(uint16(kind))
	w.i16(0)
	w.zeros(5)
	w.u16(uint16(cw))
	w.u16(uint16(ch))
	if kind == 2 {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		zw.Write(pix)
		zw.Close()
		w.buf.Write(z.Bytes())
	} else {
		w.buf.Write(pix)
	}
	return chunk(aseChunkCel, w.buf.Bytes())
}

// linkedCelChunk writes a cel that copies another frame's.
func linkedCelChunk(layer, frame int) []byte {
	var w aseWriter
	w.u16(uint16(layer))
	w.i16(0)
	w.i16(0)
	w.u8(255)
	w.u16(1)
	w.i16(0)
	w.zeros(5)
	w.u16(uint16(frame))
	return chunk(aseChunkCel, w.buf.Bytes())
}

func tagsChunk(tags []AsepriteTag) []byte {
	var w aseWriter
	w.u16(uint16(len(tags)))
	w.zeros(8)
	for _, t := range tags {
		w.u16(uint16(t.From))
		w.u16(uint16(t.To))
		dir := 0
		for i, d := range aseDirections {
			if d == t.Direction {
				dir = i
			}
		}
		w.u8(uint8(dir))
		w.u16(uint16(t.Repeat))
		w.zeros(6 + 3 + 1)
		w.str(t.Name)
	}
	return chunk(aseChunkTags, w.buf.Bytes())
}

func paletteChunk(entries []color.RGBA) []byte {
	var w aseWriter
	w.u32(uint32(len(entries)))
	w.u32(0)
	w.u32(uint32(len(entries) - 1))
	w.zeros(8)
	for _, c := range entries {
		w.u16(0)
		w.u8(c.R)
		w.u8(c.G)
		w.u8(c.B)
		w.u8(c.A)
	}
	return chunk(aseChunkPalette, w.buf.Bytes())
}

func sliceChunk(name string, x, y, cw, ch int) []byte {
	var w aseWriter
	w.u32(1)
	w.u32(0) // no nine-patch, no pivot
	w.u32(0)
	w.str(name)
	w.u32(0)
	w.i32(int32(x))
	w.i32(int32(y))
	w.u32(uint32(cw))
	w.u32(uint32(ch))
	return chunk(aseChunkSlice, w.buf.Bytes())
}

func userDataChunk(text string) []byte {
	var w aseWriter
	w.u32(1)
	w.str(text)
	return chunk(aseChunkUserData, w.buf.Bytes())
}

// rgba32 packs straight RGBA pixels for a 32-bit file.
func rgba32(pixels ...color.RGBA) []byte {
	out := make([]byte, 0, 4*len(pixels))
	for _, c := range pixels {
		out = append(out, c.R, c.G, c.B, c.A)
	}
	return out
}

// twoLayerFile is a two-frame, two-by-two sprite: a red background under
// a half-transparent green square that covers the right column, a hidden
// layer, a tag, a slice and some user data.
func twoLayerFile(t testing.TB) []byte {
	t.Helper()
	red := color.RGBA{R: 255, A: 255}
	green := color.RGBA{G: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	var w aseWriter
	w.header(2, 2, 2, 32)
	w.frame(120, [][]byte{
		layerChunk("bg", aseLayerVisible, 0, 0, 255),
		userDataChunk("the floor"),
		layerChunk("top", aseLayerVisible, 0, 0, 128),
		layerChunk("secret", 0, 0, 0, 255), // hidden
		// The background fills the frame; the green cel covers the right
		// column only, at half the layer's opacity.
		celChunk(0, 0, 0, 255, 0, 2, 2, rgba32(red, red, red, red)),
		celChunk(1, 1, 0, 255, 2, 1, 2, rgba32(green, green)),
		celChunk(2, 0, 0, 255, 0, 2, 2, rgba32(blue, blue, blue, blue)),
		tagsChunk([]AsepriteTag{{Name: "idle", From: 0, To: 1, Direction: "pingpong"}}),
		userDataChunk("the idle loop"),
		sliceChunk("hitbox", 0, 1, 2, 1),
		userDataChunk("where it hurts"),
	})
	// The second frame links both cels, so it draws the same as the first.
	w.frame(80, [][]byte{
		linkedCelChunk(0, 0),
		linkedCelChunk(1, 0),
	})
	return w.buf.Bytes()
}

func TestParseAseprite(t *testing.T) {
	a, err := ParseAseprite(twoLayerFile(t), AsepriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 2 || a.Height != 2 || len(a.Frames) != 2 {
		t.Fatalf("%dx%d, %d frames", a.Width, a.Height, len(a.Frames))
	}
	if a.Frames[0].Duration != 0.12 || a.Frames[1].Duration != 0.08 {
		t.Errorf("durations %v", a.Frames)
	}
	if len(a.Layers) != 3 || a.Layers[0].Name != "bg" || a.Layers[1].Opacity != 128 || a.Layers[2].Visible {
		t.Fatalf("layers %+v", a.Layers)
	}
	if a.Layers[0].UserData != "the floor" || a.Layers[0].Blend != "normal" {
		t.Errorf("layer user data %+v", a.Layers[0])
	}
	if len(a.Tags) != 1 || a.Tags[0].Name != "idle" || a.Tags[0].Direction != "pingpong" || a.Tags[0].UserData != "the idle loop" {
		t.Fatalf("tags %+v", a.Tags)
	}
	if r, ok := a.Slice("hitbox"); !ok || r != lin.R(0, 1, 2, 1) {
		t.Errorf("slice %v %v", r, ok)
	}
	if a.Slices[0].UserData != "where it hurts" {
		t.Errorf("slice user data %q", a.Slices[0].UserData)
	}
	// The composite: red on the left, the green cel over red on the right,
	// and the hidden blue layer nowhere.
	if got := a.Image.RGBAAt(0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Errorf("left pixel %v, want red", got)
	}
	right := a.Image.RGBAAt(1, 0)
	if right.G < 100 || right.R < 100 || right.B != 0 {
		t.Errorf("right pixel %v, want green over red", right)
	}
	// Both frames are packed, and the second links to the first, so they
	// match pixel for pixel.
	if len(a.Data.Order) != 2 {
		t.Fatalf("packed %v", a.Data.Order)
	}
	one, ok := a.Data.Frames["1"]
	if !ok {
		t.Fatal("frame 1 is missing")
	}
	x, y := int(one.Rect.X), int(one.Rect.Y)
	if a.Image.RGBAAt(x, y) != a.Image.RGBAAt(0, 0) || a.Image.RGBAAt(x+1, y) != a.Image.RGBAAt(1, 0) {
		t.Error("the linked frame does not match the frame it links")
	}
	if one.Duration != 0.08 {
		t.Errorf("packed frame 1 lasts %v", one.Duration)
	}
	// The tag plays through the atlas: pingpong over two frames is just
	// the two, since the ends are not repeated.
	if names := a.Data.Tags["idle"]; len(names) != 2 || names[0] != "0" || names[1] != "1" {
		t.Errorf("tag frames %v", names)
	}
}

// With Layers set, each layer's own frames are packed beside the
// composited ones, hidden layers included.
func TestParseAsepriteLayers(t *testing.T) {
	a, err := ParseAseprite(twoLayerFile(t), AsepriteOptions{Layers: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0", "1", "bg/0", "bg/1", "top/0", "top/1", "secret/0", "secret/1"}
	if len(a.Data.Order) != len(want) {
		t.Fatalf("packed %v", a.Data.Order)
	}
	for i, n := range want {
		if a.Data.Order[i] != n {
			t.Fatalf("packed %v, want %v", a.Data.Order, want)
		}
	}
	// The hidden layer is packed and holds its own blue pixels.
	f := a.Data.Frames["secret/0"]
	if c := a.Image.RGBAAt(int(f.Rect.X), int(f.Rect.Y)); c.B < 200 {
		t.Errorf("the hidden layer packed %v, want blue", c)
	}
	// The top layer's own frame keeps its half opacity and nothing under
	// it, so its left column is clear.
	f = a.Data.Frames["top/0"]
	if c := a.Image.RGBAAt(int(f.Rect.X), int(f.Rect.Y)); c.A != 0 {
		t.Errorf("the top layer's empty column is %v", c)
	}
	if c := a.Image.RGBAAt(int(f.Rect.X)+1, int(f.Rect.Y)); c.A < 100 || c.A > 150 {
		t.Errorf("the top layer's cel is %v, want about half opaque", c)
	}
}

// Greyscale and indexed files read as well, and the transparent index is
// clear on a normal layer.
func TestParseAsepriteColorModes(t *testing.T) {
	t.Run("greyscale", func(t *testing.T) {
		var w aseWriter
		w.header(1, 2, 1, 16)
		w.frame(100, [][]byte{
			layerChunk("l", aseLayerVisible, 0, 0, 255),
			celChunk(0, 0, 0, 255, 0, 2, 1, []byte{200, 255, 40, 255}),
		})
		a, err := ParseAseprite(w.buf.Bytes(), AsepriteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if c := a.Image.RGBAAt(0, 0); c.R != 200 || c.G != 200 || c.B != 200 {
			t.Errorf("grey pixel %v", c)
		}
	})
	t.Run("indexed", func(t *testing.T) {
		var w aseWriter
		w.header(1, 2, 1, 8)
		w.frame(100, [][]byte{
			paletteChunk([]color.RGBA{{A: 255}, {R: 10, G: 200, B: 30, A: 255}}),
			layerChunk("l", aseLayerVisible, 0, 0, 255),
			celChunk(0, 0, 0, 255, 0, 2, 1, []byte{0, 1}),
		})
		a, err := ParseAseprite(w.buf.Bytes(), AsepriteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Palette) != 2 || a.Palette[1].G != 200 {
			t.Fatalf("palette %v", a.Palette)
		}
		if c := a.Image.RGBAAt(0, 0); c.A != 0 {
			t.Errorf("the transparent index drew %v", c)
		}
		if c := a.Image.RGBAAt(1, 0); c.G != 200 {
			t.Errorf("indexed pixel %v", c)
		}
	})
}

// A layer inside a hidden group is left out, and its name carries the
// group's.
func TestParseAsepriteGroups(t *testing.T) {
	var w aseWriter
	w.header(1, 1, 1, 32)
	w.frame(100, [][]byte{
		layerChunk("gear", 0, 1, 0, 255),               // a hidden group
		layerChunk("hat", aseLayerVisible, 0, 1, 255),  // inside it
		layerChunk("body", aseLayerVisible, 0, 0, 255), // back at the top level
		celChunk(1, 0, 0, 255, 0, 1, 1, rgba32(color.RGBA{R: 255, A: 255})),
		celChunk(2, 0, 0, 255, 0, 1, 1, rgba32(color.RGBA{B: 255, A: 255})),
	})
	a, err := ParseAseprite(w.buf.Bytes(), AsepriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Layers[1].Name != "gear/hat" || a.Layers[2].Name != "body" {
		t.Fatalf("names %q %q", a.Layers[1].Name, a.Layers[2].Name)
	}
	if !a.Layers[0].Group {
		t.Error("the group is not marked as one")
	}
	if c := a.Image.RGBAAt(0, 0); c.B < 200 || c.R > 0 {
		t.Errorf("composite %v, want only the body layer", c)
	}
}

// Corrupt files are errors, never panics.
func TestParseAsepriteErrors(t *testing.T) {
	good := twoLayerFile(t)
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short", good[:64]},
		{"truncated", good[:len(good)-20]},
		{"bad magic", append([]byte{0, 0, 0, 0, 1, 2}, good[6:]...)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseAseprite(c.data, AsepriteOptions{}); err == nil {
				t.Error("no error")
			}
		})
	}
	// A frame with no layers at all still gives an empty sprite rather
	// than failing.
	var w aseWriter
	w.header(1, 4, 4, 32)
	w.frame(100, nil)
	a, err := ParseAseprite(w.buf.Bytes(), AsepriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Image.Bounds().Dx() != 4 {
		t.Errorf("empty sprite packed %v", a.Image.Bounds())
	}
}

// The parsed file uploads and binds, and the animation plays through the
// atlas with the durations the file gave it.
func TestAsepriteUpload(t *testing.T) {
	g := newHeadless(t, 16, 16)
	a, err := ParseAseprite(twoLayerFile(t), AsepriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	atlas, err := a.Upload(g, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer atlas.Tex.Destroy()
	if a.Atlas != atlas {
		t.Error("Upload did not keep the atlas")
	}
	anim := atlas.Animation("idle")
	if len(anim.Frames) != 2 {
		t.Fatalf("animation %+v", anim)
	}
	if l := anim.Length(); l < 0.19 || l > 0.21 {
		t.Errorf("animation lasts %v, want the frame durations", l)
	}
	first, _ := anim.At(0)
	second, _ := anim.At(0.15)
	if first == second {
		t.Error("the animation does not advance")
	}
	if _, ok := atlas.Region("0"); !ok {
		t.Error("frame 0 has no region")
	}
}
