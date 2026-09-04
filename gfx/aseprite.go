package gfx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// Aseprite is a parsed .aseprite or .ase file: every frame composited
// from its visible layers into one packed image, an AtlasData that names
// the packed frames and carries the file's tags as animations, and the
// pieces the editor keeps beside the pixels. ParseAseprite reads it and
// Upload puts it on the GPU.
//
// Frames are named by their number, "0" upwards, in the order they play.
// With AsepriteOptions.Layers a layer's own frames are named
// "<layer>/<number>", where a layer inside a group carries the group's
// name first, so a hat drawn on its own is atlas.Region("gear/hat/3").
type Aseprite struct {
	Width, Height int // one frame, in pixels
	Frames        []AsepriteFrame
	Layers        []AsepriteLayer
	Tags          []AsepriteTag
	Slices        []AsepriteSlice
	Palette       []color.RGBA // empty for a file with no palette chunk
	Image         *image.RGBA  // every packed frame, premultiplied
	Data          *AtlasData   // the frames and tag animations of Image
	Atlas         *Atlas       // the bound atlas, once Upload has run
}

// AsepriteFrame is one frame of the file.
type AsepriteFrame struct {
	Duration float32 // seconds, as the editor timed it
}

// AsepriteLayer is one layer, in the order the file stacks them from the
// bottom.
type AsepriteLayer struct {
	// Name is the layer's path: its own name, with the names of the
	// groups it sits in before it, separated by slashes.
	Name string
	// Visible is the layer's own visibility in the editor. A layer inside
	// a hidden group is left out of the composite even when this is set.
	Visible bool
	Opacity uint8 // 255 unless the file records layer opacity
	Group   bool  // a group, which holds no pixels of its own
	Level   int   // how deep in the group tree, zero at the top
	// Blend names the layer's blend mode ("normal", "multiply", and the
	// rest of the editor's list). Only normal is composited; a layer in
	// any other mode is drawn as normal.
	Blend     string
	UserData  string     // the note the editor keeps on the layer
	UserColor color.RGBA // its colour, zero when it has none
}

// AsepriteTag is one animation tag: a range of frames and how it plays.
type AsepriteTag struct {
	Name     string
	From, To int
	// Direction is "forward", "reverse", "pingpong" or
	// "pingpong_reverse", the same names the JSON export uses.
	Direction string
	Repeat    int // how many times the editor plays it; zero means forever
	UserData  string
	UserColor color.RGBA
}

// AsepriteSlice is a named rectangle drawn in the editor's slice tool,
// with one key per frame it changes on.
type AsepriteSlice struct {
	Name      string
	Keys      []AsepriteSliceKey
	UserData  string
	UserColor color.RGBA
}

// AsepriteSliceKey is a slice's rectangle from one frame onwards.
type AsepriteSliceKey struct {
	Frame  int      // the first frame the key applies to
	Bounds lin.Rect // in the sprite's pixels
	// Center is the nine-slice middle, relative to Bounds; it is zero
	// when the slice is not a nine-patch.
	Center lin.Rect
	// Pivot is the slice's pivot, relative to Bounds; it is zero when the
	// slice has none.
	Pivot lin.Vec2
}

// AsepriteOptions selects what ParseAseprite packs.
type AsepriteOptions struct {
	// Layers packs each layer's own frames beside the composited ones,
	// for a game that draws a layer alone: a hat, a damage overlay, a
	// mask. Hidden layers are packed too, because a layer hidden in the
	// editor is often the one a game wants. It costs a packed frame per
	// layer per frame.
	Layers bool
}

// Slice returns a slice's rectangle on its first key, which is what a
// slice that never moves has.
func (a *Aseprite) Slice(name string) (lin.Rect, bool) {
	for i := range a.Slices {
		if a.Slices[i].Name == name && len(a.Slices[i].Keys) > 0 {
			return a.Slices[i].Keys[0].Bounds, true
		}
	}
	return lin.Rect{}, false
}

// Upload puts the packed image on the GPU and binds the atlas, which it
// also stores in Atlas. Destroy the atlas's texture when the game is
// done with it.
func (a *Aseprite) Upload(g *Graphics, opts TextureOptions) (*Atlas, error) {
	tex, err := g.NewTexture(a.Image, opts)
	if err != nil {
		return nil, err
	}
	a.Atlas = a.Data.Bind(tex)
	return a.Atlas, nil
}

