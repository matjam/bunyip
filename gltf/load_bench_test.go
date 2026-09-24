package gltf

// Load benchmarks: a synthetic binary glTF of about 50 MB (positions,
// normals, texture coordinates and 32-bit indices), one with a JSON
// header for 5000 small meshes, and one carrying 16 embedded 1024x1024
// PNG images.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

type loadGLB struct {
	bin   bytes.Buffer
	views []map[string]any
	accs  []map[string]any
}

func (a *loadGLB) view(data []byte) int {
	for a.bin.Len()%4 != 0 {
		a.bin.WriteByte(0)
	}
	a.views = append(a.views, map[string]any{"buffer": 0, "byteOffset": a.bin.Len(), "byteLength": len(data)})
	a.bin.Write(data)
	return len(a.views) - 1
}

func (a *loadGLB) accessor(view, ctype, count int, typ string) int {
	a.accs = append(a.accs, map[string]any{"bufferView": view, "componentType": ctype, "count": count, "type": typ})
	return len(a.accs) - 1
}

func loadFloats(vals []float32) []byte {
	out := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

// loadBuildGLB makes meshes grids of side by side vertices each, and
// images embedded PNGs of imageSide pixels.
func loadBuildGLB(meshes, side, images, imageSide int) []byte {
	a := &loadGLB{}
	var jm []map[string]any
	var nodes []int
	var jnodes []map[string]any
	n := side * side
	pos := make([]float32, n*3)
	nrm := make([]float32, n*3)
	uv := make([]float32, n*2)
	for y := range side {
		for x := range side {
			i := y*side + x
			pos[i*3], pos[i*3+1], pos[i*3+2] = float32(x), float32((x*y)%7), float32(y)
			nrm[i*3+1] = 1
			uv[i*2], uv[i*2+1] = float32(x)/float32(side), float32(y)/float32(side)
		}
	}
	idx := make([]byte, 0, (side-1)*(side-1)*6*4)
	for y := range side - 1 {
		for x := range side - 1 {
			i := uint32(y*side + x)
			for _, v := range []uint32{i, i + uint32(side), i + 1, i + 1, i + uint32(side), i + uint32(side) + 1} {
				idx = binary.LittleEndian.AppendUint32(idx, v)
			}
		}
	}
	pb, nb, ub := loadFloats(pos), loadFloats(nrm), loadFloats(uv)
	for m := range meshes {
		p := a.accessor(a.view(pb), 5126, n, "VEC3")
		a.accs[p]["min"] = []float32{0, 0, 0}
		a.accs[p]["max"] = []float32{float32(side), 7, float32(side)}
		nr := a.accessor(a.view(nb), 5126, n, "VEC3")
		t := a.accessor(a.view(ub), 5126, n, "VEC2")
		ix := a.accessor(a.view(idx), 5125, len(idx)/4, "SCALAR")
		jm = append(jm, map[string]any{"primitives": []any{map[string]any{
			"attributes": map[string]int{"POSITION": p, "NORMAL": nr, "TEXCOORD_0": t}, "indices": ix}}})
		jnodes = append(jnodes, map[string]any{"mesh": m, "translation": []float32{float32(m), 0, 0}})
		nodes = append(nodes, m)
	}
	var jimages []map[string]any
	for i := range images {
		img := image.NewRGBA(image.Rect(0, 0, imageSide, imageSide))
		for y := range imageSide {
			for x := range imageSide {
				img.SetRGBA(x, y, color.RGBA{uint8(x + i), uint8(y), uint8(x ^ y), 255})
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		jimages = append(jimages, map[string]any{"bufferView": a.view(buf.Bytes()), "mimeType": "image/png"})
	}
	for a.bin.Len()%4 != 0 {
		a.bin.WriteByte(0)
	}
	doc := map[string]any{
		"asset":       map[string]string{"version": "2.0"},
		"buffers":     []any{map[string]int{"byteLength": a.bin.Len()}},
		"bufferViews": a.views, "accessors": a.accs, "meshes": jm, "nodes": jnodes,
		"scenes": []any{map[string]any{"nodes": nodes}}, "scene": 0,
	}
	if len(jimages) > 0 {
		doc["images"] = jimages
	}
	js, _ := json.Marshal(doc)
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	var out bytes.Buffer
	out.WriteString("glTF")
	binary.Write(&out, binary.LittleEndian, uint32(2))
	binary.Write(&out, binary.LittleEndian, uint32(12+8+len(js)+8+a.bin.Len()))
	binary.Write(&out, binary.LittleEndian, uint32(len(js)))
	binary.Write(&out, binary.LittleEndian, uint32(glbChunkJSON))
	out.Write(js)
	binary.Write(&out, binary.LittleEndian, uint32(a.bin.Len()))
	binary.Write(&out, binary.LittleEndian, uint32(glbChunkBIN))
	out.Write(a.bin.Bytes())
	return out.Bytes()
}

func BenchmarkParseGLB(b *testing.B) {
	for _, c := range []struct {
		name                      string
		meshes, side, imgs, imgSz int
	}{
		{"50MB_geometry", 64, 128, 0, 0},
		{"5000_small_meshes", 5000, 8, 0, 0},
		{"16_png_1024", 1, 8, 16, 1024},
	} {
		data := loadBuildGLB(c.meshes, c.side, c.imgs, c.imgSz)
		b.Run(fmt.Sprintf("%s_%dMB", c.name, len(data)>>20), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if _, err := Parse(data, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
