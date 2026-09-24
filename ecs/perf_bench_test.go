package ecs_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type perfPos struct{ X, Y, Z float32 }
type perfVel struct{ X, Y, Z float32 }
type perfHealth struct{ HP, Max int32 }
type perfT0 struct{ V float32 }
type perfT1 struct{ V float32 }
type perfT2 struct{ V float32 }
type perfT3 struct{ V float32 }
type perfT4 struct{ V float32 }
type perfT5 struct{ V float32 }
type perfEvent struct {
	E ecs.Entity
	N int
}

func init() {
	ecs.Register[perfPos]("perf.pos")
	ecs.Register[perfVel]("perf.vel")
	ecs.Register[perfHealth]("perf.health")
}

// perfTags adds the tag components picked by the bits of mask, so
// entities spread over 64 archetypes.
func perfTags(w *ecs.World, e ecs.Entity, mask int) {
	if mask&1 != 0 {
		w.Add(e, perfT0{})
	}
	if mask&2 != 0 {
		w.Add(e, perfT1{})
	}
	if mask&4 != 0 {
		w.Add(e, perfT2{})
	}
	if mask&8 != 0 {
		w.Add(e, perfT3{})
	}
	if mask&16 != 0 {
		w.Add(e, perfT4{})
	}
	if mask&32 != 0 {
		w.Add(e, perfT5{})
	}
}

// BenchmarkQuery100k walks 100k entities spread over 64 archetypes with
// a kept Query2.
func BenchmarkQuery100k(b *testing.B) {
	w := ecs.NewWorld()
	for i := range 100000 {
		e := w.SpawnWith(perfPos{}, perfVel{X: 1})
		perfTags(w, e, i%64)
	}
	q := w.Query2[perfPos, perfVel]()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		q.Each(func(_ ecs.Entity, p *perfPos, v *perfVel) {
			p.X += v.X
		})
	}
}

// BenchmarkSpawnDespawn10k spawns and despawns ten thousand
// three-component entities, a frame of heavy churn (bullets, particles).
func BenchmarkSpawnDespawn10k(b *testing.B) {
	w := ecs.NewWorld()
	ents := make([]ecs.Entity, 10000)
	b.Run("SpawnWith", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range ents {
				ents[i] = w.SpawnWith(perfPos{X: float32(i)}, perfVel{}, perfHealth{HP: 10})
			}
			for _, e := range ents {
				w.Despawn(e)
			}
		}
	})
	b.Run("SpawnAdd", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range ents {
				e := w.Spawn()
				w.Add(e, perfPos{X: float32(i)})
				w.Add(e, perfVel{})
				w.Add(e, perfHealth{HP: 10})
				ents[i] = e
			}
			for _, e := range ents {
				w.Despawn(e)
			}
		}
	})
	// The same SpawnWith after Add has upgraded the three columns to
	// typed storage, which is what a world whose systems query these
	// types has.
	b.Run("SpawnWithTyped", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for i := range ents {
				ents[i] = w.SpawnWith(perfPos{X: float32(i)}, perfVel{}, perfHealth{HP: 10})
			}
			for _, e := range ents {
				w.Despawn(e)
			}
		}
	})
	b.Run("Commands", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w.Defer(func(c *ecs.Commands) {
				for i := range ents {
					c.Spawn(perfPos{X: float32(i)}, perfVel{}, perfHealth{HP: 10})
				}
			})
			w.Each(func(e ecs.Entity, _ *perfHealth) { w.Despawn(e) })
		}
	})
}

type perfU1 struct{ X, Y, Z float32 }
type perfU2 struct{ X, Y, Z float32 }
type perfU3 struct{ HP, Max int32 }

// BenchmarkSpawnWithUnregistered is SpawnDespawn10k/SpawnWith for types
// never registered nor named in a generic call, which SpawnWith keeps in
// reflect-backed columns.
func BenchmarkSpawnWithUnregistered(b *testing.B) {
	w := ecs.NewWorld()
	ents := make([]ecs.Entity, 10000)
	b.ReportAllocs()
	for range b.N {
		for i := range ents {
			ents[i] = w.SpawnWith(perfU1{X: float32(i)}, perfU2{}, perfU3{HP: 10})
		}
		for _, e := range ents {
			w.Despawn(e)
		}
	}
}

// BenchmarkUpgrade100k is the first generic call on a type 100k entities
// hold in reflect-backed columns, which moves them to typed storage.
func BenchmarkUpgrade100k(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		w := ecs.NewWorld()
		for i := range 100000 {
			w.SpawnWith(perfU1{X: float32(i)})
		}
		b.StartTimer()
		w.Count[perfU1]()
	}
}

