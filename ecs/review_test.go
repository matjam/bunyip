package ecs_test

import (
	"strings"
	"testing"

	"github.com/matjam/bunyip/ecs"
)

type reviewHP struct{ HP int }
type reviewTag struct{}

// Adding a component to the visited entity moves it to a table the walk
// has not reached yet; it must not be visited there a second time.
func TestAddDuringEachVisitsOnce(t *testing.T) {
	w := ecs.NewWorld()
	a := w.SpawnWith(reviewHP{1})
	w.SpawnWith(reviewHP{2}, reviewTag{}) // the {hp, tag} table exists and sorts later
	visits := map[ecs.Entity]int{}
	w.Each(func(e ecs.Entity, h *reviewHP) {
		visits[e]++
		if !w.Has[reviewTag](e) {
			w.Add(e, reviewTag{})
		}
	})
	if visits[a] != 1 {
		t.Errorf("entity visited %d times", visits[a])
	}
}

// A save file whose parent links form a cycle is rejected rather than
// loaded into a hierarchy every ancestor walk would spin on.
func TestLoadRejectsParentCycle(t *testing.T) {
	ecs.Register[reviewHP]("reviewHP")
	doc := `{"version":1,"entities":[
		{"id":1,"parent":2,"components":{"reviewHP":{"HP":1}}},
		{"id":2,"parent":1,"components":{"reviewHP":{"HP":2}}}]}`
	w := ecs.NewWorld()
	if err := w.Load(strings.NewReader(doc)); err == nil {
		t.Fatal("a parent cycle loaded without error")
	}
}
