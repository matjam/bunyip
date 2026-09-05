package gfx

import (
	"math/rand/v2"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestClusterBuild checks the grid a frame's lights are sorted into: a
// light reaches the cluster its own position falls in, and one behind
// the camera reaches none.
func TestClusterBuild(t *testing.T) {
	cam := Camera{Position: lin.V3(0, 0, 0), Target: lin.V3(0, 0, -1)}
	var grid clusterGrid
	cases := []struct {
		name string
		pos  lin.Vec3
		want bool
	}{
		{"ahead", lin.V3(0, 0, -10), true},
		{"off to one side", lin.V3(3, 0, -10), true},
		{"behind the camera", lin.V3(0, 0, 40), false},
		{"past the far plane", lin.V3(0, 0, -5000), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			grid.build([]pointLight{{pos: c.pos, rng: 2}}, nil, nil, cam, 16.0/9.0)
			if got := grid.used > 0; got != c.want {
				t.Fatalf("light at %v reaches %d clusters, want any: %v", c.pos, grid.used, c.want)
			}
			if !c.want {
				return
			}
			// The light's own position sits inside its block of clusters.
			view := cam.viewMatrix()
			clip := cam.ViewProj(16.0 / 9.0).MulVec4(c.pos.Vec4(1))
			x := clusterTile(clip.X/clip.W, clusterX)
			y := clusterTile(clip.Y/clip.W, clusterY)
			z := clusterSlice(-view.MulPoint(c.pos).Z, grid.scale, grid.bias)
			ci := int(x) + int(y)*clusterX + int(z)*clusterX*clusterY
			if n := grid.table[2*ci+1]; n != 1 {
				t.Errorf("the cluster holding the light lists %d lights, want 1", n)
			}
		})
	}
}

// TestClusterFull checks that a cluster keeps clusterLights of the
// lights piled into it and no more, and that the ones it keeps are
// listed inside its own part of the index list.
func TestClusterFull(t *testing.T) {
	cam := Camera{Position: lin.V3(0, 0, 0), Target: lin.V3(0, 0, -1)}
	var lights []pointLight
	for range clusterLights * 2 {
		lights = append(lights, pointLight{pos: lin.V3(0, 0, -10), rng: 0.2})
	}
	var grid clusterGrid
	grid.build(lights, nil, nil, cam, 1)
	if len(grid.lights) != len(lights) {
		t.Errorf("the frame kept %d light records, want %d", len(grid.lights), len(lights))
	}
	for ci := range clusterCount {
		if n := grid.table[2*ci+1]; n > clusterLights {
			t.Fatalf("cluster %d lists %d lights, past the cap of %d", ci, n, clusterLights)
		}
		if int(grid.table[2*ci]+grid.table[2*ci+1]) > grid.used {
			t.Fatalf("cluster %d runs past the %d indices in use", ci, grid.used)
		}
	}
}

// BenchmarkClusterBuild measures the per-frame cost of sorting a
// thousand lights into the grid, the work a scene with hundreds of them
// pays on the CPU.
func BenchmarkClusterBuild(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2))
	lights := make([]pointLight, MaxLights)
	for i := range lights {
		lights[i] = pointLight{
			pos: lin.V3((r.Float32()-0.5)*120, r.Float32()*8, -r.Float32()*120),
			rng: 2 + r.Float32()*6,
		}
	}
	cam := Camera{Position: lin.V3(0, 6, 30), Target: lin.V3(0, 0, -20)}
	var grid clusterGrid
	b.ReportAllocs()
	for b.Loop() {
		grid.build(lights, nil, nil, cam, 16.0/9.0)
	}
}