// The file's shape. Aseprite writes a 128-byte header, then one block
// per frame holding chunks.
const (
	aseHeaderSize  = 128
	aseFrameHeader = 16
	aseMagic       = 0xA5E0
	aseFrameMagic  = 0xF1FA
	// Limits that keep a corrupt file from asking for the world.
	aseMaxSide   = 1 << 14
	aseMaxFrames = 1 << 12
	aseMaxPixels = 1 << 26 // the packed image, four bytes each
)

// Chunk types this reader knows.
const (
	aseChunkOldPalette  = 0x0004
	aseChunkOldPalette2 = 0x0011
	aseChunkLayer       = 0x2004
	aseChunkCel         = 0x2005
	aseChunkTags        = 0x2018
	aseChunkPalette     = 0x2019
	aseChunkUserData    = 0x2020
	aseChunkSlice       = 0x2022
)

// Layer flags.
const (
	aseLayerVisible    = 1
	aseLayerBackground = 8
)

var aseBlendNames = [...]string{
	"normal", "multiply", "screen", "overlay", "darken", "lighten",
	"color dodge", "color burn", "hard light", "soft light", "difference",
	"exclusion", "hue", "saturation", "color", "luminosity",
	"addition", "subtract", "divide",
}

var aseDirections = [...]string{"forward", "reverse", "pingpong", "pingpong_reverse"}

// aseCel is one layer's pixels on one frame, straight from the file.
type aseCel struct {
	x, y, w, h int
	opacity    uint8
	pix        []byte // in the file's colour depth
	link       int    // the frame this cel copies, or -1
}

// aseFile is the reader's working state.
type aseFile struct {
	out      *Aseprite
	depth    int // bits per pixel: 32, 16 or 8
	trans    uint8
	cels     []map[int]*aseCel // per frame, by layer index
	back     []bool            // per layer: a background layer, whose transparent index is opaque
	shown    []bool            // per layer: visible with every enclosing group visible
	udKind   int
	udIndex  int
	groupVis []bool   // visibility of the enclosing groups, by level
	groupNam []string // their names
}

// User data targets.
const (
	aseUserNone = iota
	aseUserLayer
	aseUserTag
	aseUserSlice
)

// ParseAseprite reads an Aseprite file: its header, frames, layers,
// cels, palette, tags and slices. It composites each frame's visible
// layers into one image, packs the frames into a grid and describes them
// in an AtlasData, so Atlas.Animation plays a tag with the timings the
// editor gave it. RGBA, greyscale and indexed files all read; layers
// blend as normal with their opacity, whatever mode the editor set.
func ParseAseprite(data []byte, opts AsepriteOptions) (*Aseprite, error) {
	f := &aseFile{out: &Aseprite{}, udKind: aseUserNone}
	if err := f.read(data); err != nil {
		return nil, fmt.Errorf("aseprite: %w", err)
	}
	if err := f.pack(opts); err != nil {
		return nil, fmt.Errorf("aseprite: %w", err)
	}
	return f.out, nil
}

// read walks the header and every frame's chunks.
func (f *aseFile) read(data []byte) error {
	if len(data) < aseHeaderSize {
		return fmt.Errorf("file is %d bytes, shorter than the header", len(data))
	}
	r := &aseReader{data: data}
	r.skip(4) // the file size, which is not needed to read it
	if magic := r.u16(); magic != aseMagic {
		return fmt.Errorf("magic %#04x is not an Aseprite file", magic)
	}
	frames := int(r.u16())
	f.out.Width, f.out.Height = int(r.u16()), int(r.u16())
	f.depth = int(r.u16())
	r.skip(4)                  // flags: layer opacity is honoured either way
	r.skip(2 + 4 + 4)          // the deprecated speed and two zero fields
	f.trans = r.u8()           // the palette entry that is transparent
	r.skip(3 + 2 + 1 + 1)      // ignored bytes, colour count, pixel aspect
	r.skip(2 + 2 + 2 + 2 + 84) // the grid, then the reserved tail
	if r.err != nil {
		return r.err
	}
	switch f.depth {
	case 32, 16, 8:
	default:
		return fmt.Errorf("colour depth %d", f.depth)
	}
	if f.out.Width <= 0 || f.out.Height <= 0 || f.out.Width > aseMaxSide || f.out.Height > aseMaxSide {
		return fmt.Errorf("frame is %dx%d", f.out.Width, f.out.Height)
	}
	if frames <= 0 || frames > aseMaxFrames {
		return fmt.Errorf("%d frames", frames)
	}
	r.at = aseHeaderSize
	for i := range frames {
		if err := f.frame(r, i); err != nil {
			return fmt.Errorf("frame %d: %w", i, err)
		}
	}
	return nil
}

