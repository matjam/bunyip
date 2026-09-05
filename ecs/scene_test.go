package ecs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// sceneScene builds the scene the round-trip tests use: a root with two
// children, one of which points at the other through an Entity field.
func sceneScene(t *testing.T) *Scene {
	t.Helper()
	s := NewScene("test level")
	s.SetProperty("music", "theme.ogg")
	s.SetProperty("gravity", 9.5)
	root, err := s.AddEntity("root", gfx.At(1, 0, 0), pos{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	leader, err := s.AddEntity("leader", gfx.At(0, 2, 0), inventory{Items: []string{"sword"}})
	if err != nil {
		t.Fatal(err)
	}
	minion, err := s.AddEntity("minion", gfx.At(0, 0, 3), follows{Leader: SceneRef(leader)},
		target{Who: SceneRef(root), Others: []Entity{SceneRef(leader), None}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetParent(leader, root)
	s.SetParent(minion, root)
	return s
}

func TestSceneEncodeRoundTrip(t *testing.T) {
	s := sceneScene(t)
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("no version in the document:\n%s", data)
	}
	if !strings.Contains(string(data), `"test.pos"`) {
		t.Fatalf("no registered component name:\n%s", data)
	}
	back, err := ParseScene(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != "test level" {
		t.Fatalf("name %q", back.Name)
	}
	if back.Properties["music"] != "theme.ogg" || back.Properties["gravity"] != 9.5 {
		t.Fatalf("properties %v", back.Properties)
	}
	if len(back.Entities) != 3 {
		t.Fatalf("entities %d", len(back.Entities))
	}
	if back.Entities[2].Name != "minion" || back.Entities[2].Parent != 1 {
		t.Fatalf("third entity %+v", back.Entities[2])
	}
	again, err := back.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("second encode differs:\n%s\n%s", data, again)
	}
}

func TestSceneInstantiateExportRoundTrip(t *testing.T) {
	s := sceneScene(t)
	w := NewWorld()
	inst, err := w.Instantiate(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Roots) != 1 || len(inst.Spawned) != 3 || w.Len() != 3 {
		t.Fatalf("roots %v spawned %v count %d", inst.Roots, inst.Spawned, w.Len())
	}
	root, ok := inst.Entity("root")
	if !ok || root != inst.Roots[0] {
		t.Fatalf("root lookup %v %v", root, inst.Roots)
	}
	leader, _ := inst.Entity("leader")
	minion, ok := inst.Entity("minion")
	if !ok {
		t.Fatal("minion not named")
	}
	if _, ok := inst.Entity("nobody"); ok {
		t.Fatal("unknown name found")
	}
	if kids := ChildrenOf(w, root); len(kids) != 2 || kids[0] != leader || kids[1] != minion {
		t.Fatalf("children %v", kids)
	}
	if p, _ := w.Get[pos](root); p == nil || *p != (pos{1, 2}) {
		t.Fatalf("root pos %v", p)
	}
	if f, _ := w.Get[follows](minion); f == nil || f.Leader != leader {
		t.Fatalf("follows %v, want leader %v", f, leader)
	}
	tg, _ := w.Get[target](minion)
	if tg == nil || tg.Who != root || len(tg.Others) != 2 || tg.Others[0] != leader || tg.Others[1] != None {
		t.Fatalf("target not remapped: %+v", tg)
	}
	if n, ok := NameOf(w, minion); !ok || n != "minion" {
		t.Fatalf("name component %q", n)
	}
	// The minion sits at (1, 0, 3) in the world: its own z under the
	// root's x.
	if p := WorldMatrix(w, minion).MulPoint(lin.Vec3{}); p != (lin.V3(1, 0, 3)) {
		t.Fatalf("world position %v", p)
	}

	out, err := w.ExportScene(root)
	if err != nil {
		t.Fatal(err)
	}
	out.Name = s.Name
	out.Properties = s.Properties
	first, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	second, err := out.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("export differs from the source scene:\n%s\n%s", first, second)
	}
}

func TestSceneExportWholeWorld(t *testing.T) {
	w := NewWorld()
	a := w.SpawnWith(pos{1, 1})
	b := w.SpawnWith(pos{2, 2})
	SetParent(w, b, a)
	loose := w.SpawnWith(pos{3, 3}, target{Who: b})
	s, err := w.ExportScene()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entities) != 3 {
		t.Fatalf("entities %d", len(s.Entities))
	}
	if s.Entities[0].Parent != 0 || s.Entities[1].Parent != 1 || s.Entities[2].Parent != 0 {
		t.Fatalf("parents %+v", s.Entities)
	}
	var tg target
	if err := json.Unmarshal(s.Entities[2].Components["test.target"], &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Who != SceneRef(2) {
		t.Fatalf("reference %v, want scene number 2", tg.Who)
	}
	// A reference outside the exported set becomes None.
	only, err := w.ExportScene(loose)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(only.Entities[0].Components["test.target"], &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Who != None {
		t.Fatalf("outside reference %v, want None", tg.Who)
	}
}

func TestSceneExportUnregistered(t *testing.T) {
	w := NewWorld()
	w.SpawnWith(pos{1, 1}, unregistered{2})
	var ue *UnregisteredError
	if _, err := w.ExportScene(); !errors.As(err, &ue) || ue.Names[0] != "ecs.unregistered" {
		t.Fatalf("error %v, want UnregisteredError", err)
	}
}

func TestSceneInstancesAreIndependent(t *testing.T) {
	s := sceneScene(t)
	w := NewWorld()
	first, err := w.Instantiate(s)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.Instantiate(s)
	if err != nil {
		t.Fatal(err)
	}
	if w.Len() != 6 {
		t.Fatalf("count %d", w.Len())
	}
	for _, e := range first.Spawned {
		for _, o := range second.Spawned {
			if e == o {
				t.Fatalf("instances share %v", e)
			}
		}
	}
	f, _ := first.Entity("leader")
	sec, _ := second.Entity("leader")
	inv, _ := w.Get[inventory](f)
	inv.Items[0] = "axe"
	if other, _ := w.Get[inventory](sec); other.Items[0] != "sword" {
		t.Fatal("instances share a slice")
	}
	// Each instance's references point inside itself.
	fm, _ := first.Entity("minion")
	sm, _ := second.Entity("minion")
	if fl, _ := w.Get[follows](fm); fl.Leader != f {
		t.Fatalf("first minion follows %v, want %v", fl.Leader, f)
	}
	if sl, _ := w.Get[follows](sm); sl.Leader != sec {
		t.Fatalf("second minion follows %v, want %v", sl.Leader, sec)
	}

	// Despawning one instance leaves the other whole, including an
	// entity added under one of its roots afterwards.
	extra := w.SpawnWith(pos{9, 9})
	SetParent(w, extra, first.Roots[0])
	first.Despawn(w)
	if w.Len() != 3 || w.Alive(extra) {
		t.Fatalf("count %d after despawn, extra alive %v", w.Len(), w.Alive(extra))
	}
	for _, e := range second.Spawned {
		if !w.Alive(e) {
			t.Fatalf("%v of the other instance went too", e)
		}
	}
}

func TestSceneInstantiateOptions(t *testing.T) {
	s := NewScene("bits")
	if _, err := s.AddEntity("body", gfx.At(1, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntity("flat", gfx.At2(2, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntity("bare", pos{0, 0}); err != nil {
		t.Fatal(err)
	}
	w := NewWorld()
	under := w.Spawn()
	inst, err := w.Instantiate(s, InstantiateOptions{Parent: under, Offset: lin.V3(10, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Roots) != 3 || len(ChildrenOf(w, under)) != 3 {
		t.Fatalf("roots %v, children %v", inst.Roots, ChildrenOf(w, under))
	}
	body, _ := inst.Entity("body")
	if tr, _ := w.Get[gfx.Transform](body); tr.Position != lin.V3(11, 1, 1) {
		t.Fatalf("offset transform %v", tr.Position)
	}
	flat, _ := inst.Entity("flat")
	if tr, _ := w.Get[gfx.Transform2](flat); tr.Position != lin.V2(12, 2) {
		t.Fatalf("offset transform2 %v", tr.Position)
	}
	bare, _ := inst.Entity("bare")
	if w.Has[gfx.Transform](bare) {
		t.Fatal("offset added a transform to an entity without one")
	}
}

func TestScenePrefabReference(t *testing.T) {
	lib := PrefabLibrary{
		"tank": NewPrefab(pos{5, 5}, inventory{Items: []string{"shell"}}).
			Child(NewPrefab(saveTag{}, gfx.At(0, 1, 0))),
	}
	s := NewScene("field")
	// The override replaces the prefab's pos and leaves the rest alone.
	if _, err := s.AddPrefab("first", "tank", pos{7, 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPrefab("second", "tank"); err != nil {
		t.Fatal(err)
	}
	w := NewWorld()
	w.SetResource(lib)
	inst, err := w.Instantiate(s)
	if err != nil {
		t.Fatal(err)
	}
	if w.Len() != 4 || len(inst.Spawned) != 4 || len(inst.Roots) != 2 {
		t.Fatalf("count %d spawned %v roots %v", w.Len(), inst.Spawned, inst.Roots)
	}
	first, _ := inst.Entity("first")
	if p, _ := w.Get[pos](first); p == nil || *p != (pos{7, 8}) {
		t.Fatalf("override not applied: %v", p)
	}
	if inv, _ := w.Get[inventory](first); inv == nil || inv.Items[0] != "shell" {
		t.Fatalf("prefab component lost: %v", inv)
	}
	kids := ChildrenOf(w, first)
	if len(kids) != 1 || !w.Has[saveTag](kids[0]) {
		t.Fatalf("prefab child missing: %v", kids)
	}
	second, _ := inst.Entity("second")
	if p, _ := w.Get[pos](second); p == nil || *p != (pos{5, 5}) {
		t.Fatalf("second instance took the override: %v", p)
	}
	// Despawn takes the prefab children with it.
	inst.Despawn(w)
	if w.Len() != 0 {
		t.Fatalf("count %d after despawn", w.Len())
	}
}

func TestSceneMissingPrefab(t *testing.T) {
	s := NewScene("field")
	if _, err := s.AddPrefab("one", "nosuch"); err != nil {
		t.Fatal(err)
	}
	w := NewWorld()
	_, err := w.Instantiate(s)
	var mp *MissingPrefabError
	if !errors.As(err, &mp) || mp.Name != "nosuch" {
		t.Fatalf("error %v, want MissingPrefabError", err)
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("error text %q does not name the prefab", err)
	}
	if w.Len() != 0 {
		t.Fatal("entities spawned despite the error")
	}
	// A library holding the name but no prefab is missing all the same.
	if _, err := w.Instantiate(s, InstantiateOptions{Prefabs: PrefabLibrary{"nosuch": nil}}); !errors.As(err, &mp) {
		t.Fatalf("nil prefab error %v", err)
	}
}

func TestSceneUnregisteredComponent(t *testing.T) {
	doc := `{"version":1,"entities":[{"name":"a","components":{"test.pos":{"X":1},"nope":{}}}]}`
	s, err := ParseScene([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorld()
	_, err = w.Instantiate(s)
	var ue *UnregisteredError
	if !errors.As(err, &ue) || len(ue.Names) != 1 || ue.Names[0] != "nope" {
		t.Fatalf("error %v, want UnregisteredError naming nope", err)
	}
	if w.Len() != 0 {
		t.Fatal("entities spawned despite the error")
	}
	inst, err := w.Instantiate(s, InstantiateOptions{SkipUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	e := inst.Roots[0]
	if p, _ := w.Get[pos](e); p == nil || p.X != 1 {
		t.Fatalf("known component lost: %v", p)
	}
}

func TestSceneParseErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{"no version", `{"entities":[]}`},
		{"future version", `{"version":2,"entities":[]}`},
		{"parent out of range", `{"version":1,"entities":[{"parent":4}]}`},
		{"own parent", `{"version":1,"entities":[{"parent":1}]}`},
		{"parent cycle", `{"version":1,"entities":[{"parent":2},{"parent":1}]}`},
		{"duplicate name", `{"version":1,"entities":[{"name":"a"},{"name":"a"}]}`},
		{"not json", `{`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseScene([]byte(c.doc)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestSceneBuilderErrors(t *testing.T) {
	s := NewScene("x")
	var ue *UnregisteredError
	if _, err := s.AddEntity("a", unregistered{}); !errors.As(err, &ue) {
		t.Fatalf("unregistered component: %v", err)
	}
	if len(s.Entities) != 0 {
		t.Fatal("entity added despite the error")
	}
	if _, err := s.AddEntity("a", pos{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntity("a", pos{}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err := s.AddEntity("b", &pos{}); err == nil {
		t.Fatal("pointer component accepted")
	}
	// Out of range links are ignored rather than corrupting the scene.
	s.SetParent(1, 9)
	s.SetParent(9, 1)
	s.SetParent(1, 1)
	if s.Entities[0].Parent != 0 {
		t.Fatalf("parent %d", s.Entities[0].Parent)
	}
}

func TestSceneParsedDocumentInstantiates(t *testing.T) {
	doc := `{
	  "version": 1,
	  "name": "hand written",
	  "properties": {"wave": 3},
	  "entities": [
	    {"name": "hero", "components": {"gfx.Transform": {"Position": {"X": 2}}}},
	    {"name": "pet", "parent": 1, "components": {"test.follows": {"Leader": 1}}}
	  ]
	}`
	s, err := ParseScene([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "hand written" || s.Properties["wave"] != float64(3) {
		t.Fatalf("header %q %v", s.Name, s.Properties)
	}
	w := NewWorld()
	inst, err := w.Instantiate(s)
	if err != nil {
		t.Fatal(err)
	}
	hero, _ := inst.Entity("hero")
	pet, _ := inst.Entity("pet")
	if p, ok := ParentOf(w, pet); !ok || p != hero {
		t.Fatal("parent link not rebuilt")
	}
	if f, _ := w.Get[follows](pet); f.Leader != hero {
		t.Fatalf("leader %v, want %v", f.Leader, hero)
	}
}
