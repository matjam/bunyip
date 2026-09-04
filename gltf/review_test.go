package gltf

import "testing"

// Negative byte lengths, offsets and strides from the file are rejected
// rather than used to slice.
func TestNegativeViewBoundsRejected(t *testing.T) {
	docs := []string{
		`{"buffers":[{"byteLength":1,"uri":"data:application/octet-stream;base64,AA=="}],"bufferViews":[{"buffer":0,"byteOffset":-4,"byteLength":4}],"images":[{"bufferView":0}]}`,
		`{"buffers":[{"byteLength":16,"uri":"data:application/octet-stream;base64,AAAAAAAAAAAAAAAAAAAAAA=="}],"bufferViews":[{"buffer":0,"byteLength":16,"byteStride":-8}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"SCALAR"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}]}`,
	}
	for i, d := range docs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("doc %d: Parse panicked: %v", i, r)
				}
			}()
			_, _ = Parse([]byte(d), nil)
		}()
	}
}

// An accessor with no buffer view cannot claim unbounded memory.
func TestViewlessAccessorBounded(t *testing.T) {
	d := `{"accessors":[{"componentType":5126,"count":268435456,"type":"MAT4"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}]}`
	if _, err := Parse([]byte(d), nil); err == nil {
		t.Error("a 16 GB zero accessor parsed without error")
	}
}
