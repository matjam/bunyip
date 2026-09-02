package tiled

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// Resolver fetches the bytes behind a path an external tileset names,
// relative to the map's directory.
type Resolver func(path string) ([]byte, error)

// ErrUnsupported reports a map feature this package does not read, such
// as zstd-compressed layer data or XML tilesets.
var ErrUnsupported = errors.New("tiled: unsupported")

// Load reads a .tmj or .json map, resolving external tilesets next to
// it.
func Load(name string) (*Map, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("tiled: %w", err)
	}
	dir := filepath.Dir(name)
	m, err := Parse(data, func(p string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	})
	if err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	return m, nil
}

// Parse decodes a map from memory. resolve may be nil when every tileset
// is embedded. Paths in the result (tileset images, image layers) are
// relative to the map's directory, as the resolver's are.
func Parse(data []byte, resolve Resolver) (*Map, error) {
	if isXML(data) {
		return nil, fmt.Errorf("%w: XML maps; save as JSON", ErrUnsupported)
	}
	var j jsonMap
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	m := &Map{
		Width: j.Width, Height: j.Height, TileWidth: j.TileWidth, TileHeight: j.TileHeight,
		Orientation: j.Orientation, Infinite: j.Infinite, Properties: properties(j.Properties),
	}
	if j.BackgroundColor != "" {
		if c, ok := ParseColor(j.BackgroundColor); ok {
			m.BackgroundColor = c
		}
	}
	for i, t := range j.Tilesets {
		ts, err := tileset(t, resolve)
		if err != nil {
			return nil, fmt.Errorf("tileset %d: %w", i, err)
		}
		m.Tilesets = append(m.Tilesets, ts)
	}
	sort.SliceStable(m.Tilesets, func(a, b int) bool { return m.Tilesets[a].FirstGID < m.Tilesets[b].FirstGID })
	var err error
	if m.Layers, err = layers(j.Layers); err != nil {
		return nil, err
	}
	return m, nil
}

func isXML(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte("<"))
}

func tileset(j jsonTileset, resolve Resolver) (Tileset, error) {
	dir := ""
	if j.Source != "" {
		if resolve == nil {
			return Tileset{}, fmt.Errorf("external tileset %q needs a resolver", j.Source)
		}
		data, err := resolve(j.Source)
		if err != nil {
			return Tileset{}, fmt.Errorf("%s: %w", j.Source, err)
		}
		if isXML(data) {
			return Tileset{}, fmt.Errorf("%s: %w: XML tilesets; save as JSON", j.Source, ErrUnsupported)
		}
		first, src := j.FirstGID, j.Source
		j = jsonTileset{}
		if err := json.Unmarshal(data, &j); err != nil {
			return Tileset{}, fmt.Errorf("%s: %w", src, err)
		}
		j.FirstGID = first
		dir = path.Dir(src)
	}
	ts := Tileset{
		FirstGID: j.FirstGID, Name: j.Name, Image: rebase(dir, j.Image),
		ImageWidth: j.ImageWidth, ImageHeight: j.ImageHeight,
		TileWidth: j.TileWidth, TileHeight: j.TileHeight, Columns: j.Columns, TileCount: j.TileCount,
		Margin: j.Margin, Spacing: j.Spacing, Properties: properties(j.Properties),
	}
	tiles, err := tiles(j.Tiles)
	if err != nil {
		return Tileset{}, err
	}
	for _, t := range tiles {
		tile := Tile{ID: t.ID, Properties: properties(t.Properties), Image: rebase(dir, t.Image),
			ImageWidth: t.ImageWidth, ImageHeight: t.ImageHeight}
		for _, f := range t.Animation {
			tile.Animation = append(tile.Animation, Frame{TileID: f.TileID, Duration: float32(f.Duration) / 1000})
		}
		if t.ObjectGroup != nil {
			tile.Collision = objects(t.ObjectGroup.Objects)
		}
		if ts.Tiles == nil {
			ts.Tiles = map[int]Tile{}
		}
		ts.Tiles[t.ID] = tile
	}
	return ts, nil
}

// rebase makes a path written relative to an external tileset relative
// to the map instead.
func rebase(dir, p string) string {
	if p == "" || dir == "" || dir == "." {
		return p
	}
	return path.Join(dir, p)
}

// tiles reads the tileset's tiles, an array in current files and an
// object keyed by id in older ones.
func tiles(raw json.RawMessage) ([]jsonTile, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == 'n' {
		return nil, nil
	}
	if raw[0] == '[' {
		var list []jsonTile
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("tiles: %w", err)
		}
		return list, nil
	}
	var byID map[string]jsonTile
	if err := json.Unmarshal(raw, &byID); err != nil {
		return nil, fmt.Errorf("tiles: %w", err)
	}
	var list []jsonTile
	for k, t := range byID {
		if _, err := fmt.Sscan(k, &t.ID); err != nil {
			return nil, fmt.Errorf("tiles: id %q: %w", k, err)
		}
		list = append(list, t)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].ID < list[b].ID })
	return list, nil
}

