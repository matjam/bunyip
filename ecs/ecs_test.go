package ecs

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type pos struct{ X, Y float32 }
type vel struct{ X, Y float32 }
type hp struct{ HP int }
type tag struct{}

func TestSpawnGetAddRemove(t *testing.T) {
	w := NewWorld()
	a := w.SpawnWith(pos{1, 2}, vel{3, 4})
	b := w.Spawn()
	Add(w, b, pos{5, 6})
	if p, ok := Get[pos](w, a); !ok || p.X != 1 {
		t.Fatal("get after SpawnWith")
	}
	if v, ok := Get[vel](w, b); ok || v != nil {
		t.Fatal("b should have no vel")
	}
	// Adding moves b to a's archetype and keeps its data.
	Add(w, b, vel{7, 8})
	if p, _ := Get[pos](w, b); p.Y != 6 {
		t.Fatal("pos lost when moving archetype")
	}
	if v, _ := Get[vel](w, b); v.X != 7 {
		t.Fatal("vel not stored after move")
	}
	Add(w, b, vel{9, 9}) // replace in place
	if v, _ := Get[vel](w, b); v.X != 9 {
		t.Fatal("Add did not replace")
	}
	Remove[vel](w, a)
	if Has[vel](w, a) || !Has[pos](w, a) {
		t.Fatal("Remove wrong")
	}
	if p, _ := Get[pos](w, a); p.X != 1 {
		t.Fatal("pos lost on remove")
	}
	if len(w.Components(b)) != 2 || w.Count() != 2 {
		t.Fatalf("components %v count %d", w.Components(b), w.Count())
	}
}

func TestDespawnGenerations(t *testing.T) {
	w := NewWorld()
	a := w.SpawnWith(pos{1, 1})
	b := w.SpawnWith(pos{2, 2})
	c := w.SpawnWith(pos{3, 3})
	w.Despawn(b) // middle row: c swaps into its place
	if p, _ := Get[pos](w, c); p.X != 3 {
		t.Fatal("swap-remove corrupted c")
	}
	if w.Alive(b) || w.Count() != 2 {
		t.Fatal("despawn failed")
	}
	d := w.Spawn() // reuses b's slot with a new generation
	if d == b || w.Alive(b) || !w.Alive(d) {
		t.Fatal("generation not bumped")
	}
	if _, ok := Get[pos](w, b); ok {
		t.Fatal("stale handle reads")
	}
	Add(w, b, pos{9, 9}) // ignored
	if Has[pos](w, d) {
		t.Fatal("stale handle wrote to the new entity")
	}
	_ = a
}

func TestQueries(t *testing.T) {
	w := NewWorld()
	for i := range 10 {
		e := w.SpawnWith(pos{float32(i), 0}, vel{1, 0})
		if i%2 == 0 {
			Add(w, e, hp{i})
		}
		if i%3 == 0 {
			Add(w, e, tag{})
		}
	}
	w.SpawnWith(pos{100, 0}) // no velocity: not matched
	q := NewQuery2[pos, vel](w)
	if q.Count() != 10 {
		t.Fatalf("count %d", q.Count())
	}
	q.Each(func(e Entity, p *pos, v *vel) { p.X += v.X })
	sum := float32(0)
	NewQuery1[pos](w).Each(func(e Entity, p *pos) { sum += p.X })
	if sum != 45+10+100 {
		t.Fatalf("sum %v", sum)
	}
	if n := NewQuery1[pos](w, With[hp](), Without[tag]()).Count(); n != 3 { // 2, 4, 8
		t.Fatalf("filtered count %d", n)
	}
	// A new archetype after the query was made is picked up.
	w.SpawnWith(pos{0, 0}, vel{0, 0}, hp{1}, tag{})
	if q.Count() != 11 {
		t.Fatal("query did not refresh after a new archetype")
	}
	// Despawning while iterating is safe.
	NewQuery1[hp](w).Each(func(e Entity, h *hp) {
		if h.HP < 5 {
			w.Despawn(e)
		}
	})
	if Count[hp](w) != 3 { // 6, 8 remain plus the last spawned (hp 1)? no: 1 < 5 despawned; 6, 8 remain
		if Count[hp](w) != 2 {
			t.Fatalf("hp count after despawn %d", Count[hp](w))
		}
	}
	// Adding a component to the visited entity while iterating is safe.
	NewQuery1[vel](w).Each(func(e Entity, v *vel) { Add(w, e, tag{}) })
	if Count[tag](w) != Count[vel](w) {
		t.Fatal("tag not added to every vel entity")
	}
}