// frame reads one frame's header and chunks.
func (f *aseFile) frame(r *aseReader, index int) error {
	start := r.at
	size := int(r.u32())
	if magic := r.u16(); magic != aseFrameMagic {
		return fmt.Errorf("magic %#04x is not a frame", magic)
	}
	if r.err != nil {
		return r.err
	}
	if size < aseFrameHeader || start+size > len(r.data) {
		return fmt.Errorf("claims %d bytes", size)
	}
	chunks := int(r.u16())
	duration := int(r.u16())
	r.skip(2)
	if n := int(r.u32()); n > 0 {
		chunks = n // the newer count, which the old one saturates at 65535
	}
	if r.err != nil {
		return r.err
	}
	f.out.Frames = append(f.out.Frames, AsepriteFrame{Duration: float32(duration) / 1000})
	f.cels = append(f.cels, map[int]*aseCel{})
	end := start + size
	for range chunks {
		if r.at >= end {
			break // a frame that promises more chunks than it holds
		}
		at := r.at
		csize := int(r.u32())
		ctype := int(r.u16())
		if r.err != nil {
			return r.err
		}
		if csize < 6 || at+csize > end {
			return fmt.Errorf("chunk of %d bytes", csize)
		}
		body := &aseReader{data: r.data[at+6 : at+csize]}
		if err := f.chunk(ctype, body, index); err != nil {
			return err
		}
		r.at = at + csize
	}
	r.at = end
	return nil
}

// chunk reads one chunk of a frame. A chunk this reader does not know is
// skipped, which is how a file from a newer editor still loads.
func (f *aseFile) chunk(kind int, r *aseReader, frame int) error {
	switch kind {
	case aseChunkLayer:
		return f.layer(r)
	case aseChunkCel:
		return f.cel(r, frame)
	case aseChunkTags:
		return f.tags(r)
	case aseChunkPalette:
		return f.palette(r)
	case aseChunkOldPalette, aseChunkOldPalette2:
		return f.oldPalette(r, kind)
	case aseChunkSlice:
		return f.slice(r)
	case aseChunkUserData:
		return f.userData(r)
	}
	return nil
}

// layer reads a layer chunk and works out whether it shows: a layer
// inside a hidden group is hidden however it is set itself.
func (f *aseFile) layer(r *aseReader) error {
	flags := r.u16()
	kind := r.u16()
	level := int(r.u16())
	r.skip(2 + 2) // the default size, which the editor ignores too
	blend := int(r.u16())
	opacity := r.u8()
	r.skip(3)
	name := r.str()
	if r.err != nil {
		return r.err
	}
	if level > 64 {
		return fmt.Errorf("layer %q nests %d deep", name, level)
	}
	l := AsepriteLayer{
		Name: name, Visible: flags&aseLayerVisible != 0, Opacity: opacity,
		Group: kind == 1, Level: level, Blend: "normal",
	}
	if blend >= 0 && blend < len(aseBlendNames) {
		l.Blend = aseBlendNames[blend]
	}
	// The enclosing groups' names make the path, and their visibility
	// gates this layer's.
	for len(f.groupVis) > level {
		f.groupVis = f.groupVis[:len(f.groupVis)-1]
		f.groupNam = f.groupNam[:len(f.groupNam)-1]
	}
	shown := l.Visible
	for _, v := range f.groupVis {
		shown = shown && v
	}
	if level > 0 && len(f.groupNam) == level {
		l.Name = strings.Join(f.groupNam, "/") + "/" + name
	}
	for len(f.groupVis) < level {
		// A file that skips a level: treat the missing groups as visible.
		f.groupVis = append(f.groupVis, true)
		f.groupNam = append(f.groupNam, "")
	}
	f.groupVis = append(f.groupVis, shown)
	f.groupNam = append(f.groupNam, name)
	f.out.Layers = append(f.out.Layers, l)
	f.shown = append(f.shown, shown && !l.Group)
	f.back = append(f.back, flags&aseLayerBackground != 0)
	f.udKind, f.udIndex = aseUserLayer, len(f.out.Layers)-1
	return nil
}

