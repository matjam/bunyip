package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"testing"
)

// triangleBin packs three positions (float32 x3) followed by three uint16
// indices, the way a minimal glTF buffer would.
func triangleBin() []byte {
	var buf bytes.Buffer
	for _, f := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		binary.Write(&buf, binary.LittleEndian, math.Float32bits(f))
	}
	for _, i := range []uint16{0, 1, 2} {
		binary.Write(&buf, binary.LittleEndian, i)
	}
	buf.Write([]byte{0, 0}) // pad to 4 bytes
	return buf.Bytes()
}

func triangleJSON(bufferURI string, withImage bool) string {
	images := ""
	if withImage {
		images = `,"images":[{"bufferView":2,"mimeType":"image/png"}],"textures":[{"source":0,"sampler":0}],"samplers":[{"magFilter":9728}]`
	}
	tex := ""
	if withImage {
		tex = `,"baseColorTexture":{"index":0}`
	}
	views := `{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":6}`
	if withImage {
		views += `,{"buffer":0,"byteOffset":44,"byteLength":%d}`
	}
	return `{"asset":{"version":"2.0"},"buffers":[{"byteLength":%d` + bufferURI + `}],
"bufferViews":[` + views + `],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5123,"count":3,"type":"SCALAR"}]` + images + `,
"materials":[{"name":"red","pbrMetallicRoughness":{"baseColorFactor":[1,0,0,1]` + tex + `}}],
"meshes":[{"name":"tri","primitives":[{"attributes":{"POSITION":0},"indices":1,"material":0}]}],
"nodes":[{"name":"root","translation":[0,0,-5],"children":[1]},{"name":"child","mesh":0,"scale":[2,2,2]}],
"scenes":[{"nodes":[0]}],"scene":0}`
}

func check(t *testing.T, doc *Document) {
	t.Helper()
	if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
		t.Fatalf("meshes: %+v", doc.Meshes)
	}
	p := doc.Meshes[0].Primitives[0]
	if len(p.Positions) != 3 || len(p.Indices) != 3 || len(p.Normals) != 3 || len(p.UVs) != 3 {
		t.Fatalf("primitive sizes: pos %d idx %d norm %d uv %d", len(p.Positions), len(p.Indices), len(p.Normals), len(p.UVs))
	}
	if p.Positions[1].X != 1 || p.Positions[2].Y != 1 {
		t.Errorf("positions %v", p.Positions)
	}
	if p.Normals[0].Z < 0.99 {
		t.Errorf("computed normal %v, want +Z", p.Normals[0])
	}
	if p.Material != 0 || doc.Materials[0].BaseColor != [4]float32{1, 0, 0, 1} {
		t.Errorf("material %d %+v", p.Material, doc.Materials)
	}
	if len(doc.Instances) != 1 {
		t.Fatalf("instances %+v", doc.Instances)
	}
	w := doc.Instances[0].World.MulPoint(p.Positions[1])
	if w.X != 2 || w.Z != -5 {
		t.Errorf("world point %v, want (2,0,-5): parent translation and child scale", w)
	}
}

func TestParseEmbedded(t *testing.T) {
	bin := triangleBin()
	uri := `,"uri":"data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(bin) + `"`
	doc, err := Parse([]byte(fmt.Sprintf(triangleJSON(uri, false), len(bin))), nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, doc)
}

func TestParseGLBWithImage(t *testing.T) {
	var pngBuf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	png.Encode(&pngBuf, img)
	bin := append(triangleBin(), pngBuf.Bytes()...)
	for len(bin)%4 != 0 {
		bin = append(bin, 0)
	}
	jsonText := fmt.Sprintf(triangleJSON("", true), len(bin), pngBuf.Len())
	for len(jsonText)%4 != 0 {
		jsonText += " "
	}
	var glb bytes.Buffer
	glb.WriteString("glTF")
	binary.Write(&glb, binary.LittleEndian, uint32(2))
	binary.Write(&glb, binary.LittleEndian, uint32(12+8+len(jsonText)+8+len(bin)))
	binary.Write(&glb, binary.LittleEndian, uint32(len(jsonText)))
	binary.Write(&glb, binary.LittleEndian, uint32(glbChunkJSON))
	glb.WriteString(jsonText)
	binary.Write(&glb, binary.LittleEndian, uint32(len(bin)))
	binary.Write(&glb, binary.LittleEndian, uint32(glbChunkBIN))
	glb.Write(bin)

	doc, err := Parse(glb.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	check(t, doc)
	if len(doc.Images) != 1 || doc.Images[0].Bounds().Dx() != 2 {
		t.Errorf("images %v", doc.Images)
	}
	if doc.Materials[0].Image != 0 || doc.Materials[0].Linear {
		t.Errorf("material texture binding %+v", doc.Materials[0])
	}
}
