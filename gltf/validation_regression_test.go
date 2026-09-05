package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestParseRejectsNegativeBufferLength(t *testing.T) {
	for _, uri := range []string{"data:application/octet-stream;base64,", "buffer.bin"} {
		t.Run(uri, func(t *testing.T) {
			src := fmt.Sprintf(`{"buffers":[{"byteLength":-1,"uri":%q}]}`, uri)
			if _, err := Parse([]byte(src), func(string) ([]byte, error) { return []byte{}, nil }); err == nil {
				t.Fatal("negative buffer length accepted")
			}
		})
	}
	t.Run("glb", func(t *testing.T) {
		jsonData := []byte(`{"buffers":[{"byteLength":-1}]}`)
		for len(jsonData)%4 != 0 {
			jsonData = append(jsonData, ' ')
		}
		glb := []byte("glTF")
		for _, word := range []uint32{2, uint32(12 + 8 + len(jsonData) + 8 + 4), uint32(len(jsonData)), glbChunkJSON} {
			glb = binary.LittleEndian.AppendUint32(glb, word)
		}
		glb = append(glb, jsonData...)
		for _, word := range []uint32{4, glbChunkBIN, 0} {
			glb = binary.LittleEndian.AppendUint32(glb, word)
		}
		if _, err := Parse(glb, nil); err == nil {
			t.Fatal("negative GLB buffer length accepted")
		}
	})
}

// animationValidationDocument uses real packed float buffers so the assertions
// also check that validating the layout preserves values and cubic key order.
func animationValidationDocument(t *testing.T, path, inputType, outputType, interpolation string, times, values []float32) []byte {
	t.Helper()
	var bin []byte
	for _, values := range [][]float32{times, values} {
		for _, v := range values {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
		}
	}
	return []byte(fmt.Sprintf(`{"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteLength":%d},{"buffer":0,"byteOffset":%d,"byteLength":%d}],
"accessors":[{"bufferView":0,"componentType":5126,"type":%q,"count":%d},{"bufferView":1,"componentType":5126,"type":%q,"count":%d}],
"nodes":[{}],"animations":[{"samplers":[{"input":0,"output":1,"interpolation":%q}],"channels":[{"sampler":0,"target":{"node":0,"path":%q}}]}]}`,
		len(bin), base64.StdEncoding.EncodeToString(bin), len(times)*4, len(times)*4, len(values)*4,
		inputType, len(times)/typeCounts[inputType], outputType, len(values)/typeCounts[outputType], interpolation, path))
}

func TestParseRejectsInvalidAnimationShape(t *testing.T) {
	for _, path := range []string{"translation", "scale", "rotation", "weights"} {
		for _, outputType := range []string{"SCALAR", "VEC2", "VEC3", "VEC4", "MAT4"} {
			if (path == "translation" || path == "scale") && outputType == "VEC3" || path == "rotation" && outputType == "VEC4" || path == "weights" && outputType == "SCALAR" {
				continue
			}
			t.Run(path+"/"+outputType, func(t *testing.T) {
				src := animationValidationDocument(t, path, "SCALAR", outputType, "", []float32{0}, make([]float32, typeCounts[outputType]))
				if _, err := Parse(src, nil); err == nil {
					t.Fatal("invalid animation output shape accepted")
				}
			})
		}
	}
}

func TestParseRejectsInvalidAnimationCounts(t *testing.T) {
	for _, tc := range []struct {
		name, path, inputType, outputType, interpolation string
		times, values                                    []float32
	}{
		{"vector_input", "translation", "VEC2", "VEC3", "", []float32{0, 1}, make([]float32, 3)},
		{"empty_input", "translation", "SCALAR", "VEC3", "", nil, nil},
		{"missing_key", "translation", "SCALAR", "VEC3", "", []float32{0, 1}, make([]float32, 3)},
		{"extra_key", "rotation", "SCALAR", "VEC4", "", []float32{0}, make([]float32, 8)},
		{"missing_cubic_tangent", "scale", "SCALAR", "VEC3", "CUBICSPLINE", []float32{0, 1}, make([]float32, 15)},
		{"empty_weights", "weights", "SCALAR", "SCALAR", "", []float32{0}, nil},
		{"uneven_weights", "weights", "SCALAR", "SCALAR", "", []float32{0, 1}, make([]float32, 3)},
		{"uneven_cubic_weights", "weights", "SCALAR", "SCALAR", "CUBICSPLINE", []float32{0, 1}, make([]float32, 7)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := animationValidationDocument(t, tc.path, tc.inputType, tc.outputType, tc.interpolation, tc.times, tc.values)
			if _, err := Parse(src, nil); err == nil {
				t.Fatal("invalid animation key count accepted")
			}
		})
	}
}