// cel reads one layer's pixels on one frame: raw, linked to another
// frame's cel, or zlib compressed.
func (f *aseFile) cel(r *aseReader, frame int) error {
	layer := int(r.u16())
	c := &aseCel{x: int(r.i16()), y: int(r.i16()), opacity: r.u8(), link: -1}
	kind := int(r.u16())
	r.skip(2 + 5) // the z-index, which this reader does not reorder by
	if r.err != nil {
		return r.err
	}
	if layer < 0 || layer >= len(f.out.Layers) {
		return fmt.Errorf("cel names layer %d", layer)
	}
	switch kind {
	case 0, 2:
		c.w, c.h = int(r.u16()), int(r.u16())
		if r.err != nil {
			return r.err
		}
		if c.w < 0 || c.h < 0 || c.w > aseMaxSide || c.h > aseMaxSide {
			return fmt.Errorf("cel is %dx%d", c.w, c.h)
		}
		want := c.w * c.h * f.depth / 8
		if kind == 0 {
			c.pix = r.bytes(want)
			if r.err != nil {
				return r.err
			}
		} else {
			pix, err := aseInflate(r.data[r.at:], want)
			if err != nil {
				return err
			}
			c.pix = pix
		}
	case 1:
		c.link = int(r.u16())
		if r.err != nil {
			return r.err
		}
		if c.link < 0 || c.link >= frame {
			return fmt.Errorf("cel links frame %d from frame %d", c.link, frame)
		}
	case 3:
		return nil // a tilemap cel, which needs the tileset this reader skips
	default:
		return fmt.Errorf("cel type %d", kind)
	}
	f.cels[frame][layer] = c
	f.udKind = aseUserNone
	return nil
}

// aseInflate expands a zlib stream to exactly the size the cel's own
// width, height and depth call for, so a crafted file cannot expand
// without bound.
func aseInflate(src []byte, want int) ([]byte, error) {
	if want < 0 || want > aseMaxPixels*4 {
		return nil, fmt.Errorf("cel of %d bytes", want)
	}
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("cel: %w", err)
	}
	defer zr.Close()
	out := make([]byte, want)
	if _, err := io.ReadFull(zr, out); err != nil {
		return nil, fmt.Errorf("cel: %w", err)
	}
	return out, nil
}

func (f *aseFile) tags(r *aseReader) error {
	n := int(r.u16())
	r.skip(8)
	if r.err != nil {
		return r.err
	}
	first := len(f.out.Tags)
	for range n {
		t := AsepriteTag{From: int(r.u16()), To: int(r.u16()), Direction: "forward"}
		if d := int(r.u8()); d >= 0 && d < len(aseDirections) {
			t.Direction = aseDirections[d]
		}
		t.Repeat = int(r.u16())
		r.skip(6 + 3 + 1) // reserved, the deprecated colour, one more byte
		t.Name = r.str()
		if r.err != nil {
			return r.err
		}
		f.out.Tags = append(f.out.Tags, t)
	}
	f.udKind, f.udIndex = aseUserTag, first
	return nil
}

func (f *aseFile) palette(r *aseReader) error {
	size := int(r.u32())
	from, to := int(r.u32()), int(r.u32())
	r.skip(8)
	if r.err != nil {
		return r.err
	}
	if size < 0 || size > 1<<16 || from < 0 || to < from || to >= size {
		return fmt.Errorf("palette of %d entries, %d to %d", size, from, to)
	}
	for len(f.out.Palette) < size {
		f.out.Palette = append(f.out.Palette, color.RGBA{})
	}
	for i := from; i <= to; i++ {
		flags := r.u16()
		c := color.RGBA{R: r.u8(), G: r.u8(), B: r.u8(), A: r.u8()}
		if flags&1 != 0 {
			r.str() // the entry's name, which nothing here uses
		}
		if r.err != nil {
			return r.err
		}
		f.out.Palette[i] = c
	}
	f.udKind = aseUserNone
	return nil
}

// oldPalette reads the palette chunks written before 1.1. Entries are
// full bytes in chunk 0x0004 and six bits in 0x0011.
func (f *aseFile) oldPalette(r *aseReader, kind int) error {
	packets := int(r.u16())
	if r.err != nil {
		return r.err
	}
	at := 0
	for range packets {
		at += int(r.u8())
		n := int(r.u8())
		if n == 0 {
			n = 256
		}
		if r.err != nil {
			return r.err
		}
		if at < 0 || at+n > 256 {
			return fmt.Errorf("old palette packet at %d of %d", at, n)
		}
		for len(f.out.Palette) < at+n {
			f.out.Palette = append(f.out.Palette, color.RGBA{A: 255})
		}
		for i := range n {
			c := color.RGBA{R: r.u8(), G: r.u8(), B: r.u8(), A: 255}
			if kind == aseChunkOldPalette2 {
				// Six-bit channels, scaled up to eight.
				c.R, c.G, c.B = c.R*255/63, c.G*255/63, c.B*255/63
			}
			f.out.Palette[at+i] = c
		}
		at += n
	}
	return r.err
}

