package tiled

import "testing"

// A chunk placed absurdly far from the origin is rejected rather than
// overflowing the layer's row arithmetic.
func TestChunkExtentsBounded(t *testing.T) {
	m := `{"infinite":true,"layers":[{"type":"tilelayer","name":"a","chunks":[{"x":0,"y":0,"width":1,"height":1,"data":[0]},{"x":4611686018427387904,"y":0,"width":1,"height":4,"data":[0,0,0,0]}]}]}`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Parse panicked: %v", r)
		}
	}()
	if _, err := Parse([]byte(m), nil); err == nil {
		t.Error("an out-of-range chunk parsed without error")
	}
}
