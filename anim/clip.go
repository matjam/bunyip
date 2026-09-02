package anim

import (
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Track applies an animated value to one component of an entity. The
// weight blends the sampled value with what the component holds, which
// is how crossfades and layered clips mix; 1 replaces outright.
type Track interface {
	Apply(w *ecs.World, e ecs.Entity, t, weight float32)
	Duration() float32
}

// property animates one field of component C through get and set.
type property[C, V any] struct {
	curve Curve[V]
	get   func(*C) V
	set   func(*C, V)
}

func (p property[C, V]) Apply(w *ecs.World, e ecs.Entity, t, weight float32) {
	c, ok := ecs.Get[C](w, e)
	if !ok {
		return
	}
	v := p.curve.Sample(t)
	if weight < 1 && p.curve.Lerp != nil {
		v = p.curve.Lerp(p.get(c), v, weight)
	}
	p.set(c, v)
}

func (p property[C, V]) Duration() float32 { return p.curve.Duration() }

// Property makes a track over any component field: get reads the field
// so crossfades can blend from it, set writes the animated value.
//
//	anim.Property(anim.Floats(anim.Num(0, 0), anim.Num(1, 100)),
//		func(h *Health) float32 { return float32(h.HP) },
//		func(h *Health, v float32) { h.HP = int(v) })
func Property[C, V any](curve Curve[V], get func(*C) V, set func(*C, V)) Track {
	return property[C, V]{curve: curve, get: get, set: set}
}

// Position animates a gfx.Transform's position.
func Position(curve Curve[lin.Vec3]) Track {
	return Property(curve, func(t *gfx.Transform) lin.Vec3 { return t.Position }, func(t *gfx.Transform, v lin.Vec3) { t.Position = v })
}

// Rotation animates a gfx.Transform's rotation.
func Rotation(curve Curve[lin.Quat]) Track {
	return Property(curve, func(t *gfx.Transform) lin.Quat { return t.Rotation }, func(t *gfx.Transform, v lin.Quat) { t.Rotation = v })
}

// Scale animates a gfx.Transform's scale.
func Scale(curve Curve[lin.Vec3]) Track {
	return Property(curve, func(t *gfx.Transform) lin.Vec3 {
		if t.Scale == (lin.Vec3{}) {
			return lin.V3(1, 1, 1)
		}
		return t.Scale
	}, func(t *gfx.Transform, v lin.Vec3) { t.Scale = v })
}

// Position2 animates a gfx.Sprite's position.
func Position2(curve Curve[lin.Vec2]) Track {
	return Property(curve, func(s *gfx.Sprite) lin.Vec2 { return s.Pos }, func(s *gfx.Sprite, v lin.Vec2) { s.Pos = v })
}

// Size2 animates a gfx.Sprite's size.
func Size2(curve Curve[lin.Vec2]) Track {
	return Property(curve, func(s *gfx.Sprite) lin.Vec2 { return s.Size }, func(s *gfx.Sprite, v lin.Vec2) { s.Size = v })
}

// Rotation2 animates a gfx.Sprite's rotation in radians.
func Rotation2(curve Curve[float32]) Track {
	return Property(curve, func(s *gfx.Sprite) float32 { return s.Rotation }, func(s *gfx.Sprite, v float32) { s.Rotation = v })
}

// Tint animates a gfx.Sprite's colour.
func Tint(curve Curve[gfx.Color]) Track {
	return Property(curve, func(s *gfx.Sprite) gfx.Color { return s.Color }, func(s *gfx.Sprite, v gfx.Color) { s.Color = v })
}

// LoopMode says what a clip does at its end.
type LoopMode uint8

const (
	Once     LoopMode = iota // stop at the last key and report Finished
	Loop                     // start over
	PingPong                 // run backwards, then forwards again
)

// Clip is a named set of tracks that play together.
type Clip struct {
	Name   string
	Tracks []Track
	Mode   LoopMode
	// Length overrides the duration, which is otherwise the longest track.
	Length float32
}

// NewClip bundles tracks into a clip.
func NewClip(name string, mode LoopMode, tracks ...Track) *Clip {
	return &Clip{Name: name, Tracks: tracks, Mode: mode}
}

// Duration is the clip's length in seconds.
func (c *Clip) Duration() float32 {
	if c.Length > 0 {
		return c.Length
	}
	var d float32
	for _, t := range c.Tracks {
		d = max(d, t.Duration())
	}
	return d
}

// local maps an unbounded playback time onto the clip according to its
// loop mode, reporting whether a Once clip has ended.
func (c *Clip) local(time float32) (t float32, done bool) {
	d := c.Duration()
	if d <= 0 {
		return 0, true
	}
	switch c.Mode {
	case Loop:
		t = time - d*float32(int(time/d))
		if t < 0 {
			t += d
		}
		return t, false
	case PingPong:
		cycle := 2 * d
		t = time - cycle*float32(int(time/cycle))
		if t < 0 {
			t += cycle
		}
		if t > d {
			t = cycle - t
		}
		return t, false
	}
	if time >= d {
		return d, true
	}
	return max(time, 0), false
}

// Apply samples every track at clip time t with the given weight.
func (c *Clip) Apply(w *ecs.World, e ecs.Entity, t, weight float32) {
	for _, tr := range c.Tracks {
		tr.Apply(w, e, t, weight)
	}
}
