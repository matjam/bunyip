package tiled

import (
	"errors"
	"testing"
)

var errNotFound = errors.New("not found")

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
