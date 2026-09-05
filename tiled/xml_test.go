package tiled

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestXMLMatchesJSON parses testdata/level.tmx, the hand-written XML
// form of testMap, and expects the same Map the JSON form gives.
func TestXMLMatchesJSON(t *testing.T) {
	want := parseTestMap(t)
	got, err := Load("testdata/level.tmx")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reflect.DeepEqual(got, want) {
		return
	}
	// Narrow the report down to the part that differs.
	if !reflect.DeepEqual(got.Properties, want.Properties) {
		t.Errorf("properties:\n got %#v\nwant %#v", got.Properties, want.Properties)
	}
	if !reflect.DeepEqual(got.Tilesets, want.Tilesets) {
		t.Errorf("tilesets:\n got %#v\nwant %#v", got.Tilesets, want.Tilesets)
	}
	if len(got.Layers) != len(want.Layers) {
		t.Fatalf("%d layers, want %d", len(got.Layers), len(want.Layers))
	}
	for i := range want.Layers {
		if !reflect.DeepEqual(got.Layers[i], want.Layers[i]) {
			t.Errorf("layer %d:\n got %#v\nwant %#v", i, got.Layers[i], want.Layers[i])
		}
	}
	got.Properties, want.Properties = nil, nil
	got.Tilesets, want.Tilesets = nil, nil
	got.Layers, want.Layers = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("header:\n got %+v\nwant %+v", *got, *want)
	}
}

func TestXMLTileElements(t *testing.T) {
	src := `<map width="2" height="2" tilewidth="8" tileheight="8">
	  <layer id="1" name="l" width="2" height="2">
	    <data><tile gid="1"/><tile/><tile gid="2147483650"/><tile gid="3"/></data>
	  </layer>
	</map>`
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{1, 0, 2 | FlipX, 3}; !slices.Equal(m.Layers[0].Data, want) {
		t.Errorf("data %v, want %v", m.Layers[0].Data, want)
	}
}

