package ecs

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestPrefabSpawnIndependent(t *testing.T) {
	w := NewWorld()
	pf := NewPrefab(pos{1, 1}, inventory{Items: []string{"a"}}).
		Child(NewPrefab(pos{0, 1}, saveTag{}), NewPrefab(pos{0, 2}))
	first := pf.Spawn(w)
	second := pf.Spawn(w)
	if first == second || w.Count() != 6 {
		t.Fatalf("count %d", w.Count())
	}
	p1, _ := Get[pos](w, first)
	p1.X = 99
	i1, _ := Get[inventory](w, first)
	i1.Items[0] = "z"
	if p2, _ := Get[pos](w, second); p2.X != 1 {
		t.Fatal("second instance shares pos")
	}
	if i2, _ := Get[inventory](w, second); i2.Items[0] != "a" {
		t.Fatal("second instance shares the slice")
	}
	if pf.Components()[1].(inventory).Items[0] != "a" {
		t.Fatal("prefab changed by an instance")
	}
	k1, k2 := ChildrenOf(w, first), ChildrenOf(w, second)
	if len(k1) != 2 || len(k2) != 2 || k1[0] == k2[0] {
		t.Fatalf("children %v %v", k1, k2)
	}
	if p, ok := ParentOf(w, k1[1]); !ok || p != first {
		t.Fatal("child not parented")
	}
	if !Has[saveTag](w, k1[0]) || Has[saveTag](w, k1[1]) {
		t.Fatal("child components wrong")
	}
	w.Despawn(first)
	if w.Count() != 3 {
		t.Fatal("instance tree not independent")
	}
}

func TestPrefabJSON(t *testing.T) {
	src := `{"components":{"test.pos":{"X":2,"Y":3},"test.tag":{}},"children":[{"components":{"test.inventory":{"Items":["x"]}}}]}`
	pf, err := ParsePrefab([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorld()
	e := pf.Spawn(w)
	if p, _ := Get[pos](w, e); p == nil || p.Y != 3 || !Has[saveTag](w, e) {
		t.Fatal("parsed components wrong")
	}
	kids := ChildrenOf(w, e)
	if len(kids) != 1 {
		t.Fatal("child missing")
	}
	if inv, _ := Get[inventory](w, kids[0]); inv == nil || inv.Items[0] != "x" {
		t.Fatal("child components wrong")
	}
	out, err := json.Marshal(pf)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("round trip\n got %s\nwant %s", out, src)
	}
	var ue *UnregisteredError
	if _, err := ParsePrefab([]byte(`{"components":{"nope":{}}}`)); !errors.As(err, &ue) || ue.Names[0] != "nope" {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := json.Marshal(NewPrefab(unregistered{})); !errors.As(err, &ue) {
		t.Fatalf("unregistered type: %v", err)
	}
	// A prefab taken from a live tree spawns a copy of it.
	again := PrefabOf(w, e)
	f := again.Spawn(w)
	if f == e || len(ChildrenOf(w, f)) != 1 || !Has[saveTag](w, f) || w.Count() != 4 {
		t.Fatal("PrefabOf wrong")
	}
	if _, ok := ParentOf(w, f); ok {
		t.Fatal("PrefabOf kept the parent")
	}
}

type withPtr struct {
	P    *int
	Self Entity
	Any  any
}

func TestClone(t *testing.T) {
	w := NewWorld()
	parent := w.Spawn()
	n := 5
	e := w.SpawnWith(pos{1, 2}, inventory{Items: []string{"a", "b"}}, withPtr{P: &n, Any: []int{1}})
	Add(w, e, withPtr{P: &n, Self: e, Any: []int{1}})
	SetParent(w, e, parent)
	c := Clone(w, e)
	if c == e || !w.Alive(c) || w.Count() != 3 {
		t.Fatal("clone not made")
	}
	if p, ok := ParentOf(w, c); !ok || p != parent {
		t.Fatal("clone not under the same parent")
	}
	if len(ChildrenOf(w, parent)) != 2 {
		t.Fatal("parent does not list the clone")
	}
	inv, _ := Get[inventory](w, e)
	inv.Items[0] = "changed"
	if ci, _ := Get[inventory](w, c); ci.Items[0] != "a" {
		t.Fatal("clone shares the slice")
	}
	wp, _ := Get[withPtr](w, c)
	if wp.P == &n || *wp.P != 5 {
		t.Fatal("pointer not copied")
	}
	if wp.Self != c {
		t.Fatalf("self reference %v, want %v", wp.Self, c)
	}
	wp.Any.([]int)[0] = 2
	if o, _ := Get[withPtr](w, e); o.Any.([]int)[0] != 1 {
		t.Fatal("interface value shared")
	}
	if Clone(w, Entity{id: 99, gen: 0}) != None {
		t.Fatal("dead entity cloned")
	}
}

func TestCloneTree(t *testing.T) {
	w := NewWorld()
	outside := w.Spawn()
	root := w.SpawnWith(pos{0, 0})
	a := w.SpawnWith(pos{1, 0}, follows{Leader: root})
	b := w.SpawnWith(pos{2, 0}, target{Who: outside, Others: []Entity{a}})
	leaf := w.SpawnWith(saveTag{})
	SetParent(w, a, root)
	SetParent(w, b, root)
	SetParent(w, leaf, a)
	SetParent(w, root, outside)

	nroot := CloneTree(w, root)
	if w.Count() != 9 {
		t.Fatalf("count %d", w.Count())
	}
	if p, ok := ParentOf(w, nroot); !ok || p != outside {
		t.Fatal("root copy not under the original parent")
	}
	kids := ChildrenOf(w, nroot)
	if len(kids) != 2 || kids[0] == a || kids[1] == b {
		t.Fatalf("children %v", kids)
	}
	na, nb := kids[0], kids[1]
	if p, _ := Get[pos](w, na); p.X != 1 {
		t.Fatal("children out of order")
	}
	if f, _ := Get[follows](w, na); f.Leader != nroot {
		t.Fatalf("leader %v, want %v", f.Leader, nroot)
	}
	tg, _ := Get[target](w, nb)
	if tg.Who != outside || tg.Others[0] != na {
		t.Fatalf("target %+v", tg)
	}
	nleaf := ChildrenOf(w, na)
	if len(nleaf) != 1 || nleaf[0] == leaf || !Has[saveTag](w, nleaf[0]) {
		t.Fatal("grandchild not copied")
	}
	w.Despawn(nroot)
	if !w.Alive(root) || !w.Alive(leaf) || w.Count() != 5 {
		t.Fatal("copies not independent of the originals")
	}
}

func TestComponentValues(t *testing.T) {
	w := NewWorld()
	e := w.SpawnWith(pos{1, 1}, hp{2})
	vals := w.ComponentValues(e)
	if len(vals) != 2 {
		t.Fatalf("values %v", vals)
	}
	types := w.Components(e)
	for i, v := range vals {
		if types[i] != reflect.TypeOf(v) {
			t.Fatalf("value %d is %T, want %v", i, v, types[i])
		}
	}
	if w.ComponentValues(Entity{}) != nil {
		t.Fatal("dead entity has values")
	}
}
