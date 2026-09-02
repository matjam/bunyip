package gltf_test

import (
	"fmt"

	"github.com/matjam/bunyip/gltf"
)

// Load parses .gltf and .glb files without touching the GPU; gfx.LoadModel
// uploads the result.
func ExampleLoad() {
	doc, err := gltf.Load("robot.glb")
	if err != nil {
		return
	}
	fmt.Println(len(doc.Meshes), "meshes,", len(doc.Animations), "animation clips")
}
