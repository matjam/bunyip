package ecs_test

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
)

func TestWorldDefer(t *testing.T) {
	type health struct{ n int }
	w := ecs.NewWorld()
	e := w.SpawnWith(health{1})
	var saved *ecs.Commands
	w.Defer(func(cmd *ecs.Commands) {
		saved = cmd
		w.Each(func(e ecs.Entity, _ *health) {
			cmd.Remove[health](e)
			cmd.Add(e, health{3})
			cmd.Spawn(health{5})
		})
		if h, _ := w.Get[health](e); h.n != 1 || w.Len() != 1 {
			t.Fatal("commands applied before the scope finished")
		}
		return // a normal early return still applies queued work
	})
	if h, _ := w.Get[health](e); h.n != 3 || w.Len() != 2 {
		t.Fatalf("commands not applied in order: health=%v count=%d", h, w.Len())
	}
	if saved.Len() != 0 {
		t.Fatal("completed scope retained pending commands")
	}
}

func TestWorldDeferPanic(t *testing.T) {
	w := ecs.NewWorld()
	e := w.Spawn()
	var saved *ecs.Commands
	sentinel := &struct{}{}
	func() {
		defer func() {
			if got := recover(); got != sentinel {
				t.Fatalf("panic was not propagated: %v", got)
			}
		}()
		w.Defer(func(cmd *ecs.Commands) {
			saved = cmd
			cmd.Despawn(e)
			cmd.Spawn()
			w.Spawn() // direct writes are outside the command scope
			panic(sentinel)
		})
	}()
	if !w.Alive(e) || w.Len() != 2 || saved.Len() != 0 {
		t.Fatalf("panic handling: alive=%t count=%d pending=%d", w.Alive(e), w.Len(), saved.Len())
	}
	w.Defer(func(cmd *ecs.Commands) { cmd.Despawn(e) })
	if w.Alive(e) || w.Len() != 1 {
		t.Fatal("a later scope was affected by the discarded commands")
	}
}

func TestWorldDeferNested(t *testing.T) {
	w := ecs.NewWorld()
	w.Defer(func(outer *ecs.Commands) {
		outer.Spawn()
		w.Defer(func(inner *ecs.Commands) {
			inner.Spawn()
			if w.Len() != 0 {
				t.Fatal("nested scopes applied too soon")
			}
		})
		if w.Len() != 1 || outer.Len() != 1 {
			t.Fatal("inner scope did not finish independently")
		}
	})
	if w.Len() != 2 {
		t.Fatal("outer scope did not apply")
	}
}