// perfTree spawns roots with a fan of children and grandchildren, n
// entities in all, each with a transform.
func perfTree(w *ecs.World, n int) []ecs.Entity {
	var roots []ecs.Entity
	for len(roots)*21 < n {
		root := w.SpawnWith(gfx.Transform{Position: lin.V3(1, 0, 0), Scale: lin.V3(1, 1, 1)})
		for range 4 {
			c := w.SpawnWith(gfx.Transform{Position: lin.V3(0, 1, 0), Scale: lin.V3(1, 1, 1)})
			ecs.SetParent(w, c, root)
			for range 4 {
				g := w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0, 1), Scale: lin.V3(1, 1, 1)})
				ecs.SetParent(w, g, c)
			}
		}
		roots = append(roots, root)
	}
	return roots
}

// BenchmarkWorldMatrices100k runs the hierarchy pass over about 100k
// entities in trees of 21.
func BenchmarkWorldMatrices100k(b *testing.B) {
	w := ecs.NewWorld()
	perfTree(w, 100000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(0) // the cache lasts one update
		ecs.UpdateWorldMatrices(w)
	}
}

// BenchmarkDespawnCascade builds and despawns 500 trees of 21 entities
// (10500 in all) by their roots.
func BenchmarkDespawnCascade(b *testing.B) {
	w := ecs.NewWorld()
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		roots := perfTree(w, 10500)
		b.StartTimer()
		for _, r := range roots {
			w.Despawn(r)
		}
	}
}

// BenchmarkDespawnWideParent despawns one parent holding thousands of
// direct children (a level root, a particle group).
func BenchmarkDespawnWideParent(b *testing.B) {
	for _, n := range []int{5000, 20000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := ecs.NewWorld()
			w.Query1[gfx.Transform]() // typed storage, as a running game has
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				root := w.SpawnWith(gfx.Transform{})
				for range n {
					ecs.SetParent(w, w.SpawnWith(gfx.Transform{}), root)
				}
				b.StartTimer()
				w.Despawn(root)
			}
		})
	}
}

// BenchmarkPrefabSpawn1000 spawns 1000 copies of a prefab with three
// components and four children.
func BenchmarkPrefabSpawn1000(b *testing.B) {
	child := ecs.NewPrefab(gfx.Transform{Scale: lin.V3(1, 1, 1)}, perfHealth{HP: 1})
	p := ecs.NewPrefab(gfx.Transform{Scale: lin.V3(1, 1, 1)}, perfVel{}, perfHealth{HP: 5}).Child(child, child, child, child)
	w := ecs.NewWorld()
	roots := make([]ecs.Entity, 1000)
	b.ReportAllocs()
	for range b.N {
		for i := range roots {
			roots[i] = p.Spawn(w)
		}
		b.StopTimer()
		for _, r := range roots {
			w.Despawn(r)
		}
		b.StartTimer()
	}
}

// BenchmarkInstantiate500 instantiates a scene document of 500 entities
// with three components each, half of them parented.
func BenchmarkInstantiate500(b *testing.B) {
	s := ecs.NewScene("level")
	for i := range 500 {
		n, err := s.AddEntity("", gfx.Transform{Position: lin.V3(float32(i), 0, 0), Scale: lin.V3(1, 1, 1)}, perfVel{}, perfHealth{HP: 3})
		if err != nil {
			b.Fatal(err)
		}
		if i%2 == 1 {
			s.SetParent(n, n-1)
		}
	}
	w := ecs.NewWorld()
	b.ReportAllocs()
	for range b.N {
		si, err := w.Instantiate(s)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		si.Despawn(w)
		b.StartTimer()
	}
}

// BenchmarkSaveLoad10k saves and loads a world of 10k entities with
// three components each.
func BenchmarkSaveLoad10k(b *testing.B) {
	w := ecs.NewWorld()
	for i := range 10000 {
		w.SpawnWith(gfx.Transform{Position: lin.V3(float32(i), 0, 0), Scale: lin.V3(1, 1, 1)}, perfVel{}, perfHealth{HP: 3})
	}
	var buf bytes.Buffer
	b.Run("Save", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			buf.Reset()
			if err := w.Save(&buf); err != nil {
				b.Fatal(err)
			}
		}
	})
	data := append([]byte(nil), buf.Bytes()...)
	b.Run("Load", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w2 := ecs.NewWorld()
			if err := w2.Load(bytes.NewReader(data)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkEvents10k emits and reads ten thousand events a frame.
func BenchmarkEvents10k(b *testing.B) {
	w := ecs.NewWorld()
	e := w.Spawn()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(0)
		for i := range 10000 {
			w.Emit(perfEvent{E: e, N: i})
		}
		sum := 0
		for _, ev := range w.Events[perfEvent]() {
			sum += ev.N
		}
		_ = sum
	}
}

// BenchmarkGetLoop reads one component through Get for 100k entities,
// the pattern systems use to reach a second component.
func BenchmarkGetLoop(b *testing.B) {
	w := ecs.NewWorld()
	ents := make([]ecs.Entity, 100000)
	for i := range ents {
		ents[i] = w.SpawnWith(perfPos{}, perfVel{X: 1})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, e := range ents {
			if v, ok := w.Get[perfVel](e); ok {
				v.X++
			}
		}
	}
}
