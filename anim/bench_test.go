package anim_test

import (
	"testing"

	"github.com/matjam/bunyip/anim"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// benchClip animates three fields of one gfx.Transform, the shape a
// character clip takes.
func benchClip() *anim.Clip {
	return anim.NewClip("move", anim.Loop,
		anim.Position(anim.Vec3s(anim.At(0, lin.V3(0, 0, 0)), anim.At(1, lin.V3(1, 2, 3)))),
		anim.Scale(anim.Vec3s(anim.At(0, lin.V3(1, 1, 1)), anim.At(1, lin.V3(2, 2, 2)))),
		anim.Rotation(anim.Quats(anim.At(0, lin.QuatIdentity()), anim.At(1, lin.AxisAngle(lin.V3(0, 1, 0), 1)))),
	)
}

// BenchmarkSystemPlayers advances five thousand players of a three-track
// clip, one World.Update worth of animation.
func BenchmarkSystemPlayers(b *testing.B) {
	w := ecs.NewWorld()
	c := benchClip()
	for range 5000 {
		e := w.Spawn()
		w.Add(e, gfx.Transform{Scale: lin.V3(1, 1, 1)})
		w.Add(e, anim.Player{Clip: c, Playing: true})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		anim.System(w, 1.0/60)
	}
}
