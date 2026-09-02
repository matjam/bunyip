package anim

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tween"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestCurve(t *testing.T) {
	c := Floats(Num(1, 10), Num(0, 0), NumEased(2, 30, tween.InQuad)) // unsorted on purpose
	if c.Duration() != 2 || c.Keys[0].Time != 0 {
		t.Fatal("keys not sorted")
	}
	for _, tc := range []struct{ t, want float32 }{{-1, 0}, {0, 0}, {0.5, 5}, {1, 10}, {1.5, 15}, {3, 30}} {
		if got := c.Sample(tc.t); !near(got, tc.want) {
			t.Errorf("Sample(%v) = %v, want %v", tc.t, got, tc.want)
		}
	}
	// InQuad: halfway in time is a quarter of the way in value.
	if got := c.Sample(1.5); !near(got, 15) {
		t.Errorf("eased sample %v", got)
	}
	q := Quats(At(0, lin.QuatIdentity()), At(1, lin.AxisAngle(lin.V3(0, 1, 0), lin.Radians(90))))
	p := q.Sample(0.5).Rotate(lin.V3(1, 0, 0))
	if !near(p.X, float32(math.Sqrt2/2)) || !near(p.Z, -float32(math.Sqrt2/2)) {
		t.Errorf("slerp midpoint %v", p)
	}
	if col := Colors(At(0, gfx.RGB(0, 0, 0)), At(1, gfx.RGB(255, 255, 255))).Sample(0.5); !near(col.R, 0.5) {
		t.Errorf("colour midpoint %v", col)
	}
}

func TestClipLoopModes(t *testing.T) {
	c := NewClip("c", Once, Position(Vec3s(At(0, lin.V3(0, 0, 0)), At(2, lin.V3(4, 0, 0)))))
	if lt, done := c.local(3); lt != 2 || !done {
		t.Fatal("Once should clamp and finish")
	}
	c.Mode = Loop
	if lt, done := c.local(5); !near(lt, 1) || done {
		t.Fatalf("Loop local %v done %v", lt, done)
	}
	c.Mode = PingPong
	if lt, _ := c.local(3); !near(lt, 1) {
		t.Fatalf("PingPong local %v", lt)
	}
	if lt, _ := c.local(1.5); !near(lt, 1.5) {
		t.Fatalf("PingPong forward half %v", lt)
	}
}

func TestPlayerAndSystem(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("anim", System)
	move := NewClip("move", Once, Position(Vec3s(At(0, lin.V3(0, 0, 0)), At(1, lin.V3(10, 0, 0)))))
	e := w.SpawnWith(gfx.Transform{})
	PlayerOf(w, e).Play(move)
	w.Update(0.25)
	if tr, _ := ecs.Get[gfx.Transform](w, e); !near(tr.Position.X, 2.5) {
		t.Fatalf("position after 0.25 s: %v", tr.Position)
	}
	w.Update(1) // past the end: clamps, stops, reports
	tr, _ := ecs.Get[gfx.Transform](w, e)
	p, _ := ecs.Get[Player](w, e)
	if tr.Position.X != 10 || p.Playing {
		t.Fatalf("end state %v playing %v", tr.Position, p.Playing)
	}
	if ev := ecs.Events[Finished](w); len(ev) != 1 || ev[0].Clip != move || ev[0].Entity != e {
		t.Fatalf("finished events %v", ev)
	}
	// Crossfade: halfway through a one-second fade the value is halfway
	// between the old clip's and the new clip's samples.
	hold := NewClip("hold", Loop, Position(Vec3s(At(0, lin.V3(10, 0, 0)), At(1, lin.V3(10, 0, 0)))))
	up := NewClip("up", Loop, Position(Vec3s(At(0, lin.V3(0, 20, 0)), At(1, lin.V3(0, 20, 0)))))
	PlayerOf(w, e).Play(hold)
	w.Update(0.1)
	PlayerOf(w, e).CrossFade(up, 1)
	w.Update(0.5)
	tr, _ = ecs.Get[gfx.Transform](w, e)
	if !near(tr.Position.X, 5) || !near(tr.Position.Y, 10) {
		t.Fatalf("crossfade midpoint %v", tr.Position)
	}
	w.Update(0.6)
	tr, _ = ecs.Get[gfx.Transform](w, e)
	if !near(tr.Position.X, 0) || !near(tr.Position.Y, 20) {
		t.Fatalf("crossfade end %v", tr.Position)
	}
	// Speed scales time; Stop leaves the value.
	p, _ = ecs.Get[Player](w, e)
	p.Play(move)
	p.Speed = 2
	w.Update(0.25)
	tr, _ = ecs.Get[gfx.Transform](w, e)
	if !near(tr.Position.X, 5) {
		t.Fatalf("speed 2 after 0.25 s: %v", tr.Position)
	}
	p.Stop()
	w.Update(1)
	tr, _ = ecs.Get[gfx.Transform](w, e)
	if !near(tr.Position.X, 5) {
		t.Fatal("stopped player moved")
	}
}

