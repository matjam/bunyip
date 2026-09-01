package gfx

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// TestLoadModel parses a one-quad glTF from memory, uploads it and draws it
// facing the camera; the centre pixel must carry the material's colour.
func TestLoadModel(t *testing.T) {
	g := newHeadless(t, 64, 64)
	var bin []byte
	for _, f := range []float32{-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(f))
	}
	for _, i := range []uint16{0, 1, 2, 0, 2, 3} {
		bin = binary.LittleEndian.AppendUint16(bin, i)
	}
	doc, err := gltf.Parse([]byte(fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteLength":48},{"buffer":0,"byteOffset":48,"byteLength":12}],
"accessors":[{"bufferView":0,"componentType":5126,"count":4,"type":"VEC3"},{"bufferView":1,"componentType":5123,"count":6,"type":"SCALAR"}],
"materials":[{"pbrMetallicRoughness":{"baseColorFactor":[0,0,1,1]}}],
"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1,"material":0}]}],
"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}]}`, len(bin), base64.StdEncoding.EncodeToString(bin))), nil)
	if err != nil {
		t.Fatal(err)
	}
	model, err := g.LoadModel(doc)
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}
	defer model.Destroy()
	if len(model.Parts) != 1 || model.Max.X != 1 {
		t.Fatalf("parts %d bounds %v..%v", len(model.Parts), model.Min, model.Max)
	}
	for range 2 {
		ok, err := g.Begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 2)})
		// Physically based diffuse is albedo/pi, so light the quad with pi worth of radiance.
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3.2, 3.2, 3.2, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
		g.DrawModel(model, lin.Identity())
		img, err := g.End(true)
		if err != nil {
			t.Fatal(err)
		}
		if c := img.RGBAAt(32, 32); c.B < 150 || int(c.B) < int(c.R)+50 {
			t.Errorf("centre %v should be blue", c)
		}
	}
}
