package ecs

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type saveTag struct{}

// target holds entities in exported fields; Load rewrites them by
// reflection.
type target struct {
	Who    Entity
	Others []Entity
	ByName map[string]Entity
}

// follows holds an entity it rewrites itself.
type follows struct{ Leader Entity }

func (f *follows) Remap(fn func(Entity) Entity) { f.Leader = fn(f.Leader) }

type inventory struct{ Items []string }

type saveScore struct {
	N      int
	Player Entity
}

type unregistered struct{ X int }

func init() {
	Register[pos]("test.pos")
	Register[saveTag]("test.tag")
	Register[target]("test.target")
	Register[follows]("test.follows")
	Register[inventory]("test.inventory")
	Register[saveScore]("test.score")
}

func findTagged(t *testing.T, w *World) Entity {
	t.Helper()
	e, _, ok := w.Query1[saveTag]().First()
	if !ok {
		t.Fatal("no tagged entity")
	}
	return e
}

func TestSaveLoadRoundTrip(t *testing.T) {
	src := NewWorld()
	gone := src.Spawn()
	src.Despawn(gone)
	a := src.SpawnWith(pos{1, 2}, saveTag{}) // reuses gone's slot with generation 1
	b := src.Spawn()
	src.Add(b, pos{3, 4})
	src.Add(b, follows{Leader: a})
	src.Add(b, target{Who: a, Others: []Entity{a, b, None}, ByName: map[string]Entity{"a": a}})
	src.Add(b, inventory{Items: []string{"sword"}})
	root := src.SpawnWith(gfx.At(10, 0, 0))
	child := src.SpawnWith(gfx.At(0, 5, 0))
	SetParent(src, child, root)
	src.SetResource(saveScore{N: 7, Player: b})

	var buf bytes.Buffer
	if err := src.Save(&buf, SaveOptions{Indent: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"test.pos"`) {
		t.Fatalf("save missing registered name:\n%s", buf.String())
	}

	dst := NewWorld()
	dst.Spawn() // an existing entity shifts every loaded handle
	if err := dst.Load(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if dst.Len() != src.Len()+1 {
		t.Fatalf("count %d, want %d", dst.Len(), src.Len()+1)
	}
	na := findTagged(t, dst)
	if p, ok := dst.Get[pos](na); !ok || *p != (pos{1, 2}) {
		t.Fatalf("tagged pos %v", p)
	}
	nb, f, ok := dst.Query1[follows]().First()
	if !ok || f.Leader != na {
		t.Fatalf("follows leader %v, want %v", f, na)
	}
	tg, _ := dst.Get[target](nb)
	if tg.Who != na || len(tg.Others) != 3 || tg.Others[0] != na || tg.Others[1] != nb || tg.Others[2] != None || tg.ByName["a"] != na {
		t.Fatalf("target not remapped: %+v", tg)
	}
	if inv, _ := dst.Get[inventory](nb); len(inv.Items) != 1 || inv.Items[0] != "sword" {
		t.Fatalf("inventory %v", inv)
	}
	if s := dst.Resource[saveScore](); s == nil || s.N != 7 || s.Player != nb {
		t.Fatalf("resource %+v, want player %v", s, nb)
	}
	nchild, _, ok := dst.Query1[gfx.Transform](With[Parent]()).First()
	if !ok {
		t.Fatal("child not loaded")
	}
	nroot, ok := ParentOf(dst, nchild)
	if !ok || len(ChildrenOf(dst, nroot)) != 1 || ChildrenOf(dst, nroot)[0] != nchild {
		t.Fatal("hierarchy not relinked")
	}
	p := WorldMatrix(dst, nchild).MulPoint(lin.Vec3{})
	if math.Abs(float64(p.X-10)) > 1e-5 || math.Abs(float64(p.Y-5)) > 1e-5 {
		t.Fatalf("world position %v", p)
	}
	// Saving the loaded world again produces the same content once the
	// ids are ignored: same number of entities and components.
	var again bytes.Buffer
	if err := dst.Save(&again); err != nil {
		t.Fatal(err)
	}
	var d1, d2 saveDoc
	if err := json.Unmarshal(buf.Bytes(), &d1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again.Bytes(), &d2); err != nil {
		t.Fatal(err)
	}
	if len(d2.Entities) != len(d1.Entities)+1 || len(d2.Resources) != len(d1.Resources) {
		t.Fatalf("second save differs: %d entities, %d resources", len(d2.Entities), len(d2.Resources))
	}
}

func TestSaveUnregistered(t *testing.T) {
	w := NewWorld()
	w.SpawnWith(pos{1, 1}, unregistered{1})
	w.SetResource(hp{3})
	var buf bytes.Buffer
	err := w.Save(&buf)
	var ue *UnregisteredError
	if !errors.As(err, &ue) {
		t.Fatalf("error %v, want UnregisteredError", err)
	}
	if len(ue.Names) != 2 || ue.Names[0] != "ecs.hp" || ue.Names[1] != "ecs.unregistered" {
		t.Fatalf("names %v", ue.Names)
	}
	if buf.Len() != 0 {
		t.Fatal("wrote despite the error")
	}
	if err := w.Save(&buf, SaveOptions{SkipUnregistered: true}); err != nil {
		t.Fatal(err)
	}
	dst := NewWorld()
	if err := dst.Load(&buf); err != nil {
		t.Fatal(err)
	}
	e := dst.Entities()[0]
	if !dst.Has[pos](e) || dst.Has[unregistered](e) || dst.Resource[hp]() != nil {
		t.Fatal("skipped types came through")
	}
}

func TestLoadUnknownName(t *testing.T) {
	doc := `{"version":1,"entities":[{"id":1,"components":{"test.pos":{"X":1,"Y":2},"nope":{}}}],"resources":{"missing":1}}`
	w := NewWorld()
	err := w.Load(strings.NewReader(doc))
	var ue *UnregisteredError
	if !errors.As(err, &ue) || len(ue.Names) != 2 || ue.Names[0] != "missing" || ue.Names[1] != "nope" {
		t.Fatalf("error %v", err)
	}
	if w.Len() != 0 {
		t.Fatal("entities added despite the error")
	}
	if err := w.Load(strings.NewReader(doc), LoadOptions{SkipUnknown: true}); err != nil {
		t.Fatal(err)
	}
	if w.Len() != 1 || w.Count[pos]() != 1 {
		t.Fatal("entity not loaded")
	}
	if err := w.Load(strings.NewReader(`{"version":2,"entities":[]}`)); err == nil {
		t.Fatal("future version accepted")
	}
	if err := w.Load(strings.NewReader(`{"version":1,"entities":[{"id":3,"components":{}},{"id":3,"components":{}}]}`)); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestEntityJSON(t *testing.T) {
	w := NewWorld()
	w.Despawn(w.Spawn())
	e := w.Spawn() // slot 1, generation 1
	type holder struct {
		E    Entity
		Keys map[Entity]int
		Nil  Entity
	}
	b, err := json.Marshal(holder{E: e, Keys: map[Entity]int{e: 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"E":4294967297,"Keys":{"4294967297":1},"Nil":0}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	var h holder
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	if h.E != e || h.Keys[e] != 1 || h.Nil != None {
		t.Fatalf("decoded %+v", h)
	}
	if err := json.Unmarshal([]byte(`{"E":"x"}`), &h); err == nil {
		t.Fatal("bad entity accepted")
	}
}

func TestRegisterConflicts(t *testing.T) {
	Register[pos]("test.pos") // same pair again is fine
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s did not panic", name)
			}
		}()
		fn()
	}
	mustPanic("name reuse", func() { Register[vel]("test.pos") })
	mustPanic("type reuse", func() { Register[pos]("test.other") })
	mustPanic("pointer", func() { Register[*pos]("test.ptr") })
	mustPanic("empty", func() { Register[vel]("") })
}