func TestSpriteTracksAndProperty(t *testing.T) {
	type Health struct{ HP int }
	w := ecs.NewWorld()
	w.AddSystem("anim", System)
	clip := NewClip("fx", Loop,
		Position2(Vec2s(At(0, lin.V2(0, 0)), At(2, lin.V2(100, 0)))),
		Rotation2(Floats(Num(0, 0), Num(2, 2))),
		Tint(Colors(At(0, gfx.RGBA(255, 255, 255, 255)), At(2, gfx.RGBA(255, 255, 255, 0)))),
		Property(Floats(Num(0, 100), Num(2, 0)), func(h *Health) float32 { return float32(h.HP) }, func(h *Health, v float32) { h.HP = int(v) }),
	)
	e := w.SpawnWith(gfx.Sprite{Color: gfx.White}, Health{100})
	PlayerOf(w, e).Play(clip)
	w.Update(1)
	s, _ := ecs.Get[gfx.Sprite](w, e)
	h, _ := ecs.Get[Health](w, e)
	if !near(s.Pos.X, 50) || !near(s.Rotation, 1) || !near(s.Color.A, 0.5) || h.HP != 50 {
		t.Fatalf("sprite %+v health %v", s, h)
	}
}

func TestFlipbookAndSkeleton(t *testing.T) {
	w := ecs.NewWorld()
	w.AddSystem("anim", System)
	sheet := &gfx.Sheet{Texture: &gfx.Texture{Width: 64, Height: 16}, FrameW: 16, FrameH: 16, Columns: 4, Rows: 1}
	e := w.SpawnWith(gfx.Sprite{}, Flipbook{Sheet: sheet, Frames: []int{0, 1, 2, 3}, FPS: 4})
	w.Update(0.3) // frame 1
	s, _ := ecs.Get[gfx.Sprite](w, e)
	if !near(s.UV0.X, 0.25) {
		t.Fatalf("frame 1 uv %v", s.UV0)
	}
	w.Update(1) // past the end of a non-looping book
	f, _ := ecs.Get[Flipbook](w, e)
	s, _ = ecs.Get[gfx.Sprite](w, e)
	if !f.Done || !near(s.UV0.X, 0.75) || len(ecs.Events[Finished](w)) != 1 {
		t.Fatalf("flipbook end done=%v uv=%v events=%d", f.Done, s.UV0, len(ecs.Events[Finished](w)))
	}
	f.Loop = true
	f.Restart()
	w.Update(1.1) // 4.4 frames in: frame 0 again
	s, _ = ecs.Get[gfx.Sprite](w, e)
	if !near(s.UV0.X, 0) {
		t.Fatalf("looping flipbook uv %v", s.UV0)
	}
	// A skeleton with no player is ignored rather than crashing.
	w.SpawnWith(Skeleton{})
	w.Update(0.1)
}
