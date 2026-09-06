package engine

import (
	"testing"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
)

func TestLoopModifierReleaseAfterKey(t *testing.T) {
	l := newPauseLoop(Config{})
	in := l.input.Game().(*input.State)
	l.handleEvents([]platform.Event{
		{Kind: platform.EventKeyDown, Key: input.KeyLeftShift},
		{Kind: platform.EventModifiers, Mods: input.ModShift},
	})
	l.input.EndUpdate()
	l.input.EndFrame()
	l.handleEvents([]platform.Event{
		{Kind: platform.EventKeyUp, Key: input.KeyLeftShift, Mods: input.ModShift},
		{Kind: platform.EventModifiers},
	})
	for _, drawing := range []bool{false, true} {
		l.input.SetDrawing(drawing)
		if in.Mods() != 0 || in.KeyDown(input.KeyLeftShift) || !in.KeyReleased(input.KeyLeftShift) {
			t.Errorf("drawing %v: Shift release left modifiers=%v down=%v released=%v", drawing, in.Mods(), in.KeyDown(input.KeyLeftShift), in.KeyReleased(input.KeyLeftShift))
		}
		for key := input.Key(0); key < input.KeyCount; key++ {
			if in.KeyPressed(key) || in.KeyRepeated(key) {
				t.Errorf("drawing %v: modifier release created a press/repeat of %s", drawing, key)
			}
		}
	}
}

func TestLoopModifiersWithoutKeyEdges(t *testing.T) {
	for _, mods := range []input.Mods{input.ModShift, input.ModControl | input.ModAlt | input.ModSuper, input.ModCapsLock | input.ModNumLock, 0} {
		t.Run(mods.String(), func(t *testing.T) {
			l := newPauseLoop(Config{})
			in := l.input.Game().(*input.State)
			// A genuine key and composition already exist. A modifier-only
			// update must preserve their held state and text without edges.
			l.handleEvents([]platform.Event{
				{Kind: platform.EventKeyDown, Key: input.KeyA},
				{Kind: platform.EventCompose, Text: "composing"},
			})
			l.input.EndUpdate()
			l.input.EndFrame()
			l.handleEvents([]platform.Event{{Kind: platform.EventModifiers, Mods: mods}})
			for _, drawing := range []bool{false, true} {
				l.input.SetDrawing(drawing)
				if in.Mods() != mods || !in.KeyDown(input.KeyA) || in.Composition() != "composing" || len(in.Chars()) != 0 {
					t.Errorf("drawing %v: modifiers changed other state: mods=%v down=%v composition=%q chars=%q", drawing, in.Mods(), in.KeyDown(input.KeyA), in.Composition(), in.Chars())
				}
				for key := input.Key(0); key < input.KeyCount; key++ {
					if in.KeyPressed(key) || in.KeyReleased(key) || in.KeyRepeated(key) {
						t.Errorf("drawing %v: modifier update created an edge of %s", drawing, key)
					}
				}
			}
			l.input.SetDrawing(false)
			l.input.EndUpdate()
			l.input.EndFrame()
			if in.Mods() != mods {
				t.Error("modifier state did not survive frame/update boundaries")
			}
		})
	}
}

func TestLoopModifierFocusLifecycle(t *testing.T) {
	l := newPauseLoop(Config{})
	in := l.input.Game().(*input.State)
	for _, step := range []struct {
		name   string
		events []platform.Event
		mods   input.Mods
	}{
		{"focused locks", []platform.Event{{Kind: platform.EventModifiers, Mods: input.ModCapsLock | input.ModNumLock}}, input.ModCapsLock | input.ModNumLock},
		{"focus loss clears", []platform.Event{{Kind: platform.EventFocus}}, 0},
		{"pointer modifiers without keyboard focus", []platform.Event{{Kind: platform.EventModifiers, Mods: input.ModShift}}, input.ModShift},
		{"keyboard leave after prior configure focus loss", []platform.Event{{Kind: platform.EventModifiers}}, 0},
		{"focus regain then lock state", []platform.Event{{Kind: platform.EventFocus, Focused: true}, {Kind: platform.EventModifiers, Mods: input.ModCapsLock}}, input.ModCapsLock},
	} {
		l.handleEvents(step.events)
		if in.Mods() != step.mods {
			t.Errorf("%s: modifiers=%v, want %v", step.name, in.Mods(), step.mods)
		}
	}
}
