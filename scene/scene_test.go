package scene

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type health struct{ HP int }
type name string

func TestEntitiesAndComponents(t *testing.T) {
	w := NewWorld()
	a, b, c := w.Spawn(), w.Spawn(), w.Spawn()
	Set(w, a, health{10})
	Set(w, b, health{20})
	Set(w, b, name("bob"))
	Set(w, c, name("carol"))
	if h, ok := Get[health](w, b); !ok || h.HP != 20 {
		t.Fatal("get failed")
	}
	if Has[name](w, a) || !Has[name](w, c) {
		t.Fatal("has wrong")
	}
	Each(w, func(e Entity, h *health) { h.HP++ })
	if h, _ := Get[health](w, a); h.HP != 11 {
		t.Fatal("in-place edit lost")
	}
	both := 0
	Each2(w, func(e Entity, h *health, n *name) {
		both++
		if e != b {
			t.Fatal("wrong entity in Each2")
		}
	})
	if both != 1 || Count[health](w) != 2 || w.Count() != 3 {
		t.Fatalf("counts %d %d %d", both, Count[health](w), w.Count())
	}
	// Removing from the middle keeps the store consistent (swap-remove).
	Remove[health](w, a)
	if Has[health](w, a) || !Has[health](w, b) || Count[health](w) != 1 {
		t.Fatal("remove broke the store")
	}
	w.Despawn(b)
	if w.Alive(b) || Has[health](w, b) || Has[name](w, b) || w.Count() != 2 {
		t.Fatal("despawn incomplete")
	}
	// The slot is reused with a new generation; the old handle stays dead.
	d := w.Spawn()
	if d == b || !w.Alive(d) || w.Alive(b) {
		t.Fatal("generation not bumped")
	}
	Set(w, d, name("dave"))
	if _, ok := Get[name](w, b); ok {
		t.Fatal("stale handle reads the new entity's component")
	}
	Set(w, b, name("ghost")) // ignored: b is dead
	if n, _ := Get[name](w, d); *n != "dave" {
		t.Fatal("dead handle overwrote a live component")
	}
}

func TestHierarchy(t *testing.T) {
	w := NewWorld()
	root, child, grandchild := w.Spawn(), w.Spawn(), w.Spawn()
	w.SetParent(child, root)
	w.SetParent(grandchild, child)
	w.SetParent(root, grandchild) // cycle: refused
	if p, ok := w.Parent(root); ok || p.Valid() {
		t.Fatal("cycle accepted")
	}
	Set(w, root, gfx.At(10, 0, 0))
	Set(w, child, gfx.At(0, 5, 0).Scaled(2))
	Set(w, grandchild, gfx.At(1, 0, 0))
	p := w.WorldMatrix(grandchild).MulPoint(lin.Vec3{})
	if math.Abs(float64(p.X-12)) > 1e-5 || math.Abs(float64(p.Y-5)) > 1e-5 {
		t.Fatalf("world position %v, want (12, 5, 0)", p)
	}
	if len(w.Children(root)) != 1 || w.Children(root)[0] != child {
		t.Fatal("children wrong")
	}
	w.SetParent(child, None)
	if _, ok := w.Parent(child); ok || len(w.Children(root)) != 0 {
		t.Fatal("detach failed")
	}
	w.SetParent(child, root)
	w.Despawn(root)
	if w.Alive(child) || w.Alive(grandchild) || w.Count() != 0 {
		t.Fatal("descendants survived despawn")
	}
}