func layers(js []jsonLayer) ([]Layer, error) {
	var out []Layer
	for _, j := range js {
		l := Layer{
			ID: j.ID, Name: j.Name, Width: j.Width, Height: j.Height, Visible: true, Opacity: 1,
			OffsetX: float32(j.OffsetX), OffsetY: float32(j.OffsetY), Properties: properties(j.Properties),
		}
		if j.Visible != nil {
			l.Visible = *j.Visible
		}
		if j.Opacity != nil {
			l.Opacity = float32(*j.Opacity)
		}
		var err error
		switch j.Type {
		case "tilelayer":
			l.Kind = TileLayer
			err = tileData(&l, j)
		case "objectgroup":
			l.Kind = ObjectLayer
			l.Objects = objects(j.Objects)
		case "imagelayer":
			l.Kind = ImageLayer
			l.Image = j.Image
		case "group":
			l.Kind = GroupLayer
			l.Layers, err = layers(j.Layers)
		default:
			err = fmt.Errorf("%w: layer type %q", ErrUnsupported, j.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", j.Name, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// tileData fills a tile layer's cells from its data or, for infinite
// maps, from its chunks flattened into one grid over their bounds.
func tileData(l *Layer, j jsonLayer) error {
	if len(j.Chunks) == 0 {
		cells, err := decodeCells(j.Data, j.Encoding, j.Compression)
		if err != nil {
			return err
		}
		if len(cells) != l.Width*l.Height {
			return fmt.Errorf("%d cells for a %dx%d layer", len(cells), l.Width, l.Height)
		}
		l.Data = cells
		return nil
	}
	x0, y0 := j.Chunks[0].X, j.Chunks[0].Y
	x1, y1 := x0, y0
	for _, c := range j.Chunks {
		x0, y0 = min(x0, c.X), min(y0, c.Y)
		x1, y1 = max(x1, c.X+c.Width), max(y1, c.Y+c.Height)
	}
	l.StartX, l.StartY, l.Width, l.Height = x0, y0, x1-x0, y1-y0
	l.Data = make([]uint32, l.Width*l.Height)
	for _, c := range j.Chunks {
		cells, err := decodeCells(c.Data, j.Encoding, j.Compression)
		if err != nil {
			return fmt.Errorf("chunk (%d,%d): %w", c.X, c.Y, err)
		}
		if len(cells) != c.Width*c.Height {
			return fmt.Errorf("chunk (%d,%d): %d cells for %dx%d", c.X, c.Y, len(cells), c.Width, c.Height)
		}
		for y := range c.Height {
			row := (c.Y-y0+y)*l.Width + c.X - x0
			copy(l.Data[row:row+c.Width], cells[y*c.Width:(y+1)*c.Width])
		}
	}
	return nil
}

// decodeCells reads layer data: a JSON array of ids, or a base64 string
// of little-endian uint32s, optionally zlib or gzip compressed.
func decodeCells(raw json.RawMessage, encoding, compression string) ([]uint32, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("layer has no data")
	}
	if raw[0] == '[' {
		var cells []uint32
		if err := json.Unmarshal(raw, &cells); err != nil {
			return nil, fmt.Errorf("data: %w", err)
		}
		return cells, nil
	}
	if encoding != "base64" {
		return nil, fmt.Errorf("%w: encoding %q", ErrUnsupported, encoding)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	buf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("data: base64: %w", err)
	}
	switch compression {
	case "":
	case "zlib":
		r, err := zlib.NewReader(bytes.NewReader(buf))
		if err != nil {
			return nil, fmt.Errorf("data: zlib: %w", err)
		}
		if buf, err = io.ReadAll(r); err != nil {
			return nil, fmt.Errorf("data: zlib: %w", err)
		}
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(buf))
		if err != nil {
			return nil, fmt.Errorf("data: gzip: %w", err)
		}
		if buf, err = io.ReadAll(r); err != nil {
			return nil, fmt.Errorf("data: gzip: %w", err)
		}
	default:
		return nil, fmt.Errorf("%w: compression %q", ErrUnsupported, compression)
	}
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("data: %d bytes is not whole cells", len(buf))
	}
	cells := make([]uint32, len(buf)/4)
	for i := range cells {
		cells[i] = binary.LittleEndian.Uint32(buf[4*i:])
	}
	return cells, nil
}

func objects(js []jsonObject) []Object {
	var out []Object
	for _, j := range js {
		o := Object{
			ID: j.ID, Name: j.Name, Class: j.Class, X: float32(j.X), Y: float32(j.Y),
			Width: float32(j.Width), Height: float32(j.Height), Rotation: float32(j.Rotation),
			GID: j.GID, Visible: true, Point: j.Point, Ellipse: j.Ellipse,
			Polygon: points(j.Polygon), Polyline: points(j.Polyline), Properties: properties(j.Properties),
		}
		if o.Class == "" {
			o.Class = j.Type // the field's name before Tiled 1.9
		}
		if j.Visible != nil {
			o.Visible = *j.Visible
		}
		if j.Text != nil {
			o.Text = j.Text.Text
		}
		out = append(out, o)
	}
	return out
}

func points(js []jsonPoint) []lin.Vec2 {
	if js == nil {
		return nil
	}
	out := make([]lin.Vec2, len(js))
	for i, p := range js {
		out[i] = lin.V2(float32(p.X), float32(p.Y))
	}
	return out
}

// properties converts Tiled's typed property list. Unknown or malformed
// values are dropped rather than failing the whole map.
func properties(js []jsonProperty) Properties {
	if len(js) == 0 {
		return nil
	}
	out := Properties{}
	for _, j := range js {
		var v any
		if err := json.Unmarshal(j.Value, &v); err != nil {
			continue
		}
		if v = convertProperty(j.Type, v); v != nil {
			out[j.Name] = v
		}
	}
	return out
}

func convertProperty(kind string, v any) any {
	switch kind {
	case "int", "object":
		f, ok := v.(float64)
		if !ok {
			return nil
		}
		return int(f)
	case "float":
		f, ok := v.(float64)
		if !ok {
			return nil
		}
		return f
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil
		}
		return b
	case "class":
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		return classProperties(m)
	default: // string, color, file
		s, ok := v.(string)
		if !ok {
			return nil
		}
		return s
	}
}

