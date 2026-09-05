package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

// sortTestDraws builds a spread of draws with the fields the sort reads:
// n distinct shaders, meshes, sets and uniform offsets, a quarter of
// them blended and a scattering culled and skinned.
func sortTestDraws(n, shaders, meshes int) []meshDraw {
	sh := make([]*Shader, shaders)
	for i := range sh {
		sh[i] = &Shader{}
	}
	ms := make([]*Mesh, meshes)
	for i := range ms {
		ms[i] = &Mesh{}
	}
	draws := make([]meshDraw, n)
	for i := range draws {
		d := &draws[i]
		d.mesh = ms[(i*7)%len(ms)]
		d.shader = sh[i%len(sh)]
		d.uniform = int32(i%3) - 1 // -1, 0, 1: none, and two blocks
		d.set = vk.VkDescriptorSet(uintptr(1 + i%5))
		d.depth = float32(math.Mod(float64(i)*37.5, 100))
		d.culled = i%9 == 0
		d.skinned = i%11 == 0
		d.mat = Material{Roughness: 0.5}
		if i%4 == 0 {
			d.mat.Blend = true
		}
		d.blended = d.mat.blended()
	}
	return draws
}

// runsOf counts the instanced runs a sorted list records, by the rule
// drawRuns merges with.
func runsOf(l drawList) int {
	runs := 0
	for i := 0; i < l.len(); {
		d := l.at(i)
		run := 1
		if !d.skinned {
			key := meshKey(&d.mat, false, d.shell > 0, outKey{})
			for i+run < l.len() {
				e := l.at(i + run)
				if e.skinned || e.mesh != d.mesh || e.set != d.set || e.shader != d.shader || e.uniform != d.uniform || meshKey(&e.mat, false, e.shell > 0, outKey{}) != key {
					break
				}
				run++
			}
		}
		runs++
		i += run
	}
	return runs
}

// checkOrder reports that every draw appears once, that the classes come
// out in order, and that blended draws are farthest first.
func checkOrder(t *testing.T, l drawList, n int) {
	t.Helper()
	if l.len() != n {
		t.Fatalf("sorted %d draws, want %d", l.len(), n)
	}
	seen := make([]bool, n)
	class := -1
	var lastDepth float32
	for i := range l.len() {
		d := l.at(i)
		idx := int(l.order[i])
		if seen[idx] {
			t.Fatalf("draw %d appears twice", idx)
		}
		seen[idx] = true
		c := 0
		if d.blended {
			c = 2
		}
		if d.culled {
			c++
		}
		if c < class {
			t.Fatalf("draw %d of class %d follows class %d", i, c, class)
		}
		if c != class {
			class, lastDepth = c, float32(math.Inf(1))
		}
		if d.blended {
			if d.depth > lastDepth {
				t.Fatalf("blended draw %d at depth %v follows %v", i, d.depth, lastDepth)
			}
			lastDepth = d.depth
		}
	}
}

// TestSortKeyMatchesRecords checks the packed key against the record
// comparator: the same draws in each class, the same blended order, and
// the same instanced runs, which is what the batching depends on.
func TestSortKeyMatchesRecords(t *testing.T) {
	src := sortTestDraws(2000, 4, 16)
	var fast, slow drawQueue
	fast.draws = append(fast.draws, src...)
	slow.draws = append(slow.draws, src...)
	got := fast.sortDraws()
	slow.order = make([]int32, len(src))
	want := slow.sortRecords()
	checkOrder(t, got, len(src))
	checkOrder(t, want, len(src))
	for i := range got.len() {
		a, b := got.at(i), want.at(i)
		if a.blended != b.blended || a.culled != b.culled {
			t.Fatalf("draw %d: classes differ", i)
		}
		if a.blended && a.depth != b.depth {
			t.Fatalf("blended draw %d: depth %v, want %v", i, a.depth, b.depth)
		}
	}
	if a, b := runsOf(got), runsOf(want); a != b {
		t.Errorf("packed key records %d runs, the record sort %d", a, b)
	}
}

// TestSortKeyOverflow gives a frame more shaders than the key's field
// holds, so the sort falls back to comparing records.
func TestSortKeyOverflow(t *testing.T) {
	src := sortTestDraws(2048, 1<<sortShaderBits+3, 16)
	var q drawQueue
	q.draws = append(q.draws, src...)
	if q.buildKeys() {
		t.Fatalf("%d shaders fit an %d-bit field", 1<<sortShaderBits+3, sortShaderBits)
	}
	got := q.sortDraws()
	checkOrder(t, got, len(src))
	var ref drawQueue
	ref.draws = append(ref.draws, src...)
	ref.order = make([]int32, len(src))
	if a, b := runsOf(got), runsOf(ref.sortRecords()); a != b {
		t.Errorf("the fallback records %d runs, the record sort %d", a, b)
	}
}

// TestSortDepthBits checks the depth ordering the blended key packs.
func TestSortDepthBits(t *testing.T) {
	for _, pair := range [][2]float32{{-5, -1}, {-1, 0}, {0, 0.5}, {1, 2}, {2, 1e6}} {
		if depthBits(pair[0]) >= depthBits(pair[1]) {
			t.Errorf("depthBits(%v) does not order before depthBits(%v)", pair[0], pair[1])
		}
	}
}
