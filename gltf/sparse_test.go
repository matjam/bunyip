package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

// TestSparseAccessors parses a quad whose morph target is written the way
// Blender writes one: a sparse accessor with no buffer view, so the
// untouched vertices stay at zero, and a second target that overrides two
// elements of dense base data. The index accessor is sparse too, to cover
// the integer path.
func TestSparseAccessors(t *testing.T) {
	var bin []byte
	f32 := func(vals ...float32) {
		for _, v := range vals {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
		}
	}
	u16 := func(vals ...uint16) {
		for _, v := range vals {
			bin = binary.LittleEndian.AppendUint16(bin, v)
		}
	}
	f32(0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0) // 0: four positions
	u16(0, 1, 2, 0, 2, 3)                   // 48: six indices
	u16(1, 3)                               // 60: sparse element indices for both targets
	f32(0, 2, 0, 0, 5, 0)                   // 64: sparse values for target 0
	f32(7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7) // 88: dense base for target 1
	f32(0, 0, 9, 0, 0, 9)                   // 136: sparse values for target 1
	u16(4)                                  // 160: one sparse index for the index accessor
	u16(1)                                  // 162: its value
	bin = append(bin, 0, 0)

	src := fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":48},{"buffer":0,"byteOffset":48,"byteLength":12},
 {"buffer":0,"byteOffset":60,"byteLength":4},{"buffer":0,"byteOffset":64,"byteLength":24},
 {"buffer":0,"byteOffset":88,"byteLength":48},{"buffer":0,"byteOffset":136,"byteLength":24},
 {"buffer":0,"byteOffset":160,"byteLength":2},{"buffer":0,"byteOffset":162,"byteLength":2}],
"accessors":[
 {"bufferView":0,"componentType":5126,"count":4,"type":"VEC3"},
 {"bufferView":1,"componentType":5123,"count":6,"type":"SCALAR",
  "sparse":{"count":1,"indices":{"bufferView":6,"byteOffset":0,"componentType":5123},"values":{"bufferView":7,"byteOffset":0}}},
 {"componentType":5126,"count":4,"type":"VEC3",
  "sparse":{"count":2,"indices":{"bufferView":2,"byteOffset":0,"componentType":5123},"values":{"bufferView":3,"byteOffset":0}}},
 {"bufferView":4,"componentType":5126,"count":4,"type":"VEC3",
  "sparse":{"count":2,"indices":{"bufferView":2,"byteOffset":0,"componentType":5123},"values":{"bufferView":5,"byteOffset":0}}}],
"meshes":[{"name":"quad","primitives":[{"attributes":{"POSITION":0},"indices":1,"targets":[{"POSITION":2},{"POSITION":3}]}]}],
"nodes":[{"name":"a","mesh":0}],"scenes":[{"nodes":[0]}]}`,
		len(bin), base64.StdEncoding.EncodeToString(bin))

	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Meshes[0].Primitives[0]
	if len(p.Targets) != 2 {
		t.Fatalf("targets %d", len(p.Targets))
	}
	// Target 0 has no buffer view: the two sparse elements carry deltas
	// and the rest stay zero.
	want0 := [4][3]float32{{0, 0, 0}, {0, 2, 0}, {0, 0, 0}, {0, 5, 0}}
	for i, w := range want0 {
		got := p.Targets[0].Positions[i]
		if got.X != w[0] || got.Y != w[1] || got.Z != w[2] {
			t.Errorf("target 0 delta %d = %v, want %v", i, got, w)
		}
	}
	// Target 1 has dense sevens with elements 1 and 3 replaced.
	want1 := [4][3]float32{{7, 7, 7}, {0, 0, 9}, {7, 7, 7}, {0, 0, 9}}
	for i, w := range want1 {
		got := p.Targets[1].Positions[i]
		if got.X != w[0] || got.Y != w[1] || got.Z != w[2] {
			t.Errorf("target 1 delta %d = %v, want %v", i, got, w)
		}
	}
	// The index accessor's element 4 was 2 densely and is 1 sparsely.
	want := []uint32{0, 1, 2, 0, 1, 3}
	for i, w := range want {
		if p.Indices[i] != w {
			t.Errorf("index %d = %d, want %d", i, p.Indices[i], w)
		}
	}
}

// TestSparseAccessorErrors rejects a sparse block that points outside the
// accessor or its buffer views instead of reading past them.
func TestSparseAccessorErrors(t *testing.T) {
	var bin []byte
	for _, v := range []float32{0, 0, 0, 1, 0, 0, 1, 1, 0} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
	}
	for _, v := range []uint16{0, 1, 2, 9} { // three indices, then an element past the accessor
		bin = binary.LittleEndian.AppendUint16(bin, v)
	}
	for range 3 {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(1))
	}
	data := base64.StdEncoding.EncodeToString(bin)
	cases := []struct {
		name   string
		sparse string
	}{
		{"index out of range", `{"count":1,"indices":{"bufferView":2,"byteOffset":0,"componentType":5123},"values":{"bufferView":3,"byteOffset":0}}`},
		{"count above the accessor", `{"count":9,"indices":{"bufferView":2,"byteOffset":0,"componentType":5123},"values":{"bufferView":3,"byteOffset":0}}`},
		{"values overrun", `{"count":1,"indices":{"bufferView":2,"byteOffset":0,"componentType":5123},"values":{"bufferView":3,"byteOffset":8}}`},
		{"unsupported index type", `{"count":1,"indices":{"bufferView":2,"byteOffset":0,"componentType":5126},"values":{"bufferView":3,"byteOffset":0}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":6},
 {"buffer":0,"byteOffset":42,"byteLength":2},{"buffer":0,"byteOffset":44,"byteLength":12}],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},
 {"bufferView":1,"componentType":5123,"count":3,"type":"SCALAR"},
 {"componentType":5126,"count":3,"type":"VEC3","sparse":%s}],
"meshes":[{"name":"tri","primitives":[{"attributes":{"POSITION":0},"indices":1,"targets":[{"POSITION":2}]}]}],
"nodes":[{"name":"a","mesh":0}],"scenes":[{"nodes":[0]}]}`, len(bin), data, c.sparse)
			if _, err := Parse([]byte(src), nil); err == nil {
				t.Fatal("want an error, got none")
			}
		})
	}
}