func (f *aseFile) slice(r *aseReader) error {
	keys := int(r.u32())
	flags := r.u32()
	r.skip(4)
	name := r.str()
	if r.err != nil {
		return r.err
	}
	if keys < 0 || keys > 1<<16 {
		return fmt.Errorf("slice %q has %d keys", name, keys)
	}
	s := AsepriteSlice{Name: name}
	for range keys {
		k := AsepriteSliceKey{Frame: int(r.u32())}
		k.Bounds = lin.R(float32(r.i32()), float32(r.i32()), float32(r.u32()), float32(r.u32()))
		if flags&1 != 0 {
			k.Center = lin.R(float32(r.i32()), float32(r.i32()), float32(r.u32()), float32(r.u32()))
		}
		if flags&2 != 0 {
			k.Pivot = lin.V2(float32(r.i32()), float32(r.i32()))
		}
		if r.err != nil {
			return r.err
		}
		s.Keys = append(s.Keys, k)
	}
	f.out.Slices = append(f.out.Slices, s)
	f.udKind, f.udIndex = aseUserSlice, len(f.out.Slices)-1
	return nil
}

// userData attaches the note and colour the editor keeps on whatever
// chunk came before it. Tags take theirs in order, one chunk each.
func (f *aseFile) userData(r *aseReader) error {
	flags := r.u32()
	var text string
	var c color.RGBA
	if flags&1 != 0 {
		text = r.str()
	}
	if flags&2 != 0 {
		c = color.RGBA{R: r.u8(), G: r.u8(), B: r.u8(), A: r.u8()}
	}
	if r.err != nil {
		return r.err
	}
	switch f.udKind {
	case aseUserLayer:
		if f.udIndex < len(f.out.Layers) {
			f.out.Layers[f.udIndex].UserData, f.out.Layers[f.udIndex].UserColor = text, c
		}
		f.udKind = aseUserNone
	case aseUserSlice:
		if f.udIndex < len(f.out.Slices) {
			f.out.Slices[f.udIndex].UserData, f.out.Slices[f.udIndex].UserColor = text, c
		}
		f.udKind = aseUserNone
	case aseUserTag:
		if f.udIndex < len(f.out.Tags) {
			f.out.Tags[f.udIndex].UserData, f.out.Tags[f.udIndex].UserColor = text, c
		}
		f.udIndex++
	}
	return nil
}

// pack composites every frame, adds a layer's own frames when they were
// asked for, lays them out in a grid and describes the result.
func (f *aseFile) pack(opts AsepriteOptions) error {
	a := f.out
	w, h := a.Width, a.Height
	names := make([]string, 0, len(a.Frames))
	images := make([]*image.RGBA, 0, len(a.Frames))
	for i := range a.Frames {
		names = append(names, strconv.Itoa(i))
		images = append(images, f.compose(i, -1))
	}
	if opts.Layers {
		for l := range a.Layers {
			if a.Layers[l].Group {
				continue
			}
			for i := range a.Frames {
				names = append(names, a.Layers[l].Name+"/"+strconv.Itoa(i))
				images = append(images, f.compose(i, l))
			}
		}
	}
	cols := int(math.Ceil(math.Sqrt(float64(len(images)))))
	rows := (len(images) + cols - 1) / cols
	if cols*w > aseMaxSide || rows*h > aseMaxSide || cols*w*rows*h > aseMaxPixels {
		return fmt.Errorf("packing %d frames of %dx%d needs %dx%d pixels", len(images), w, h, cols*w, rows*h)
	}
	a.Image = image.NewRGBA(image.Rect(0, 0, cols*w, rows*h))
	a.Data = &AtlasData{Frames: map[string]AtlasFrame{}, Tags: map[string][]string{},
		Size: lin.V2(float32(cols*w), float32(rows*h))}
	for i, img := range images {
		x, y := i%cols*w, i/cols*h
		draw.Draw(a.Image, image.Rect(x, y, x+w, y+h), img, image.Point{}, draw.Src)
		frame := AtlasFrame{Rect: lin.R(float32(x), float32(y), float32(w), float32(h)),
			SourceSize: lin.V2(float32(w), float32(h))}
		if i < len(a.Frames) {
			frame.Duration = a.Frames[i].Duration
		} else {
			frame.Duration = a.Frames[i%len(a.Frames)].Duration
		}
		a.Data.Order = append(a.Data.Order, names[i])
		a.Data.Frames[names[i]] = frame
	}
	// Tags index the composited frames, which are the first entries.
	composited := a.Data.Order[:len(a.Frames)]
	for _, t := range a.Tags {
		a.Data.Tags[t.Name] = tagFrames(composited, atlasTagJSON{From: t.From, To: t.To, Direction: t.Direction})
	}
	return nil
}

