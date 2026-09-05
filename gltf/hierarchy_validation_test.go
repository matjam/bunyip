package gltf

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/matjam/bunyip/lin"
)

// Run the exponential reproducer in a subprocess so a regression cannot
// consume the test runner indefinitely.
func TestParseRejectsCyclicHierarchyBounded(t *testing.T) {
	const probe = "BUNYIP_GLTF_HIERARCHY_PROBE"
	if os.Getenv(probe) == "1" {
		_, err := Parse([]byte(`{"scenes":[{"nodes":[0]}],"nodes":[{"children":[0,0]}]}`), nil)
		if err == nil {
			t.Fatal("cyclic hierarchy accepted")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestParseRejectsCyclicHierarchyBounded$")
	cmd.Env = append(os.Environ(), probe+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("parsing the duplicate self-reference exceeded two seconds: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("hierarchy probe failed: %v\n%s", err, out)
	}
}

func TestParseRejectsInvalidHierarchy(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"self cycle", `{"nodes":[{"children":[0]}],"scenes":[{"nodes":[0]}]}`},
		{"two node cycle", `{"nodes":[{"children":[1]},{"children":[0]}]}`},
		{"unselected cycle", `{"nodes":[{},{"children":[2]},{"children":[1]}],"scenes":[{"nodes":[0]}]}`},
		{"duplicate child", `{"nodes":[{"children":[1,1]},{}]}`},
		{"multiple parents", `{"nodes":[{"children":[2]},{"children":[2]},{}]}`},
		{"negative child", `{"nodes":[{"children":[-1]}]}`},
		{"child past end", `{"nodes":[{"children":[1]}]}`},
		{"negative root", `{"nodes":[{}],"scenes":[{"nodes":[-1]}]}`},
		{"root past end", `{"nodes":[{}],"scenes":[{"nodes":[1]}]}`},
		{"invalid unselected scene", `{"nodes":[{}],"scenes":[{"nodes":[0]},{"nodes":[1]}]}`},
		{"duplicate root", `{"nodes":[{}],"scenes":[{"nodes":[0,0]}]}`},
		{"child as scene root", `{"nodes":[{"children":[1]},{}],"scenes":[{"nodes":[1]}]}`},
		{"negative default scene", `{"scene":-1,"scenes":[{}]}`},
		{"default scene past end", `{"scene":1,"scenes":[{}]}`},
		{"default scene without scenes", `{"scene":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.src), nil)
			if err == nil || doc != nil {
				t.Fatalf("invalid hierarchy returned document %v, error %v", doc, err)
			}
		})
	}
}

func TestParseValidatesHierarchyBeforeResources(t *testing.T) {
	called := false
	src := `{"nodes":[{"children":[-1]}],"buffers":[{"uri":"mesh.bin","byteLength":0}]}`
	_, err := Parse([]byte(src), func(string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || called {
		t.Fatalf("invalid hierarchy: error %v, resolver called %v", err, called)
	}
}

func TestParseDeepHierarchy(t *testing.T) {
	const count = 4096
	j := jsonDoc{Nodes: make([]jsonNode, count), Meshes: []jsonMesh{{}}}
	mesh := 0
	for i := range j.Nodes {
		j.Nodes[i].Translation = []float32{1, 0, 0}
		if i+1 < count {
			j.Nodes[i].Children = []int{i + 1}
		} else {
			j.Nodes[i].Mesh = &mesh
		}
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Instances) != 1 || doc.Instances[0].Node != count-1 {
		t.Fatalf("deep leaf missing: %v", doc.Instances)
	}
	if got := doc.Instances[0].World.MulPoint(lin.Vec3{}); got != lin.V3(count, 0, 0) {
		t.Fatalf("leaf world position %v, want (%d,0,0)", got, count)
	}
	if doc.Nodes[count-1].Parent != count-2 {
		t.Fatalf("leaf parent %d, want %d", doc.Nodes[count-1].Parent, count-2)
	}
}

func TestParseHierarchySceneSelection(t *testing.T) {
	const nodes = `"meshes":[{}],"nodes":[{"mesh":0,"translation":[1,0,0],"children":[2,1]},{"mesh":0,"translation":[10,0,0]},{"mesh":0,"translation":[100,0,0]},{"mesh":0,"translation":[1000,0,0]}]`
	tests := []struct {
		name   string
		scenes string
		order  []int
		x      []float32
	}{
		{"inferred roots", "", []int{0, 2, 1, 3}, []float32{1, 101, 11, 1000}},
		{"first scene", `,"scenes":[{"nodes":[3,0]},{"nodes":[0]}]`, []int{3, 0, 2, 1}, []float32{1000, 1, 101, 11}},
		{"reused root in selected scene", `,"scenes":[{"nodes":[3,0]},{"nodes":[0]}],"scene":1`, []int{0, 2, 1}, []float32{1, 101, 11}},
		{"empty scene", `,"scenes":[{}]`, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte("{"+nodes+tt.scenes+"}"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Instances) != len(tt.order) {
				t.Fatalf("instances %d, want %d", len(doc.Instances), len(tt.order))
			}
			for i, inst := range doc.Instances {
				if inst.Node != tt.order[i] || inst.World[12] != tt.x[i] {
					t.Errorf("instance %d: node %d at x=%g, want node %d at x=%g", i, inst.Node, inst.World[12], tt.order[i], tt.x[i])
				}
			}
		})
	}
}
