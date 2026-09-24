package ecs_test

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
)

// Despawning a parent with many children removes every child and leaves
// the hierarchy around it intact: the grandparent loses only the parent
// from its list, and a sibling subtree is untouched.
func TestDespawnWideParent(t *testing.T) {
	const n = 20000
	w := ecs.NewWorld()
	root := w.SpawnWith(gfx.Transform{})
	parent := w.SpawnWith(gfx.Transform{})
	sibling := w.SpawnWith(gfx.Transform{})
	ecs.SetParent(w, parent, root)
	ecs.SetParent(w, sibling, root)
	nephew := w.SpawnWith(gfx.Transform{})
	ecs.SetParent(w, nephew, sibling)
	kids := make([]ecs.Entity, n)
	for i := range kids {
		kids[i] = w.SpawnWith(gfx.Transform{})
		ecs.SetParent(w, kids[i], parent)
	}
	// A grandchild under one of the children goes too.
	grand := w.SpawnWith(gfx.Transform{})
	ecs.SetParent(w, grand, kids[n/2])

	w.Despawn(parent)

	if w.Alive(parent) || w.Alive(grand) {
		t.Fatal("parent or grandchild survived")
	}
	for _, k := range kids {
		if w.Alive(k) {
			t.Fatalf("child %v survived", k)
		}
	}
	if w.Len() != 3 {
		t.Fatalf("%d entities left, want root, sibling and nephew", w.Len())
	}
	if got := ecs.ChildrenOf(w, root); len(got) != 1 || got[0] != sibling {
		t.Fatalf("root's children = %v, want [%v]", got, sibling)
	}
	if got := ecs.ChildrenOf(w, sibling); len(got) != 1 || got[0] != nephew {
		t.Fatalf("sibling's children = %v, want [%v]", got, nephew)
	}
	if p, ok := ecs.ParentOf(w, nephew); !ok || p != sibling {
		t.Fatal("nephew lost its parent")
	}
	// Reused slots start clean.
	fresh := w.SpawnWith(gfx.Transform{})
	if _, ok := ecs.ParentOf(w, fresh); ok || len(ecs.ChildrenOf(w, fresh)) != 0 {
		t.Fatal("a reused slot kept hierarchy links")
	}
}

// Despawning a child directly still detaches it from a live parent.
func TestDespawnChildDetaches(t *testing.T) {
	w := ecs.NewWorld()
	p := w.Spawn()
	a, b, c := w.Spawn(), w.Spawn(), w.Spawn()
	for _, k := range []ecs.Entity{a, b, c} {
		ecs.SetParent(w, k, p)
	}
	w.Despawn(b)
	if got := ecs.ChildrenOf(w, p); len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("children after despawn = %v", got)
	}
}

type unregA struct {
	N    int
	Name string
}
type unregB struct{ V []int }

// A type only ever passed through SpawnWith keeps its values through
// spawns, despawns and table moves in reflect-backed columns, and keeps
// them again when a generic call upgrades the columns to typed storage.
func TestSpawnWithUnregisteredUpgrade(t *testing.T) {
	w := ecs.NewWorld()
	var ents []ecs.Entity
	for i := range 50 {
		ents = append(ents, w.SpawnWith(unregA{N: i, Name: string(rune('a' + i%26))}, unregB{V: []int{i}}))
	}
	// Despawn every third one, which swaps rows in the columns.
	live := map[ecs.Entity]int{}
	for i, e := range ents {
		if i%3 == 0 {
			w.Despawn(e)
			continue
		}
		live[e] = i
	}
	// Move some into another table through the any-valued path.
	w.Defer(func(c *ecs.Commands) {
		for e, i := range live {
			if i%2 == 0 {
				c.Add(e, perfT0{V: float32(i)})
			}
		}
	})
	// The first generic read upgrades the columns.
	for e, i := range live {
		a, ok := w.Get[unregA](e)
		if !ok || a.N != i || a.Name != string(rune('a'+i%26)) {
			t.Fatalf("entity %d: got %+v", i, a)
		}
		b, ok := w.Get[unregB](e)
		if !ok || len(b.V) != 1 || b.V[0] != i {
			t.Fatalf("entity %d: got %+v", i, b)
		}
	}
	if n := w.Count[unregA](); n != len(live) {
		t.Fatalf("count %d, want %d", n, len(live))
	}
}

// Spawning and despawning a registered type through SpawnWith allocates
// nothing per entity once the table has grown: the values below are
// constants, so the caller boxes nothing either.
func TestSpawnWithRegisteredAllocs(t *testing.T) {
	w := ecs.NewWorld()
	for range 100 {
		w.Despawn(w.SpawnWith(perfVel{}, perfHealth{HP: 10}))
	}
	allocs := testing.AllocsPerRun(1000, func() {
		w.Despawn(w.SpawnWith(perfVel{}, perfHealth{HP: 10}))
	})
	if allocs != 0 {
		t.Fatalf("SpawnWith and Despawn allocated %v times", allocs)
	}
}

// The same holds for a type only ever passed through SpawnWith, which
// has reflect-backed columns.
func TestSpawnWithUnregisteredAllocs(t *testing.T) {
	w := ecs.NewWorld()
	for range 100 {
		w.Despawn(w.SpawnWith(unregA{N: 1}))
	}
	allocs := testing.AllocsPerRun(1000, func() {
		w.Despawn(w.SpawnWith(unregA{N: 1}))
	})
	if allocs != 0 {
		t.Fatalf("SpawnWith and Despawn allocated %v times", allocs)
	}
}

// SpawnWith refuses a pointer component even when a generic call has
// already registered the pointer type.
func TestSpawnWithRejectsPointer(t *testing.T) {
	w := ecs.NewWorld()
	e := w.Spawn()
	w.Add(e, &unregA{})
	defer func() {
		if recover() == nil {
			t.Fatal("SpawnWith accepted a pointer")
		}
	}()
	w.SpawnWith(&unregA{})
}

// Commands keep no closure or argument slice per recorded spawn.
func TestCommandsSpawnAllocs(t *testing.T) {
	w := ecs.NewWorld()
	run := func() {
		w.Defer(func(c *ecs.Commands) {
			for range 100 {
				c.Spawn(perfVel{}, perfHealth{HP: 10})
			}
		})
		w.Each(func(e ecs.Entity, _ *perfHealth) { w.Despawn(e) })
	}
	run()
	// The deferred scope's closure and the buffer's reset account for
	// the few allocations left; none are per command.
	if allocs := testing.AllocsPerRun(100, run); allocs > 4 {
		t.Fatalf("100 deferred spawns allocated %v times", allocs)
	}
}