// classProperties converts a class value's members, which carry no type
// names: whole numbers become int, other numbers float64.
func classProperties(m map[string]any) Properties {
	out := Properties{}
	for k, v := range m {
		switch x := v.(type) {
		case float64:
			if x == float64(int(x)) {
				out[k] = int(x)
			} else {
				out[k] = x
			}
		case map[string]any:
			out[k] = classProperties(x)
		default:
			out[k] = x
		}
	}
	return out
}

// The JSON shapes Tiled writes. Field names match the file's keys.

type jsonMap struct {
	Width           int            `json:"width"`
	Height          int            `json:"height"`
	TileWidth       int            `json:"tilewidth"`
	TileHeight      int            `json:"tileheight"`
	Orientation     string         `json:"orientation"`
	BackgroundColor string         `json:"backgroundcolor"`
	Infinite        bool           `json:"infinite"`
	Layers          []jsonLayer    `json:"layers"`
	Tilesets        []jsonTileset  `json:"tilesets"`
	Properties      []jsonProperty `json:"properties"`
}

type jsonLayer struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Data        json.RawMessage `json:"data"`
	Encoding    string          `json:"encoding"`
	Compression string          `json:"compression"`
	Chunks      []jsonChunk     `json:"chunks"`
	Objects     []jsonObject    `json:"objects"`
	Visible     *bool           `json:"visible"`
	Opacity     *float64        `json:"opacity"`
	OffsetX     float64         `json:"offsetx"`
	OffsetY     float64         `json:"offsety"`
	Image       string          `json:"image"`
	Layers      []jsonLayer     `json:"layers"`
	Properties  []jsonProperty  `json:"properties"`
}

type jsonChunk struct {
	X      int             `json:"x"`
	Y      int             `json:"y"`
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Data   json.RawMessage `json:"data"`
}

type jsonObject struct {
	ID         int            `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Class      string         `json:"class"`
	X          float64        `json:"x"`
	Y          float64        `json:"y"`
	Width      float64        `json:"width"`
	Height     float64        `json:"height"`
	Rotation   float64        `json:"rotation"`
	GID        uint32         `json:"gid"`
	Visible    *bool          `json:"visible"`
	Point      bool           `json:"point"`
	Ellipse    bool           `json:"ellipse"`
	Polygon    []jsonPoint    `json:"polygon"`
	Polyline   []jsonPoint    `json:"polyline"`
	Text       *jsonText      `json:"text"`
	Properties []jsonProperty `json:"properties"`
}

type jsonPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type jsonText struct {
	Text string `json:"text"`
}

type jsonTileset struct {
	FirstGID    uint32          `json:"firstgid"`
	Source      string          `json:"source"`
	Name        string          `json:"name"`
	Image       string          `json:"image"`
	ImageWidth  int             `json:"imagewidth"`
	ImageHeight int             `json:"imageheight"`
	TileWidth   int             `json:"tilewidth"`
	TileHeight  int             `json:"tileheight"`
	Columns     int             `json:"columns"`
	TileCount   int             `json:"tilecount"`
	Margin      int             `json:"margin"`
	Spacing     int             `json:"spacing"`
	Tiles       json.RawMessage `json:"tiles"`
	Properties  []jsonProperty  `json:"properties"`
}

type jsonTile struct {
	ID          int              `json:"id"`
	Animation   []jsonFrame      `json:"animation"`
	Properties  []jsonProperty   `json:"properties"`
	ObjectGroup *jsonObjectGroup `json:"objectgroup"`
	Image       string           `json:"image"`
	ImageWidth  int              `json:"imagewidth"`
	ImageHeight int              `json:"imageheight"`
}

type jsonFrame struct {
	TileID   int `json:"tileid"`
	Duration int `json:"duration"` // milliseconds
}

type jsonObjectGroup struct {
	Objects []jsonObject `json:"objects"`
}

type jsonProperty struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}
