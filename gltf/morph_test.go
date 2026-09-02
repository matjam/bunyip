package gltf

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

// TestMorphTargets parses a triangle with two targets (one with normals),
// mesh and node default weights, and a weights clip, including the
// cubic-spline layout.
func TestMorphTargets(t *testing.T) {
	var bin []byte
	f32 := func(vals ...float32) {
		for _, v := range vals {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
		}
	}
	f32(0, 0, 0, 1, 0, 0, 0, 1, 0)            // 0: positions
	f32(0, 0, 1, 0, 0, 1, 0, 0, 1)            // 36: target 0 position deltas
	f32(1, 0, 0, 1, 0, 0, 1, 0, 0)            // 72: target 1 position deltas
	f32(0, 1, 0, 0, 1, 0, 0, 1, 0)            // 108: target 1 normal deltas
	f32(0, 1)                                 // 144: times
	f32(0, 0, 1, 0.5)                         // 152: weights, two per key
	f32(9, 9, 0, 0, 9, 9, 9, 9, 1, 0.5, 9, 9) // 168: cubic weights: in, value, out per key
	for _, i := range []uint16{0, 1, 2} {     // 216: indices
		bin = binary.LittleEndian.AppendUint16(bin, i)
	}
	bin = append(bin, 0, 0)
	src := fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":36},{"buffer":0,"byteOffset":72,"byteLength":36},
 {"buffer":0,"byteOffset":108,"byteLength":36},{"buffer":0,"byteOffset":144,"byteLength":8},{"buffer":0,"byteOffset":152,"byteLength":16},
 {"buffer":0,"byteOffset":168,"byteLength":48},{"buffer":0,"byteOffset":216,"byteLength":6}],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5126,"count":3,"type":"VEC3"},
 {"bufferView":2,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":3,"componentType":5126,"count":3,"type":"VEC3"},
 {"bufferView":4,"componentType":5126,"count":2,"type":"SCALAR"},{"bufferView":5,"componentType":5126,"count":4,"type":"SCALAR"},
 {"bufferView":6,"componentType":5126,"count":12,"type":"SCALAR"},{"bufferView":7,"componentType":5123,"count":3,"type":"SCALAR"}],
"meshes":[{"name":"tri","weights":[0.25,0],"extras":{"targetNames":["up","side"]},
 "primitives":[{"attributes":{"POSITION":0},"indices":7,"targets":[{"POSITION":1},{"POSITION":2,"NORMAL":3}]}]}],
"nodes":[{"name":"a","mesh":0},{"name":"b","mesh":0,"weights":[0,1]}],"scenes":[{"nodes":[0,1]}],
"animations":[{"name":"linear","channels":[{"sampler":0,"target":{"node":0,"path":"weights"}}],"samplers":[{"input":4,"output":5}]},
 {"name":"cubic","channels":[{"sampler":0,"target":{"node":0,"path":"weights"}}],"samplers":[{"input":4,"output":6,"interpolation":"CUBICSPLINE"}]}]}`,
		len(bin), base64.StdEncoding.EncodeToString(bin))
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	mesh := doc.Meshes[0]
	if mesh.TargetCount() != 2 || len(mesh.Weights) != 2 || mesh.Weights[0] != 0.25 || len(mesh.TargetNames) != 2 || mesh.TargetNames[1] != "side" {
		t.Fatalf("mesh %+v", mesh)
	}
	p := mesh.Primitives[0]
	if len(p.Targets) != 2 || p.Targets[0].Positions[2].Z != 1 || p.Targets[0].Normals != nil || p.Targets[1].Normals[1].Y != 1 {
		t.Fatalf("targets %+v", p.Targets)
	}
	if doc.Nodes[0].Weights != nil || len(doc.Nodes[1].Weights) != 2 || doc.Nodes[1].Weights[1] != 1 {
		t.Fatalf("node weights %v %v", doc.Nodes[0].Weights, doc.Nodes[1].Weights)
	}
	for _, name := range []string{"linear", "cubic"} {
		var anim *Animation
		for i := range doc.Animations {
			if doc.Animations[i].Name == name {
				anim = &doc.Animations[i]
			}
		}
		if anim == nil || len(anim.Channels) != 1 {
			t.Fatalf("%s: %+v", name, anim)
		}
		ch := anim.Channels[0]
		if ch.Path != PathWeights || ch.WeightCount() != 2 || len(ch.Times) != 2 || anim.Duration != 1 {
			t.Fatalf("%s channel %+v", name, ch)
		}
		if ch.Weights[2] != 1 || ch.Weights[3] != 0.5 {
			t.Fatalf("%s weights %v", name, ch.Weights)
		}
	}
}
