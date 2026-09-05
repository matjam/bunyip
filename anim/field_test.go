package anim_test

import (
	"testing"

	"github.com/matjam/bunyip/anim"
	"github.com/matjam/bunyip/ecs"
)

func TestCurveField(t *testing.T) {
	type stats struct{ opacity, other float32 }
	type moved struct{}
	curve := anim.Floats(anim.Num(0, 0), anim.Num(1, 10))
	calls := 0
	track := curve.Field(func(s *stats) *float32 { calls++; return &s.opacity })
	w := ecs.NewWorld()
	e := w.SpawnWith(stats{opacity: 2, other: 7})
	track.Apply(w, e, 1, 0.25)
	if s, _ := w.Get[stats](e); s.opacity != 4 || s.other != 7 {
		t.Fatalf("field blend changed the wrong value: %+v", s)
	}
	before := calls
	track.Apply(w, w.Spawn(), 1, 1)
	if calls != before {
		t.Fatal("accessor called for an entity missing the component")
	}
	// A clip caches its binding plan, but must reacquire the component
	// after structural changes instead of retaining its old field pointer.
	clip := anim.NewClip("fade", anim.Once, track)
	clip.Apply(w, e, 0, 1)
	w.Add(e, moved{})
	clip.Apply(w, e, 0.5, 1)
	if s, _ := w.Get[stats](e); s.opacity != 5 || s.other != 7 {
		t.Fatalf("field after moving archetypes: %+v", s)
	}
	if track.Duration() != 1 {
		t.Fatalf("field duration=%g", track.Duration())
	}
}

func TestCurveFieldCrossFade(t *testing.T) {
	type opacity struct{ value float32 }
	field := func(o *opacity) *float32 { return &o.value }
	old := anim.NewClip("old", anim.Loop, anim.Floats(anim.Num(0, 2), anim.Num(1, 2)).Field(field))
	next := anim.NewClip("next", anim.Loop, anim.Floats(anim.Num(0, 10), anim.Num(1, 10)).Field(field))
	w := ecs.NewWorld()
	w.AddSystem("animation", anim.System)
	e := w.SpawnWith(opacity{})
	anim.PlayerOf(w, e).Play(old)
	w.Update(0.1)
	anim.PlayerOf(w, e).CrossFade(next, 1)
	w.Update(0.25)
	if o, _ := w.Get[opacity](e); o.value != 4 {
		t.Fatalf("crossfade value=%g, want 4", o.value)
	}
}
