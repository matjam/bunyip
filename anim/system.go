package anim

import (
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Player is the component that plays clips on an entity. Add an empty
// one and call Play through PlayerOf, or set Clip directly.
type Player struct {
	Clip    *Clip
	Time    float32
	Speed   float32 // playback rate; zero means 1
	Playing bool

	// A crossfade keeps the previous clip running while the new one
	// blends in over fadeTotal seconds.
	prev      *Clip
	prevTime  float32
	fadeLeft  float32
	fadeTotal float32
}

// Play starts a clip from the beginning, replacing any playing one.
func (p *Player) Play(c *Clip) {
	p.Clip, p.Time, p.Playing = c, 0, c != nil
	p.prev, p.fadeLeft, p.fadeTotal = nil, 0, 0
}

// CrossFade starts a clip while blending out the current one over the
// given seconds.
func (p *Player) CrossFade(c *Clip, seconds float32) {
	if p.Clip == nil || !p.Playing || seconds <= 0 {
		p.Play(c)
		return
	}
	p.prev, p.prevTime = p.Clip, p.Time
	p.Clip, p.Time, p.Playing = c, 0, c != nil
	p.fadeLeft, p.fadeTotal = seconds, seconds
}

// Stop halts playback, leaving the entity where the clip left it.
func (p *Player) Stop() { p.Playing = false }

// Progress is how far through the clip playback is, 0 to 1.
func (p *Player) Progress() float32 {
	if p.Clip == nil || p.Clip.Duration() <= 0 {
		return 0
	}
	t, _ := p.Clip.local(p.Time)
	return t / p.Clip.Duration()
}

// PlayerOf returns the entity's Player, adding one when it has none.
func PlayerOf(w *ecs.World, e ecs.Entity) *Player {
	if p, ok := ecs.Get[Player](w, e); ok {
		return p
	}
	ecs.Add(w, e, Player{})
	p, _ := ecs.Get[Player](w, e)
	return p
}

// Finished is emitted when a Once clip or a non-looping Flipbook ends.
type Finished struct {
	Entity ecs.Entity
	Clip   *Clip // nil for a Flipbook
}

// Flipbook plays sprite-sheet frames into the entity's gfx.Sprite.
type Flipbook struct {
	Sheet  *gfx.Sheet
	Frames []int
	FPS    float32 // zero means 10
	Loop   bool
	Time   float64
	Done   bool
}

// Frame returns the sheet frame to show now.
func (f *Flipbook) Frame() int {
	if len(f.Frames) == 0 {
		return 0
	}
	fps := f.FPS
	if fps <= 0 {
		fps = 10
	}
	i := int(f.Time * float64(fps))
	if f.Loop {
		i %= len(f.Frames)
	} else {
		i = min(i, len(f.Frames)-1)
	}
	return f.Frames[i]
}

func (f *Flipbook) length() float64 {
	fps := f.FPS
	if fps <= 0 {
		fps = 10
	}
	return float64(len(f.Frames)) / float64(fps)
}

// Restart plays the flipbook from its first frame.
func (f *Flipbook) Restart() { f.Time, f.Done = 0, false }

// Skeleton plays a glTF model's animation clips through a
// gfx.AnimPlayer; draw the entity with gfx.DrawModelAnimated. Events
// the player crosses are emitted as SkeletonEvent, and with root motion
// on the player, the movement is applied to the entity's gfx.Transform
// when it has one.
type Skeleton struct {
	Player *gfx.AnimPlayer
	Speed  float64 // zero means 1
	// KeepRootMotion leaves the transform alone so the game reads
	// Player.RootMotion itself, for a physics-driven body.
	KeepRootMotion bool
	// Blend, when set, chooses and times the player's clips from its
	// parameters every update instead of Play and CrossFade: a
	// locomotion blend space driven by SetParameter. Nil plays whatever
	// the player was told to.
	Blend *Blend
}

// SetParameter sets a blend parameter, such as the speed a locomotion
// space reads; without a Blend it does nothing.
func (s *Skeleton) SetParameter(name string, v float32) {
	if s.Blend != nil {
		s.Blend.Set(name, v)
	}
}

// Parameter reads a blend parameter; 0 without a Blend or when unset.
func (s *Skeleton) Parameter(name string) float32 {
	if s.Blend == nil {
		return 0
	}
	return s.Blend.Get(name)
}

// SkeletonEvent is emitted when a Skeleton's player crosses an event
// added with AnimPlayer.AddEvent.
type SkeletonEvent struct {
	Entity ecs.Entity
	Event  gfx.AnimEvent
}

// queries are cached per world.
type queries struct {
	players   *ecs.Query1[Player]
	flipbooks *ecs.Query2[Flipbook, gfx.Sprite]
	skeletons *ecs.Query1[Skeleton]
}

// System advances every Player, Flipbook and Skeleton by dt seconds and
// writes the results into their components. Register it after the
// systems that decide what to play and before drawing:
//
//	w.AddSystem("anim", anim.System)
func System(w *ecs.World, dt float64) {
	q := ecs.Resource[queries](w)
	if q == nil {
		ecs.SetResource(w, queries{players: ecs.NewQuery1[Player](w), flipbooks: ecs.NewQuery2[Flipbook, gfx.Sprite](w), skeletons: ecs.NewQuery1[Skeleton](w)})
		q = ecs.Resource[queries](w)
	}
	step := float32(dt)
	q.players.Each(func(e ecs.Entity, p *Player) {
		if !p.Playing || p.Clip == nil {
			return
		}
		speed := p.Speed
		if speed == 0 {
			speed = 1
		}
		p.Time += step * speed
		weight := float32(1)
		if p.prev != nil {
			p.prevTime += step * speed
			p.fadeLeft -= step
			if p.fadeLeft <= 0 {
				p.prev = nil
			} else {
				weight = 1 - p.fadeLeft/p.fadeTotal
				pt, _ := p.prev.local(p.prevTime)
				p.prev.Apply(w, e, pt, 1)
			}
		}
		t, done := p.Clip.local(p.Time)
		p.Clip.Apply(w, e, t, weight)
		if done {
			p.Playing = false
			p.prev = nil
			ecs.Emit(w, Finished{Entity: e, Clip: p.Clip})
		}
	})
	q.flipbooks.Each(func(e ecs.Entity, f *Flipbook, s *gfx.Sprite) {
		if f.Sheet == nil || len(f.Frames) == 0 || f.Done {
			return
		}
		f.Time += dt
		if !f.Loop && f.Time >= f.length() {
			f.Time = f.length()
			f.Done = true
			ecs.Emit(w, Finished{Entity: e})
		}
		s.UV0, s.UV1 = f.Sheet.UV(f.Frame())
	})
	q.skeletons.Each(func(e ecs.Entity, s *Skeleton) {
		if s.Player == nil {
			return
		}
		speed := s.Speed
		if speed == 0 {
			speed = 1
		}
		if s.Blend != nil {
			s.Blend.Advance(s.Player, dt*speed)
		} else {
			s.Player.Advance(dt * speed)
		}
		for _, ev := range s.Player.Events() {
			ecs.Emit(w, SkeletonEvent{Entity: e, Event: ev})
		}
		if delta, yaw := s.Player.RootMotion(); !s.KeepRootMotion && (delta != (lin.Vec3{}) || yaw != 0) {
			if tr, ok := ecs.Get[gfx.Transform](w, e); ok {
				rot := tr.Rotation
				if rot == (lin.Quat{}) {
					rot = lin.QuatIdentity()
				}
				tr.Position = tr.Position.Add(rot.Rotate(delta))
				tr.Rotation = lin.AxisAngle(lin.V3(0, 1, 0), yaw).Mul(rot).Norm()
			}
		}
	})
}