func TestSpawnWithThenTyped(t *testing.T) {
	// Types first seen through SpawnWith (no generics) are upgraded to
	// typed columns when a generic function meets them.
	w := NewWorld()
	e := w.SpawnWith(hp{4})
	if h, ok := Get[hp](w, e); !ok || h.HP != 4 {
		t.Fatal("get through reflect column")
	}
	NewQuery1[hp](w).Each(func(e Entity, h *hp) { h.HP++ })
	if h, _ := Get[hp](w, e); h.HP != 5 {
		t.Fatal("upgrade lost data")
	}
	var c Commands
	c.Add(e, pos{1, 1}, hp{7})
	c.Spawn(pos{2, 2})
	c.Despawn(e)
	if c.Len() != 3 {
		t.Fatal("commands not recorded")
	}
	c.Apply(w)
	if w.Alive(e) || Count[pos](w) != 1 || c.Len() != 0 {
		t.Fatal("commands not applied")
	}
}

func TestResourcesSystemsEvents(t *testing.T) {
	type score struct{ N int }
	type scored struct{ N int }
	w := NewWorld()
	SetResource(w, score{})
	w.AddSystem("emit", func(w *World, dt float64) { Emit(w, scored{10}) })
	w.AddSystem("apply", func(w *World, dt float64) {
		for _, ev := range Events[scored](w) {
			Resource[score](w).N += ev.N
		}
	})
	w.Update(1.0 / 60)
	w.Update(1.0 / 60)
	// Events are cleared at the start of each Update, so "apply" (after
	// "emit") sees exactly one event per frame.
	if got := Resource[score](w).N; got != 20 {
		t.Fatalf("score %d", got)
	}
	if len(Events[scored](w)) != 1 {
		t.Fatal("Draw should still see the last Update's events")
	}
	if MustResource[score](w) == nil || Resource[hp](w) != nil {
		t.Fatal("resources wrong")
	}
	if len(w.Stats()) != 2 || w.Stats()[0].Name != "emit" {
		t.Fatal("stats missing")
	}
}

func TestHierarchy(t *testing.T) {
	w := NewWorld()
	root, child, grandchild := w.Spawn(), w.Spawn(), w.Spawn()
	SetParent(w, child, root)
	SetParent(w, grandchild, child)
	SetParent(w, root, grandchild) // cycle: refused
	if _, ok := ParentOf(w, root); ok {
		t.Fatal("cycle accepted")
	}
	Add(w, root, gfx.At(10, 0, 0))
	Add(w, child, gfx.At(0, 5, 0).Scaled(2))
	Add(w, grandchild, gfx.At(1, 0, 0))
	p := WorldMatrix(w, grandchild).MulPoint(lin.Vec3{})
	if math.Abs(float64(p.X-12)) > 1e-5 || math.Abs(float64(p.Y-5)) > 1e-5 {
		t.Fatalf("world position %v", p)
	}
	// The cached pass agrees with the walk, entity for entity, and a
	// later Update drops it back to the walk.
	UpdateWorldMatrices(w)
	for _, e := range []Entity{root, child, grandchild} {
		want := WorldMatrix(w, e)
		w.wmat.valid = false
		if got := WorldMatrix(w, e); got != want {
			t.Fatalf("cached matrix for %v: %v, walked %v", e, want, got)
		}
		w.wmat.valid = true
	}
	w.Update(0)
	if w.wmat.valid && w.wmat.stamp == w.updates {
		t.Fatal("cache survived an Update")
	}
	if ch := ChildrenOf(w, root); len(ch) != 1 || ch[0] != child {
		t.Fatal("children wrong")
	}
	SetParent(w, child, None)
	if _, ok := ParentOf(w, child); ok || len(ChildrenOf(w, root)) != 0 {
		t.Fatal("detach failed")
	}
	SetParent(w, child, root)
	w.Despawn(root)
	if w.Alive(child) || w.Alive(grandchild) || w.Count() != 0 {
		t.Fatal("descendants survived")
	}
}

func BenchmarkQuery2(b *testing.B) {
	w := NewWorld()
	for i := range 100000 {
		e := w.SpawnWith(pos{float32(i), 0}, vel{1, 1})
		if i%4 == 0 {
			Add(w, e, hp{1})
		}
	}
	q := NewQuery2[pos, vel](w)
	b.ResetTimer()
	for range b.N {
		q.Each(func(e Entity, p *pos, v *vel) { p.X += v.X; p.Y += v.Y })
	}
}

func BenchmarkGet(b *testing.B) {
	w := NewWorld()
	var es []Entity
	for i := range 1000 {
		es = append(es, w.SpawnWith(pos{float32(i), 0}, vel{1, 1}))
	}
	b.ResetTimer()
	for i := range b.N {
		p, _ := Get[pos](w, es[i%len(es)])
		p.X++
	}
}

func BenchmarkSpawnDespawn(b *testing.B) {
	w := NewWorld()
	for range b.N {
		e := w.SpawnWith(pos{}, vel{})
		w.Despawn(e)
	}
}
