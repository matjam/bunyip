package ecs_test

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type benchPos struct{ X, Y float32 }
type benchVel struct{ X, Y float32 }

// benchWorld2 builds a world where many entities carry a benchPos and
// only five also carry a benchVel, so Each2 walks almost nothing and the
// cost is whatever setting the query up takes.
func benchWorld2(match, rest int) *ecs.World {
	w := ecs.NewWorld()
	for range rest {
		e := w.Spawn()
		w.Add(e, benchPos{})
	}
	for range match {
		e := w.Spawn()
		w.Add(e, benchPos{})
		w.Add(e, benchVel{X: 1})
	}
	return w
}

func BenchmarkEach2Sparse(b *testing.B) {
	w := benchWorld2(5, 10000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Each2(func(_ ecs.Entity, p *benchPos, v *benchVel) {
			p.X += v.X
		})
	}
}

func BenchmarkEach3Sparse(b *testing.B) {
	w := ecs.NewWorld()
	for range 10000 {
		e := w.Spawn()
		w.Add(e, benchPos{})
	}
	for range 5 {
		e := w.Spawn()
		w.Add(e, benchPos{})
		w.Add(e, benchVel{X: 1})
		w.Add(e, gfx.Transform{})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Each3(func(_ ecs.Entity, p *benchPos, v *benchVel, _ *gfx.Transform) {
			p.X += v.X
		})
	}
}

// benchHierarchy builds chains of the given depth until it has n
// entities, and returns the deepest entity of each chain.
func benchHierarchy(n, depth int) (*ecs.World, []ecs.Entity) {
	w := ecs.NewWorld()
	var leaves []ecs.Entity
	for i := 0; i < n; i += depth {
		parent := ecs.None
		for d := range depth {
			e := w.Spawn()
			w.Add(e, gfx.Transform{Position: lin.V3(1, float32(d), 0), Scale: lin.V3(1, 1, 1)})
			if parent.Valid() {
				ecs.SetParent(w, e, parent)
			}
			parent = e
		}
		leaves = append(leaves, parent)
	}
	return w, leaves
}

func BenchmarkWorldMatrixWalk(b *testing.B) {
	w, leaves := benchHierarchy(10000, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, e := range leaves {
			sink = ecs.WorldMatrix(w, e)
		}
	}
}

func BenchmarkWorldMatrixCached(b *testing.B) {
	w, leaves := benchHierarchy(10000, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ecs.UpdateWorldMatrices(w)
		for _, e := range leaves {
			sink = ecs.WorldMatrix(w, e)
		}
	}
}

// BenchmarkWorldMatrixCachedAll reads every entity's matrix, not just
// the leaves, which is what a draw pass over a hierarchy does.
func BenchmarkWorldMatrixCachedAll(b *testing.B) {
	w, _ := benchHierarchy(10000, 4)
	all := w.Entities()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ecs.UpdateWorldMatrices(w)
		for _, e := range all {
			sink = ecs.WorldMatrix(w, e)
		}
	}
}

// BenchmarkWorldMatrixWalkAll is the same read without the pass.
func BenchmarkWorldMatrixWalkAll(b *testing.B) {
	w, _ := benchHierarchy(10000, 4)
	all := w.Entities()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, e := range all {
			sink = ecs.WorldMatrix(w, e)
		}
	}
}

var sink lin.Mat4