// compose draws one frame: every layer that shows, bottom to top, or one
// layer alone when only is not negative.
func (f *aseFile) compose(frame, only int) *image.RGBA {
	a := f.out
	dst := image.NewRGBA(image.Rect(0, 0, a.Width, a.Height))
	for l := range a.Layers {
		if only >= 0 && l != only {
			continue
		}
		if only < 0 && !f.shown[l] {
			continue
		}
		c := f.celAt(frame, l)
		if c == nil || c.w == 0 || c.h == 0 {
			continue
		}
		src := f.celImage(c, a.Layers[l].Opacity, f.back[l])
		if src == nil {
			continue
		}
		draw.Draw(dst, image.Rect(c.x, c.y, c.x+c.w, c.y+c.h), src, image.Point{}, draw.Over)
	}
	return dst
}

// celAt returns a layer's cel on a frame, following a linked cel to the
// frame it copies.
func (f *aseFile) celAt(frame, layer int) *aseCel {
	c := f.cels[frame][layer]
	if c == nil {
		return nil
	}
	if c.link >= 0 && c.link < len(f.cels) {
		return f.cels[c.link][layer]
	}
	return c
}

// celImage converts a cel to premultiplied RGBA, scaled by the layer's
// opacity and its own.
func (f *aseFile) celImage(c *aseCel, layerOpacity uint8, background bool) *image.RGBA {
	bpp := f.depth / 8
	if len(c.pix) < c.w*c.h*bpp {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	opacity := mul8(layerOpacity, c.opacity)
	for i := range c.w * c.h {
		var r, g, b, alpha uint8
		switch f.depth {
		case 32:
			p := c.pix[4*i:]
			r, g, b, alpha = p[0], p[1], p[2], p[3]
		case 16:
			p := c.pix[2*i:]
			r, g, b, alpha = p[0], p[0], p[0], p[1]
		default:
			idx := c.pix[i]
			if int(idx) < len(f.out.Palette) {
				e := f.out.Palette[idx]
				r, g, b, alpha = e.R, e.G, e.B, e.A
			}
			if idx == f.trans && !background {
				alpha = 0
			}
		}
		alpha = mul8(alpha, opacity)
		// Premultiplied, which is what image.RGBA and draw.Over expect.
		img.Pix[4*i] = mul8(r, alpha)
		img.Pix[4*i+1] = mul8(g, alpha)
		img.Pix[4*i+2] = mul8(b, alpha)
		img.Pix[4*i+3] = alpha
	}
	return img
}

// mul8 multiplies two bytes as fractions of 255, rounding.
func mul8(a, b uint8) uint8 { return uint8((uint32(a)*uint32(b) + 127) / 255) }

// aseReader reads the file's little-endian fields, remembering the first
// overrun so every read after it is harmless.
type aseReader struct {
	data []byte
	at   int
	err  error
}

func (r *aseReader) fail() {
	if r.err == nil {
		r.err = io.ErrUnexpectedEOF
	}
}

func (r *aseReader) take(n int) []byte {
	if r.err != nil || n < 0 || r.at+n > len(r.data) {
		r.fail()
		return nil
	}
	b := r.data[r.at : r.at+n]
	r.at += n
	return b
}

func (r *aseReader) skip(n int) { r.take(n) }

func (r *aseReader) u8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *aseReader) u16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *aseReader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *aseReader) i16() int16 { return int16(r.u16()) }
func (r *aseReader) i32() int32 { return int32(r.u32()) }

// bytes copies n bytes out, for pixels the reader keeps.
func (r *aseReader) bytes(n int) []byte {
	b := r.take(n)
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

// str reads a length-prefixed UTF-8 string.
func (r *aseReader) str() string {
	n := int(r.u16())
	b := r.take(n)
	if b == nil {
		return ""
	}
	return string(b)
}
