package tiled

import (
	"errors"
	"os"
	"testing"
)

var errNotFound = errors.New("not found")

// FuzzParseXML feeds the map parser corrupt XML with a resolver that
// finds nothing: an error, never a panic.
func FuzzParseXML(f *testing.F) {
	if tmx, err := os.ReadFile("testdata/level.tmx"); err == nil {
		f.Add(tmx)
	}
	f.Add([]byte(`<map width="2" height="1" tilewidth="8" tileheight="8"><layer name="a" width="2" height="1"><data encoding="csv">1,2</data></layer></map>`))
	f.Add([]byte(`<map infinite="1"><layer name="a"><data encoding="base64" compression="zlib"><chunk x="0" y="0" width="1" height="1">eJw=</chunk></data></layer></map>`))
	f.Add([]byte(`<map><layer name="a" width="1" height="1"><data><tile gid="1"/></data></layer></map>`))
	f.Add([]byte(`<map><group><objectgroup><object id="1"><polygon points="0,0 1,1"/><text>hi</text></object></objectgroup></group></map>`))
	f.Add([]byte(`<map><tileset firstgid="1" source="t.tsx"/></map>`))
	f.Add([]byte(`<map><properties><property name="c" type="class"><properties><property name="n" type="int" value="1"/></properties></property></properties></map>`))
	f.Add([]byte(`<map/>`))
	f.Add([]byte(`<`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data, func(string) ([]byte, error) { return nil, errNotFound })
	})
}

// FuzzParseTileset feeds the standalone tileset parser corrupt XML and
// JSON: an error, never a panic.
func FuzzParseTileset(f *testing.F) {
	if tsx, err := os.ReadFile("testdata/sets/props.tsx"); err == nil {
		f.Add(tsx)
	}
	f.Add([]byte(propsTileset))
	f.Add([]byte(`<tileset name="t" tilewidth="8" tileheight="8"><tile id="0"><animation><frame tileid="0" duration="1"/></animation><objectgroup><object id="1"><ellipse/></object></objectgroup></tile></tileset>`))
	f.Add([]byte(`{"name":"t","tiles":{"1":{"image":"b.png"}}}`))
	f.Add([]byte(`<tileset`))
	f.Add([]byte(`{`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseTileset(data)
	})
}

// FuzzParse feeds the map parser corrupt JSON with a resolver that finds
// nothing: an error, never a panic.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"width":2,"height":2,"tilewidth":16,"tileheight":16,"layers":[{"type":"tilelayer","name":"a","width":2,"height":2,"data":[1,2,3,4]}],"tilesets":[{"firstgid":1,"name":"t","tilewidth":16,"tileheight":16,"tilecount":4,"columns":2,"image":"t.png","imagewidth":32,"imageheight":32}]}`))
	f.Add([]byte(`{"layers":[{"type":"tilelayer","encoding":"base64","compression":"zlib","data":"eJw="}]}`))
	f.Add([]byte(`{"layers":[{"type":"objectgroup","objects":[{"polygon":[{"x":0,"y":0}]}]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data, func(string) ([]byte, error) { return nil, errNotFound })
	})
}