func TestXMLInfiniteChunks(t *testing.T) {
	src := `<map width="4" height="2" tilewidth="8" tileheight="8" infinite="1">
	  <layer id="1" name="l" width="4" height="2">
	    <data encoding="csv">
	      <chunk x="-2" y="-2" width="2" height="2">
1,2,
3,4
</chunk>
	      <chunk x="2" y="0" width="2" height="2">
5,6,
7,8
</chunk>
	    </data>
	  </layer>
	</map>`
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Infinite {
		t.Error("infinite flag")
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
}

func TestXMLBase64Chunks(t *testing.T) {
	src := fmt.Sprintf(`<map width="2" height="2" tilewidth="8" tileheight="8" infinite="1">
	  <layer id="1" name="l" width="2" height="2">
	    <data encoding="base64" compression="gzip">
	      <chunk x="0" y="0" width="2" height="2">%s</chunk>
	    </data>
	  </layer>
	</map>`, encode(t, []uint32{1, 2, 3, 4}, "gzip"))
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{1, 2, 3, 4}; !slices.Equal(m.Layers[0].Data, want) {
		t.Errorf("data %v", m.Layers[0].Data)
	}
}

func TestXMLProperties(t *testing.T) {
	src := `<map width="1" height="1" tilewidth="8" tileheight="8">
	  <properties>
	    <property name="note" type="string">line one
line two</property>
	    <property name="target" type="object" value="7"/>
	    <property name="broken" type="int" value="x"/>
	    <property name="path" type="file" value="a/b.png"/>
	    <property name="stats" type="class" propertytype="Stats">
	      <properties>
	        <property name="hp" type="int" value="3"/>
	        <property name="inner" type="class" propertytype="Inner">
	          <properties><property name="on" type="bool" value="false"/></properties>
	        </property>
	      </properties>
	    </property>
	  </properties>
	  <objectgroup id="1" name="o">
	    <object id="1" name="a" class="door" x="1" y="2"/>
	    <object id="2" name="b" type="key" x="1" y="2">
	      <properties><property name="n" type="float" value="1.5"/></properties>
	    </object>
	  </objectgroup>
	  <imagelayer id="2" name="bg" offsetx="3" offsety="4">
	    <image source="img/bg.png" width="8" height="8"/>
	  </imagelayer>
	</map>`
	m, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := m.Properties
	if p.String("note") != "line one\nline two" {
		t.Errorf("multi-line string %q", p.String("note"))
	}
	if p.Int("target") != 7 || p.String("path") != "a/b.png" || p.Has("broken") {
		t.Errorf("properties: %v", p)
	}
	stats := p.Class("stats")
	if stats.Int("hp") != 3 || stats.Class("inner").Has("on") != true || stats.Class("inner").Bool("on") {
		t.Errorf("nested class: %v", stats)
	}
	objs := m.Layers[0].Objects
	if objs[0].Class != "door" || objs[1].Class != "key" || objs[1].Properties.Float("n") != 1.5 {
		t.Errorf("objects: %+v", objs)
	}
	bg := m.Layers[1]
	if bg.Kind != ImageLayer || bg.Image != "img/bg.png" || bg.OffsetX != 3 || bg.OffsetY != 4 {
		t.Errorf("image layer: %+v", bg)
	}
}

func TestXMLErrors(t *testing.T) {
	layer := func(data string) string {
		return `<map width="1" height="1"><layer name="l" width="1" height="1">` + data + `</layer></map>`
	}
	cases := []struct {
		name string
		src  string
		want error
	}{
		{"unknown compression", layer(`<data encoding="base64" compression="lzma">` + encode(t, []uint32{1}, "") + `</data>`), ErrUnsupported},
		{"unknown encoding", layer(`<data encoding="hex">01</data>`), ErrUnsupported},
		{"tileset root", `<tileset name="t"/>`, nil},
		{"bad csv", layer(`<data encoding="csv">1,x</data>`), nil},
		{"short csv", layer(`<data encoding="csv">1,2</data>`), nil},
		{"no data", layer(``), nil},
		{"bad base64", layer(`<data encoding="base64">!!</data>`), nil},
		{"bad polygon", `<map><objectgroup name="o"><object id="1"><polygon points="1"/></object></objectgroup></map>`, nil},
		{"bad polyline", `<map><objectgroup name="o"><object id="1"><polyline points="1,y"/></object></objectgroup></map>`, nil},
		{"external without resolver", `<map><tileset firstgid="1" source="x.tsx"/></map>`, nil},
		{"unclosed", `<map><layer name="l">`, nil},
		{"bad attribute", `<map width="wide"/>`, nil},
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

func TestParseTileset(t *testing.T) {
	tsx, err := os.ReadFile("testdata/sets/props.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, src string }{{"xml", string(tsx)}, {"json", propsTileset}} {
		ts, err := ParseTileset([]byte(c.src))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if ts.FirstGID != 0 || ts.Name != "props" || ts.Image != "props.png" || ts.ImageWidth != 36 ||
			ts.Columns != 2 || ts.TileCount != 2 || ts.Margin != 1 || ts.Spacing != 2 {
			t.Errorf("%s: %+v", c.name, *ts)
		}
		if ts.Tiles[1].Properties.Float("weight") != 2.5 {
			t.Errorf("%s: tile properties %v", c.name, ts.Tiles[1].Properties)
		}
	}
	for _, src := range []string{`<map/>`, `{`, `<tileset`, ``} {
		if _, err := ParseTileset([]byte(src)); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}

// TestCrossFormatTilesets checks that a map in one form may name an
// external tileset in the other.
func TestCrossFormatTilesets(t *testing.T) {
	tsx, err := os.ReadFile("testdata/sets/props.tsx")
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(p string) ([]byte, error) {
		switch p {
		case "sets/props.tsx":
			return tsx, nil
		case "sets/props.tsj":
			return []byte(propsTileset), nil
		}
		return nil, errors.New("not found")
	}
	cases := []struct{ name, src string }{
		{"json map, xml tileset", `{"tilesets": [{"firstgid": 5, "source": "sets/props.tsx"}]}`},
		{"xml map, json tileset", `<map><tileset firstgid="5" source="sets/props.tsj"/></map>`},
	}
	for _, c := range cases {
		m, err := Parse([]byte(c.src), resolve)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		ts := m.Tilesets[0]
		if ts.FirstGID != 5 || ts.Name != "props" || ts.Image != "sets/props.png" || ts.Tiles[1].Properties.Float("weight") != 2.5 {
			t.Errorf("%s: %+v", c.name, ts)
		}
	}
}

func TestXMLTilesetDetails(t *testing.T) {
	src := `<tileset name="things" tilewidth="8" tileheight="8" tilecount="2" columns="0">
	  <properties><property name="kind" value="props"/></properties>
	  <tile id="0" type="crate">
	    <image source="crate.png" width="8" height="8"/>
	    <objectgroup><object id="1" x="1" y="1" width="6" height="6"><ellipse/></object></objectgroup>
	  </tile>
	  <tile id="1">
	    <image source="fire.png" width="8" height="8"/>
	    <animation><frame tileid="1" duration="100"/><frame tileid="0" duration="200"/></animation>
	  </tile>
	  <wangsets><wangset name="w" type="corner" tile="-1">
	    <wangcolor name="grass" color="#00ff00" tile="0" probability="0.5"/>
	    <wangcolor name="dirt" color="#804000" tile="1" probability="1"/>
	    <wangtile tileid="0" wangid="0,1,0,2,0,1,0,1"/>
	    <wangtile tileid="1" wangid="0,2,0,2,0,2,0,2"/>
	  </wangset></wangsets>
	</tileset>`
	ts, err := ParseTileset([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if ts.Image != "" || ts.Properties.String("kind") != "props" || len(ts.Tiles) != 2 {
		t.Errorf("tileset: %+v", *ts)
	}
	crate := ts.Tiles[0]
	if crate.Image != "crate.png" || crate.ImageWidth != 8 || len(crate.Collision) != 1 || !crate.Collision[0].Ellipse ||
		crate.Collision[0].Rect() != lin.R(1, 1, 6, 6) {
		t.Errorf("crate: %+v", crate)
	}
	fire := ts.Tiles[1]
	if want := []Frame{{TileID: 1, Duration: 0.1}, {TileID: 0, Duration: 0.2}}; !slices.Equal(fire.Animation, want) {
		t.Errorf("animation %v, want %v", fire.Animation, want)
	}
	if len(ts.WangSets) != 1 {
		t.Fatalf("wangsets: %+v", ts.WangSets)
	}
	ws := ts.WangSets[0]
	if ws.Name != "w" || ws.Type != "corner" || len(ws.Colors) != 2 || len(ws.Tiles) != 2 {
		t.Fatalf("wangset: %+v", ws)
	}
	if ws.Colors[0].Name != "grass" || ws.Colors[0].Probability != 0.5 || ws.Colors[0].Color.G != 255 {
		t.Errorf("colour: %+v", ws.Colors[0])
	}
	if ws.Tiles[0].WangID != [8]int{0, 1, 0, 2, 0, 1, 0, 1} {
		t.Errorf("wangid: %v", ws.Tiles[0].WangID)
	}
	if r := ws.Rules(); r == nil {
		t.Error("no rules from the set")
	}
}
