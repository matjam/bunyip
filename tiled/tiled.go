// Package tiled reads maps saved by the Tiled editor and builds
// drawable levels from them. It decodes the JSON form (.tmj or .json
// maps, .tsj tilesets) and the XML form (.tmx maps, .tsx tilesets) into
// plain Go types. Parse distinguishes the forms by their first byte, and
// a map in one form may name an external tileset in the other. Parse and
// Load need no GPU. Build turns a Map into gfx tilemaps for drawing.
//
// A Map has tile layers (CSV or base64 with zlib or gzip compression,
// flipped and rotated tiles), object layers (rectangles, ellipses,
// points, polygons and polylines with their names, types and custom
// properties, for spawn points, triggers and collision shapes), image
// layers and groups, and tilesets embedded or external with per-tile
// animations, collision shapes and properties. Build loads the
// tilesets' images, makes one gfx.Tilemap per tile layer with the
// animations wired up, and returns a Level whose Draw draws the layers
// in order. Keep the object layers to place your own entities.
// Properties are typed (string, int, float, bool, colour, file, object)
// and read with the accessors on Properties.
package tiled

import (
	"image/color"
	"sort"

	"github.com/matjam/bunyip/lin"
)

// Map is one Tiled map: its grid, layers in draw order, and tilesets.
type Map struct {
	Width, Height         int // in tiles; the chunk bounds for infinite maps
	TileWidth, TileHeight int // in pixels
	Orientation           string
	BackgroundColor       color.RGBA // zero when the map has none
	Infinite              bool
	Layers                []Layer
	Tilesets              []Tileset // sorted by FirstGID
	Properties            Properties
}

// LayerKind is what a layer holds.
type LayerKind int

const (
	TileLayer LayerKind = iota
	ObjectLayer
	ImageLayer
	GroupLayer
)

// Layer is one layer of a map. A tile layer holds Data; an object layer
// holds Objects; an image layer names an Image; a group holds Layers.
type Layer struct {
	ID            int
	Name          string
	Kind          LayerKind
	Width, Height int // tile layers, in tiles
	// StartX and StartY are the map coordinates of Data[0]; they are
	// zero except for infinite maps, whose chunks are flattened into
	// Data and may begin at negative coordinates.
	StartX, StartY int
	// Data holds one global id per cell, row-major, with Tiled's flip
	// bits still set; zero is empty. SplitGID separates the parts.
	Data       []uint32
	Objects    []Object
	Image      string // image layers, relative to the map's directory
	Layers     []Layer
	Visible    bool
	Opacity    float32
	OffsetX    float32
	OffsetY    float32
	Properties Properties
}

// Tileset is a grid of tiles cut from one image, or a collection of
// tile images when Image is empty.
type Tileset struct {
	FirstGID              uint32
	Name                  string
	Image                 string // relative to the map's directory
	ImageWidth            int
	ImageHeight           int
	TileWidth, TileHeight int
	Columns               int
	TileCount             int
	Margin, Spacing       int
	Tiles                 map[int]Tile // by local id; only tiles with extra data
	Properties            Properties
}

// Tile is the extra data a tileset attaches to one of its tiles.
type Tile struct {
	ID          int
	Animation   []Frame
	Properties  Properties
	Collision   []Object // from the tile's object group
	Image       string   // image-collection tilesets
	ImageWidth  int
	ImageHeight int
}

// Frame is one step of a tile animation.
type Frame struct {
	TileID   int
	Duration float32 // seconds
}

// Object is a shape placed on an object layer or a tile's collision
// group. X and Y are pixels; a tile object (GID set) is anchored at its
// bottom-left, everything else at its top-left.
type Object struct {
	ID       int
	Name     string
	Class    string
	X, Y     float32
	Width    float32
	Height   float32
	Rotation float32 // degrees, clockwise
	GID      uint32  // tile objects, with flip bits
	Visible  bool
	Point    bool
	Ellipse  bool
	Polygon  []lin.Vec2 // closed, relative to X and Y
	Polyline []lin.Vec2 // open, relative to X and Y
	Text     string
	// Properties are the object's custom properties.
	Properties Properties
}

// Rect is the object's bounding rectangle.
func (o Object) Rect() lin.Rect { return lin.R(o.X, o.Y, o.Width, o.Height) }

// Properties are custom properties by name. Values are bool, int,
// float64, string, or Properties for class values; colours and files are
// strings.
type Properties map[string]any

// Has reports whether a property is set.
func (p Properties) Has(name string) bool { _, ok := p[name]; return ok }

