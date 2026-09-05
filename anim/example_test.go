package anim_test

import (
	"fmt"

	"github.com/matjam/bunyip/anim"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tween"
)

func Example() {
	w := ecs.NewWorld()
	w.AddSystem("anim", anim.System) // after the systems that choose clips, before drawing

	// A 3D entity: rise and spin over two seconds, then hold.
	rise := anim.NewClip("rise", anim.Once,
		anim.Position(anim.Vec3s(anim.At(0, lin.V3(0, 0, 0)), anim.AtEased(2, lin.V3(0, 4, 0), tween.OutCubic))),
		anim.Rotation(anim.Quats(anim.At(0, lin.QuatIdentity()), anim.At(2, lin.AxisAngle(lin.V3(0, 1, 0), lin.Radians(180))))),
	)
	cube := w.SpawnWith(gfx.Transform{}, anim.Player{})
	anim.PlayerOf(w, cube).Play(rise)

	// A 2D entity: a looping bob on a sprite.
	bob := anim.NewClip("bob", anim.PingPong,
		anim.Position2(anim.Vec2s(anim.At(0, lin.V2(100, 100)), anim.At(0.5, lin.V2(100, 80)))),
	)
	sprite := w.SpawnWith(gfx.Sprite{Size: lin.V2(32, 32), Color: gfx.White}, anim.Player{})
	anim.PlayerOf(w, sprite).Play(bob)

	for range 4 {
		w.Update(0.5)
	}
	t, _ := w.Get[gfx.Transform](cube)
	s, _ := w.Get[gfx.Sprite](sprite)
	fmt.Printf("cube at y=%.0f, sprite at y=%.0f\n", t.Position.Y, s.Pos.Y)
	for _, ev := range w.Events[anim.Finished]() {
		fmt.Println("finished:", ev.Clip.Name)
	}
	// Output:
	// cube at y=4, sprite at y=100
	// finished: rise
}

func ExamplePlayer_CrossFade() {
	w := ecs.NewWorld()
	w.AddSystem("anim", anim.System)
	idle := anim.NewClip("idle", anim.Loop, anim.Scale(anim.Vec3s(anim.At(0, lin.V3(1, 1, 1)), anim.At(1, lin.V3(1, 1, 1)))))
	jump := anim.NewClip("jump", anim.Once, anim.Scale(anim.Vec3s(anim.At(0, lin.V3(2, 2, 2)), anim.At(1, lin.V3(2, 2, 2)))))
	e := w.SpawnWith(gfx.Transform{}, anim.Player{})
	p := anim.PlayerOf(w, e)
	p.Play(idle)
	w.Update(0.1)
	p.CrossFade(jump, 0.5) // blend from idle's scale to jump's over half a second
	w.Update(0.25)
	t, _ := w.Get[gfx.Transform](e)
	fmt.Printf("%.1f\n", t.Scale.X)
	// Output:
	// 1.5
}

func ExampleCurve_Field() {
	type Light struct{ Intensity float32 }
	w := ecs.NewWorld()
	e := w.SpawnWith(Light{Intensity: 1})
	fade := anim.Floats(anim.Num(0, 1), anim.Num(1, 0)).Field(
		func(l *Light) *float32 { return &l.Intensity })
	fade.Apply(w, e, 0.25, 1)
	l, _ := w.Get[Light](e)
	fmt.Println(l.Intensity)
	// Output:
	// 0.75
}
