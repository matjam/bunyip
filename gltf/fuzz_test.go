package gltf

import (
	"errors"
	"testing"
)

// FuzzParse feeds the glTF parser corrupt documents with a resolver that
// finds nothing: an error, never a panic.
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"asset":{"version":"2.0"},"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5123,"count":3,"type":"SCALAR"}],"bufferViews":[{"buffer":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":6}],"buffers":[{"byteLength":42,"uri":"data:application/octet-stream;base64,AAAAAAAAAAAAAAAAAACAPwAAAAAAAAAAAAAAAAAAgD8AAAAAAAABAAIAAAA="}]}`))
	f.Add([]byte(`{"asset":{"version":"2.0"},"materials":[{"extensions":{"KHR_materials_transmission":{"transmissionTexture":{"index":0}}}}],"textures":[{"source":0}],"images":[{"uri":"x.png"}]}`))
	f.Add([]byte("glTF\x02\x00\x00\x00\x14\x00\x00\x00"))
	f.Add([]byte(`{}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data, func(string) ([]byte, error) { return nil, errors.New("not found") })
	})
}