// Bool returns a bool property, or false.
func (p Properties) Bool(name string) bool { b, _ := p[name].(bool); return b }

// Int returns an int property, or zero. Float values are truncated.
func (p Properties) Int(name string) int {
	switch v := p[name].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

// Float returns a float property, or zero. Int values are converted.
func (p Properties) Float(name string) float64 {
	switch v := p[name].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// String returns a string property, or "".
func (p Properties) String(name string) string { s, _ := p[name].(string); return s }

// Color returns a colour property, or zero.
func (p Properties) Color(name string) color.RGBA {
	c, _ := ParseColor(p.String(name))
	return c
}

// Class returns a nested class property, or nil.
func (p Properties) Class(name string) Properties { c, _ := p[name].(Properties); return c }

// ParseColor reads Tiled's #AARRGGBB or #RRGGBB colour text.
func ParseColor(s string) (color.RGBA, bool) {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{}, false
	}
	var v [4]uint8
	for i := range len(s) / 2 {
		hi, ok1 := hexDigit(s[2*i])
		lo, ok2 := hexDigit(s[2*i+1])
		if !ok1 || !ok2 {
			return color.RGBA{}, false
		}
		v[i] = hi<<4 | lo
	}
	if len(s) == 6 {
		return color.RGBA{R: v[0], G: v[1], B: v[2], A: 255}, true
	}
	return color.RGBA{R: v[1], G: v[2], B: v[3], A: v[0]}, true
}

func hexDigit(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// Flip bits Tiled stores above the tile id in a global id.
const (
	FlipX    uint32 = 0x80000000
	FlipY    uint32 = 0x40000000
	FlipDiag uint32 = 0x20000000
	flipHex  uint32 = 0x10000000 // hexagonal rotation, not carried
)

// SplitGID separates a global id into the tile id and its flip bits.
func SplitGID(gid uint32) (id uint32, flipX, flipY, diagonal bool) {
	return gid & (flipHex - 1), gid&FlipX != 0, gid&FlipY != 0, gid&FlipDiag != 0
}

// Tileset finds the tileset a global id belongs to and the tile's local
// id within it. An empty cell (zero) or an id past every tileset gives
// nil and -1.
func (m *Map) Tileset(gid uint32) (*Tileset, int) {
	id, _, _, _ := SplitGID(gid)
	if id == 0 {
		return nil, -1
	}
	i := sort.Search(len(m.Tilesets), func(i int) bool { return m.Tilesets[i].FirstGID > id }) - 1
	if i < 0 {
		return nil, -1
	}
	ts := &m.Tilesets[i]
	local := int(id - ts.FirstGID)
	if ts.TileCount > 0 && local >= ts.TileCount {
		return nil, -1
	}
	return ts, local
}

// TileLayers returns every tile layer in draw order, descending into
// groups.
func (m *Map) TileLayers() []*Layer { return collect(m.Layers, TileLayer, nil) }

// ObjectLayers returns every object layer in order, descending into
// groups.
func (m *Map) ObjectLayers() []*Layer { return collect(m.Layers, ObjectLayer, nil) }

// FindLayer returns the first layer with the name at any depth, or nil.
func (m *Map) FindLayer(name string) *Layer { return find(m.Layers, name) }

func collect(layers []Layer, kind LayerKind, out []*Layer) []*Layer {
	for i := range layers {
		l := &layers[i]
		if l.Kind == kind {
			out = append(out, l)
		}
		if l.Kind == GroupLayer {
			out = collect(l.Layers, kind, out)
		}
	}
	return out
}

func find(layers []Layer, name string) *Layer {
	for i := range layers {
		if layers[i].Name == name {
			return &layers[i]
		}
		if l := find(layers[i].Layers, name); l != nil {
			return l
		}
	}
	return nil
}

// CellAt returns the global id at map coordinates, or zero outside the
// layer or on a layer without tiles.
func (l *Layer) CellAt(x, y int) uint32 {
	x, y = x-l.StartX, y-l.StartY
	if x < 0 || y < 0 || x >= l.Width || y >= l.Height || y*l.Width+x >= len(l.Data) {
		return 0
	}
	return l.Data[y*l.Width+x]
}

// TileProperties returns the custom properties of the tile behind a
// global id, or nil when the tile has none.
func (m *Map) TileProperties(gid uint32) Properties {
	ts, local := m.Tileset(gid)
	if ts == nil {
		return nil
	}
	return ts.Tiles[local].Properties
}