func TestParseValidAnimationLayouts(t *testing.T) {
	for _, path := range []string{"translation", "scale", "rotation", "weights"} {
		for _, interpolation := range []string{"", "LINEAR", "STEP", "CUBICSPLINE"} {
			t.Run(path+"/"+interpolation, func(t *testing.T) {
				outputType, width := "VEC3", 3
				if path == "rotation" {
					outputType, width = "VEC4", 4
				} else if path == "weights" {
					outputType, width = "SCALAR", 2
				}
				var values, want []float32
				for key := range 2 {
					if interpolation == "CUBICSPLINE" {
						values = append(values, make([]float32, width)...)
					}
					for component := range width {
						v := float32(key*width + component + 1)
						values, want = append(values, v), append(want, v)
					}
					if interpolation == "CUBICSPLINE" {
						values = append(values, make([]float32, width)...)
					}
				}
				src := animationValidationDocument(t, path, "SCALAR", outputType, interpolation, []float32{0, 1}, values)
				doc, err := Parse(src, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(doc.Animations) != 1 || len(doc.Animations[0].Channels) != 1 {
					t.Fatalf("animation channels: %+v", doc.Animations)
				}
				ch := doc.Animations[0].Channels[0]
				if !reflect.DeepEqual(ch.Times, []float32{0, 1}) || doc.Animations[0].Duration != 1 || ch.Step != (interpolation == "STEP") {
					t.Fatalf("animation timing: %+v", ch)
				}
				if path == "weights" {
					if !reflect.DeepEqual(ch.Weights, want) || ch.WeightCount() != width {
						t.Fatalf("weights %v, want %v", ch.Weights, want)
					}
				} else {
					var vectors []lin.Vec4
					for key := range 2 {
						v := lin.V4(want[key*width], want[key*width+1], want[key*width+2], 0)
						if width == 4 {
							v.W = want[key*width+3]
						}
						vectors = append(vectors, v)
					}
					if !reflect.DeepEqual(ch.Values, vectors) {
						t.Fatalf("values %v, want %v", ch.Values, vectors)
					}
				}
			})
		}
	}
}

func TestParseRejectsOverflowingAccessorBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*jsonDoc)
	}{
		{"view_offset", func(d *jsonDoc) { d.BufferViews[0].ByteOffset = math.MaxInt }},
		{"accessor_offset", func(d *jsonDoc) { d.Accessors[0].ByteOffset = math.MaxInt }},
		{"accessor_stride", func(d *jsonDoc) { d.BufferViews[0].ByteStride = math.MaxInt }},
		{"short_stride", func(d *jsonDoc) { d.BufferViews[1].ByteStride = 4 }},
		{"sparse_index_offset", func(d *jsonDoc) {
			d.Accessors[0].Sparse = &jsonSparse{Count: 1}
			d.Accessors[0].Sparse.Indices.ComponentType = 5121
			d.Accessors[0].Sparse.Indices.ByteOffset = math.MaxInt
		}},
		{"sparse_value_offset", func(d *jsonDoc) {
			d.Accessors[0].Sparse = &jsonSparse{Count: 1}
			d.Accessors[0].Sparse.Indices.ComponentType = 5121
			d.Accessors[0].Sparse.Values.ByteOffset = math.MaxInt
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := animationValidationDocument(t, "translation", "SCALAR", "VEC3", "", []float32{0, 1, 2}, make([]float32, 9))
			var d jsonDoc
			if err := json.Unmarshal(src, &d); err != nil {
				t.Fatal(err)
			}
			tc.edit(&d)
			src, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(src, nil); err == nil {
				t.Fatal("invalid accessor bounds accepted")
			}
		})
	}
}

func TestParseRejectsInvalidIndexAccessorBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*jsonDoc)
	}{
		{"offset", func(d *jsonDoc) { d.Accessors[0].ByteOffset = math.MaxInt }},
		{"stride", func(d *jsonDoc) { d.BufferViews[0].ByteStride = math.MaxInt }},
		{"short_stride", func(d *jsonDoc) { d.BufferViews[0].ByteStride = 1 }},
		{"oversized_count", func(d *jsonDoc) { d.Accessors[0].Count = 1 << 28 }},
		{"oversized_sparse_base", func(d *jsonDoc) {
			d.Accessors[0].BufferView = nil
			d.Accessors[0].Count = 1 << 28
			d.Accessors[0].Sparse = &jsonSparse{Count: 1}
			d.Accessors[0].Sparse.Indices.ComponentType = 5121
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := animationValidationDocument(t, "translation", "SCALAR", "VEC3", "", []float32{0, 0, 0}, make([]float32, 9))
			var d jsonDoc
			if err := json.Unmarshal(src, &d); err != nil {
				t.Fatal(err)
			}
			d.Animations = nil
			d.Accessors[0].ComponentType = 5125
			index := 0
			d.Meshes = []jsonMesh{{Primitives: []jsonPrimitive{{Attributes: map[string]int{"POSITION": 1}, Indices: &index}}}}
			tc.edit(&d)
			src, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(src, nil); err == nil {
				t.Fatal("invalid index accessor bounds accepted")
			}
		})
	}
}
