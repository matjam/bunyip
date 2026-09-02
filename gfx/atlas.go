package gfx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/matjam/bunyip/lin"
)

// AtlasFrame is one named rectangle of a packed texture.
type AtlasFrame struct {
	Rect     lin.Rect // pixels in the texture; for a rotated frame the packed size
	Duration float32  // seconds, from Aseprite exports; zero otherwise
	// Rotated frames are stored turned a quarter turn clockwise; Rect is
	// the packed rectangle and SourceSize the upright one.
	Rotated bool
	Trimmed bool
	// Offset is where the packed pixels sit within the untrimmed source.
	Offset     lin.Vec2
	SourceSize lin.Vec2
}

// AtlasData is a parsed atlas description before it is tied to a
// texture: TexturePacker JSON (hash or array) or Aseprite's JSON export.
type AtlasData struct {
	Frames map[string]AtlasFrame
	Order  []string            // frame names in file order
	Tags   map[string][]string // frame names per animation tag, in play order
	Image  string              // meta.image, the texture file the atlas expects
	Size   lin.Vec2            // meta.size, the texture size; zero if absent
}

// ParseAtlas reads a TexturePacker or Aseprite JSON atlas.
func ParseAtlas(data []byte) (*AtlasData, error) {
	var doc atlasJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("atlas: %w", err)
	}
	d := &AtlasData{Frames: map[string]AtlasFrame{}, Tags: map[string][]string{}, Image: doc.Meta.Image}
	if doc.Meta.Size != nil {
		d.Size = lin.V2(float32(doc.Meta.Size.W), float32(doc.Meta.Size.H))
	}
	if err := d.readFrames(doc.Frames); err != nil {
		return nil, fmt.Errorf("atlas: %w", err)
	}
	for _, t := range doc.Meta.FrameTags {
		d.Tags[t.Name] = tagFrames(d.Order, t)
	}
	return d, nil
}

// readFrames accepts frames as an array of entries with a filename or an
// object keyed by name, keeping the file's order either way.
func (d *AtlasData) readFrames(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return errors.New("no frames")
	}
	if raw[0] == '[' {
		var list []atlasFrameJSON
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, f := range list {
			d.add(f.Filename, f)
		}
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening brace
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, ok := tok.(string)
		if !ok {
			return fmt.Errorf("frame key %v is not a string", tok)
		}
		var f atlasFrameJSON
		if err := dec.Decode(&f); err != nil {
			return fmt.Errorf("frame %q: %w", name, err)
		}
		d.add(name, f)
	}
	if _, err := dec.Token(); err != nil && err != io.EOF { // closing brace
		return err
	}
	return nil
}

func (d *AtlasData) add(name string, f atlasFrameJSON) {
	fr := AtlasFrame{
		Rect:     lin.R(float32(f.Frame.X), float32(f.Frame.Y), float32(f.Frame.W), float32(f.Frame.H)),
		Duration: float32(f.Duration) / 1000,
		Rotated:  f.Rotated,
		Trimmed:  f.Trimmed,
	}
	if f.Rotated {
		// TexturePacker writes the upright size; the packed rectangle
		// has the axes swapped.
		fr.Rect.W, fr.Rect.H = fr.Rect.H, fr.Rect.W
	}
	if f.SpriteSourceSize != nil {
		fr.Offset = lin.V2(float32(f.SpriteSourceSize.X), float32(f.SpriteSourceSize.Y))
	}
	if f.SourceSize != nil {
		fr.SourceSize = lin.V2(float32(f.SourceSize.W), float32(f.SourceSize.H))
	} else {
		fr.SourceSize = lin.V2(float32(f.Frame.W), float32(f.Frame.H))
	}
	if _, dup := d.Frames[name]; !dup {
		d.Order = append(d.Order, name)
	}
	d.Frames[name] = fr
}

// tagFrames lists a tag's frames in play order. Aseprite's tags index
// frames by position; pingpong plays forward then back without
// repeating the ends.
func tagFrames(order []string, t atlasTagJSON) []string {
	from, to := max(t.From, 0), min(t.To, len(order)-1)
	if from > to {
		return nil
	}
	var names []string
	forward := func() {
		for i := from; i <= to; i++ {
			names = append(names, order[i])
		}
	}
	reverse := func() {
		for i := to; i >= from; i-- {
			names = append(names, order[i])
		}
	}
	switch t.Direction {
	case "reverse":
		reverse()
	case "pingpong":
		forward()
		for i := to - 1; i > from; i-- {
			names = append(names, order[i])
		}
	case "pingpong_reverse":
		reverse()
		for i := from + 1; i < to; i++ {
			names = append(names, order[i])
		}
	default:
		forward()
	}
	return names
}

// Atlas is a texture with named regions and animation tags.
type Atlas struct {
	Tex     *Texture
	Data    *AtlasData
	regions map[string]Region
}

// Bind ties the atlas to the texture its frames index.
func (d *AtlasData) Bind(tex *Texture) *Atlas {
	a := &Atlas{Tex: tex, Data: d, regions: make(map[string]Region, len(d.Frames))}
	for name, f := range d.Frames {
		a.regions[name] = NewRegion(tex, f.Rect)
	}
	return a
}

// Region returns a frame's region by name.
func (a *Atlas) Region(name string) (Region, bool) {
	r, ok := a.regions[name]
	return r, ok
}

// Names lists the frames in file order.
func (a *Atlas) Names() []string { return a.Data.Order }

// Tag returns a tag's frames as regions in play order, or nil for an
// unknown tag.
func (a *Atlas) Tag(name string) []Region {
	names := a.Data.Tags[name]
	if names == nil {
		return nil
	}
	out := make([]Region, 0, len(names))
	for _, n := range names {
		out = append(out, a.regions[n])
	}
	return out
}

// Durations returns a tag's frame durations in seconds, matching Tag.
func (a *Atlas) Durations(name string) []float32 {
	names := a.Data.Tags[name]
	if names == nil {
		return nil
	}
	out := make([]float32, 0, len(names))
	for _, n := range names {
		out = append(out, a.Data.Frames[n].Duration)
	}
	return out
}

// The shared JSON shape of TexturePacker and Aseprite atlases.

type atlasJSON struct {
	Frames json.RawMessage `json:"frames"`
	Meta   struct {
		Image     string         `json:"image"`
		Size      *atlasSize     `json:"size"`
		FrameTags []atlasTagJSON `json:"frameTags"`
	} `json:"meta"`
}

type atlasFrameJSON struct {
	Filename         string     `json:"filename"`
	Frame            atlasRect  `json:"frame"`
	Rotated          bool       `json:"rotated"`
	Trimmed          bool       `json:"trimmed"`
	SpriteSourceSize *atlasRect `json:"spriteSourceSize"`
	SourceSize       *atlasSize `json:"sourceSize"`
	Duration         float64    `json:"duration"` // milliseconds
}

type atlasRect struct {
	X, Y, W, H float64
}

type atlasSize struct {
	W, H float64
}

type atlasTagJSON struct {
	Name      string `json:"name"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Direction string `json:"direction"`
}
