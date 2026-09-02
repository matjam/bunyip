package tiled

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// encode packs cells the way Tiled's base64 layer data does.
func encode(t *testing.T, cells []uint32, compression string) string {
	t.Helper()
	raw := make([]byte, 4*len(cells))
	for i, c := range cells {
		binary.LittleEndian.PutUint32(raw[4*i:], c)
	}
	var buf bytes.Buffer
	switch compression {
	case "":
		buf.Write(raw)
	case "zlib":
		w := zlib.NewWriter(&buf)
		w.Write(raw)
		w.Close()
	case "gzip":
		w := gzip.NewWriter(&buf)
		w.Write(raw)
		w.Close()
	default:
		t.Fatalf("encode: unknown compression %q", compression)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

const propsTileset = `{
  "name": "props", "image": "props.png", "imagewidth": 36, "imageheight": 18,
  "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 2, "margin": 1, "spacing": 2,
  "tiles": [{"id": 1, "properties": [{"name": "weight", "type": "float", "value": 2.5}]}]
}`

func testMap(t *testing.T) string {
	t.Helper()
	decor := encode(t, []uint32{0, 4 | FlipX, 4 | FlipY, 4 | FlipDiag, 0, 0, 0, 0, 0, 0, 0, 4 | FlipX | FlipY}, "zlib")
	overlay := encode(t, []uint32{9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10}, "")
	return fmt.Sprintf(`{
  "width": 4, "height": 3, "tilewidth": 16, "tileheight": 16, "orientation": "orthogonal",
  "backgroundcolor": "#80336699", "infinite": false,
  "properties": [
    {"name": "difficulty", "type": "int", "value": 3},
    {"name": "title", "type": "string", "value": "Test"},
    {"name": "gravity", "type": "float", "value": 9.5},
    {"name": "dark", "type": "bool", "value": true},
    {"name": "tint", "type": "color", "value": "#80ff0000"},
    {"name": "spawn", "type": "class", "propertytype": "Spawn", "value": {"hp": 10, "rate": 0.5, "name": "orc"}}
  ],
  "tilesets": [
    {"firstgid": 9, "source": "sets/props.tsj"},
    {"firstgid": 1, "name": "ground", "image": "img/ground.png", "imagewidth": 64, "imageheight": 32,
     "tilewidth": 16, "tileheight": 16, "columns": 4, "tilecount": 8, "margin": 0, "spacing": 0,
     "tiles": [
       {"id": 4, "animation": [{"tileid": 4, "duration": 250}, {"tileid": 5, "duration": 500}]},
       {"id": 2, "properties": [{"name": "solid", "type": "bool", "value": true}],
        "objectgroup": {"objects": [{"id": 1, "x": 0, "y": 8, "width": 16, "height": 8}]}}
     ]}
  ],
  "layers": [
    {"id": 1, "name": "ground", "type": "tilelayer", "width": 4, "height": 3, "visible": true, "opacity": 1,
     "data": [1, 2, 3, 4, 5, 5, 5, 5, 9, 10, 0, 3]},
    {"id": 2, "name": "decor", "type": "tilelayer", "width": 4, "height": 3,
     "encoding": "base64", "compression": "zlib", "data": "%s"},
    {"id": 3, "name": "things", "type": "objectgroup", "objects": [
      {"id": 1, "name": "zone", "type": "trigger", "x": 10, "y": 20, "width": 0, "height": 0, "rotation": 0, "visible": true,
       "polygon": [{"x": 0, "y": 0}, {"x": 32, "y": 0}, {"x": 16, "y": 24}],
       "properties": [{"name": "target", "type": "string", "value": "exit"}, {"name": "count", "type": "int", "value": 2}]},
      {"id": 2, "name": "spawn", "class": "spawn", "point": true, "x": 5, "y": 6},
      {"id": 3, "name": "crate", "gid": 2147483650, "x": 16, "y": 32, "width": 16, "height": 16, "rotation": 45},
      {"id": 4, "name": "pool", "ellipse": true, "x": 1, "y": 2, "width": 8, "height": 4, "visible": false},
      {"id": 5, "name": "path", "polyline": [{"x": 0, "y": 0}, {"x": 4, "y": 4}], "x": 0, "y": 0},
      {"id": 6, "name": "sign", "text": {"text": "hello", "wrap": true}, "x": 3, "y": 3, "width": 20, "height": 10}
    ]},
    {"id": 4, "name": "fx", "type": "group", "opacity": 0.5, "offsetx": 1, "layers": [
      {"id": 5, "name": "overlay", "type": "tilelayer", "width": 4, "height": 3, "visible": false, "opacity": 0.5,
       "offsetx": 4, "offsety": -2, "encoding": "base64", "data": "%s",
       "properties": [{"name": "parallax", "type": "float", "value": 0.5}]}
    ]}
  ]
}`, decor, overlay)
}

func resolver(t *testing.T) Resolver {
	return func(p string) ([]byte, error) {
		if p != "sets/props.tsj" {
			t.Errorf("resolver asked for %q", p)
			return nil, errors.New("not found")
		}
		return []byte(propsTileset), nil
	}
}

func parseTestMap(t *testing.T) *Map {
	t.Helper()
	m, err := Parse([]byte(testMap(t)), resolver(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func TestMapHeader(t *testing.T) {
	m := parseTestMap(t)
	if m.Width != 4 || m.Height != 3 || m.TileWidth != 16 || m.TileHeight != 16 || m.Orientation != "orthogonal" {
		t.Errorf("header: %+v", *m)
	}
	if want := (color.RGBA{0x33, 0x66, 0x99, 0x80}); m.BackgroundColor != want {
		t.Errorf("background %v, want %v", m.BackgroundColor, want)
	}
	p := m.Properties
	if p.Int("difficulty") != 3 || p.String("title") != "Test" || p.Float("gravity") != 9.5 || !p.Bool("dark") {
		t.Errorf("properties: %v", p)
	}
	if want := (color.RGBA{255, 0, 0, 0x80}); p.Color("tint") != want {
		t.Errorf("tint %v, want %v", p.Color("tint"), want)
	}
	if c := p.Class("spawn"); c.Int("hp") != 10 || c.Float("rate") != 0.5 || c.String("name") != "orc" {
		t.Errorf("class property: %v", c)
	}
	if p.Has("missing") || p.Int("missing") != 0 || p.String("missing") != "" {
		t.Error("missing property should be zero")
	}
}

func TestTilesets(t *testing.T) {
	m := parseTestMap(t)
	if len(m.Tilesets) != 2 {
		t.Fatalf("%d tilesets", len(m.Tilesets))
	}
	g, p := m.Tilesets[0], m.Tilesets[1]
	if g.FirstGID != 1 || p.FirstGID != 9 {
		t.Errorf("tilesets not sorted by first gid: %d, %d", g.FirstGID, p.FirstGID)
	}
	if g.Name != "ground" || g.Image != "img/ground.png" || g.ImageWidth != 64 || g.ImageHeight != 32 ||
		g.TileWidth != 16 || g.TileHeight != 16 || g.Columns != 4 || g.TileCount != 8 {
		t.Errorf("embedded tileset: %+v", g)
	}
	if p.Name != "props" || p.Image != "sets/props.png" || p.ImageWidth != 36 || p.ImageHeight != 18 ||
		p.Columns != 2 || p.TileCount != 2 || p.Margin != 1 || p.Spacing != 2 {
		t.Errorf("external tileset: %+v", p)
	}
	if p.Tiles[1].Properties.Float("weight") != 2.5 {
		t.Errorf("external tile properties: %v", p.Tiles[1].Properties)
	}
	anim := g.Tiles[4].Animation
	want := []Frame{{TileID: 4, Duration: 0.25}, {TileID: 5, Duration: 0.5}}
	if !slices.Equal(anim, want) {
		t.Errorf("animation %v, want %v", anim, want)
	}
	solid := g.Tiles[2]
	if !solid.Properties.Bool("solid") || solid.ID != 2 {
		t.Errorf("tile 2: %+v", solid)
	}
	if len(solid.Collision) != 1 || solid.Collision[0].Rect() != lin.R(0, 8, 16, 8) {
		t.Errorf("collision: %+v", solid.Collision)
	}
	if !m.TileProperties(3).Bool("solid") || m.TileProperties(1) != nil {
		t.Error("TileProperties should follow the gid to the tile")
	}
}

func TestTilesetLookup(t *testing.T) {
	m := parseTestMap(t)
	cases := []struct {
		name  string
		gid   uint32
		set   string
		local int
	}{
		{"empty", 0, "", -1},
		{"first of ground", 1, "ground", 0},
		{"last of ground", 8, "ground", 7},
		{"first of props", 9, "props", 0},
		{"flipped", 10 | FlipX | FlipDiag, "props", 1},
		{"past the end", 11, "", -1},
	}
	for _, c := range cases {
		ts, local := m.Tileset(c.gid)
		name := ""
		if ts != nil {
			name = ts.Name
		}
		if name != c.set || local != c.local {
			t.Errorf("%s: got %q/%d, want %q/%d", c.name, name, local, c.set, c.local)
		}
	}
}

func TestLayers(t *testing.T) {
	m := parseTestMap(t)
	if len(m.Layers) != 4 {
		t.Fatalf("%d layers", len(m.Layers))
	}
	ground := m.Layers[0]
	if ground.Kind != TileLayer || ground.Name != "ground" || ground.ID != 1 || !ground.Visible || ground.Opacity != 1 {
		t.Errorf("ground: %+v", ground)
	}
	if want := []uint32{1, 2, 3, 4, 5, 5, 5, 5, 9, 10, 0, 3}; !slices.Equal(ground.Data, want) {
		t.Errorf("ground data %v", ground.Data)
	}
	if ground.CellAt(1, 2) != 10 || ground.CellAt(4, 0) != 0 || ground.CellAt(-1, 0) != 0 {
		t.Error("CellAt")
	}
	decor := m.Layers[1]
	want := []uint32{0, 4 | FlipX, 4 | FlipY, 4 | FlipDiag, 0, 0, 0, 0, 0, 0, 0, 4 | FlipX | FlipY}
	if !slices.Equal(decor.Data, want) {
		t.Errorf("decor data %v, want %v", decor.Data, want)
	}
	id, fx, fy, d := SplitGID(decor.CellAt(3, 2))
	if id != 4 || !fx || !fy || d {
		t.Errorf("SplitGID: %d %v %v %v", id, fx, fy, d)
	}
	if _, _, _, d := SplitGID(decor.CellAt(3, 0)); !d {
		t.Error("diagonal flip lost")
	}
	group := m.Layers[3]
	if group.Kind != GroupLayer || len(group.Layers) != 1 || group.Opacity != 0.5 || group.OffsetX != 1 {
		t.Errorf("group: %+v", group)
	}
	overlay := group.Layers[0]
	if overlay.Visible || overlay.Opacity != 0.5 || overlay.OffsetX != 4 || overlay.OffsetY != -2 ||
		overlay.Properties.Float("parallax") != 0.5 {
		t.Errorf("overlay: %+v", overlay)
	}
	if overlay.Data[0] != 9 || overlay.Data[11] != 10 {
		t.Errorf("overlay data %v", overlay.Data)
	}
	tiles := m.TileLayers()
	if len(tiles) != 3 || tiles[2].Name != "overlay" {
		t.Errorf("TileLayers: %d, last %q", len(tiles), tiles[len(tiles)-1].Name)
	}
	if objs := m.ObjectLayers(); len(objs) != 1 || objs[0].Name != "things" {
		t.Errorf("ObjectLayers: %v", objs)
	}
	if m.FindLayer("overlay") == nil || m.FindLayer("nope") != nil {
		t.Error("FindLayer")
	}
}

func TestObjects(t *testing.T) {
	m := parseTestMap(t)
	objs := m.Layers[2].Objects
	if len(objs) != 6 {
		t.Fatalf("%d objects", len(objs))
	}
	zone := objs[0]
	if zone.ID != 1 || zone.Name != "zone" || zone.Class != "trigger" || zone.X != 10 || zone.Y != 20 || !zone.Visible {
		t.Errorf("zone: %+v", zone)
	}
	if want := []lin.Vec2{lin.V2(0, 0), lin.V2(32, 0), lin.V2(16, 24)}; !slices.Equal(zone.Polygon, want) {
		t.Errorf("polygon %v", zone.Polygon)
	}
	if zone.Properties.String("target") != "exit" || zone.Properties.Int("count") != 2 {
		t.Errorf("zone properties %v", zone.Properties)
	}
	spawn := objs[1]
	if !spawn.Point || spawn.Class != "spawn" || spawn.X != 5 || spawn.Y != 6 {
		t.Errorf("spawn: %+v", spawn)
	}
	crate := objs[2]
	id, fx, _, _ := SplitGID(crate.GID)
	if id != 2 || !fx || crate.Rotation != 45 || crate.Rect() != lin.R(16, 32, 16, 16) {
		t.Errorf("crate: %+v", crate)
	}
	if ts, local := m.Tileset(crate.GID); ts == nil || ts.Name != "ground" || local != 1 {
		t.Error("tile object gid should resolve")
	}
	pool := objs[3]
	if !pool.Ellipse || pool.Visible || pool.Width != 8 {
		t.Errorf("pool: %+v", pool)
	}
	if path := objs[4]; !slices.Equal(path.Polyline, []lin.Vec2{lin.V2(0, 0), lin.V2(4, 4)}) || path.Polygon != nil {
		t.Errorf("path: %+v", path)
	}
	if sign := objs[5]; sign.Text != "hello" {
		t.Errorf("sign: %+v", sign)
	}
}

func TestInfiniteChunks(t *testing.T) {
	a := encode(t, []uint32{1, 2, 3, 4}, "gzip")
	b := encode(t, []uint32{5, 6, 7, 8}, "gzip")
	src := fmt.Sprintf(`{"width": 4, "height": 2, "tilewidth": 8, "tileheight": 8, "infinite": true,
	  "tilesets": [{"firstgid": 1, "name": "t", "image": "t.png", "tilewidth": 8, "tileheight": 8, "columns": 4, "tilecount": 8}],
	  "layers": [{"name": "l", "type": "tilelayer", "encoding": "base64", "compression": "gzip", "chunks": [
	    {"x": -2, "y": -2, "width": 2, "height": 2, "data": "%s"},
	    {"x": 2, "y": 0, "width": 2, "height": 2, "data": "%s"}]}]}`, a, b)
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	l := m.Layers[0]
	if l.StartX != -2 || l.StartY != -2 || l.Width != 6 || l.Height != 4 {
		t.Fatalf("bounds: start (%d,%d) size %dx%d", l.StartX, l.StartY, l.Width, l.Height)
	}
	want := []uint32{
		1, 2, 0, 0, 0, 0,
		3, 4, 0, 0, 0, 0,
		0, 0, 0, 0, 5, 6,
		0, 0, 0, 0, 7, 8,
	}
	if !slices.Equal(l.Data, want) {
		t.Errorf("flattened %v", l.Data)
	}
	if l.CellAt(-2, -2) != 1 || l.CellAt(3, 1) != 8 || l.CellAt(0, 0) != 0 {
		t.Error("CellAt with negative start")
	}
}

func TestErrors(t *testing.T) {
	zstd := fmt.Sprintf(`{"width": 1, "height": 1, "layers": [{"name": "l", "type": "tilelayer", "width": 1, "height": 1,
	  "encoding": "base64", "compression": "zstd", "data": "%s"}]}`, encode(t, []uint32{1}, ""))
	cases := []struct {
		name string
		src  string
		want error
	}{
		{"zstd", zstd, ErrUnsupported},
		{"xml tileset as map", `<?xml version="1.0"?><tileset name="t"/>`, nil},
		{"external without resolver", `{"tilesets": [{"firstgid": 1, "source": "x.tsj"}]}`, nil},
		{"short data", `{"layers": [{"name": "l", "type": "tilelayer", "width": 2, "height": 2, "data": [1]}]}`, nil},
		{"bad json", `{`, nil},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src), nil)
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v is not %v", c.name, err, c.want)
		}
	}
}

func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		ok   bool
	}{
		{"#ff8000", color.RGBA{255, 128, 0, 255}, true},
		{"#80ff8000", color.RGBA{255, 128, 0, 128}, true},
		{"FF8000", color.RGBA{255, 128, 0, 255}, true},
		{"#abc", color.RGBA{}, false},
		{"#gg0000", color.RGBA{}, false},
		{"", color.RGBA{}, false},
	}
	for _, c := range cases {
		got, ok := ParseColor(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestOldTilesFormat(t *testing.T) {
	src := `{"tilesets": [{"firstgid": 1, "name": "t", "image": "t.png", "tilewidth": 8, "tileheight": 8, "columns": 1, "tilecount": 2,
	  "tiles": {"1": {"image": "b.png", "imagewidth": 8, "imageheight": 8}}}], "layers": []}`
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tile := m.Tilesets[0].Tiles[1]; tile.ID != 1 || tile.Image != "b.png" || tile.ImageWidth != 8 {
		t.Errorf("tile: %+v", tile)
	}
}
