package anim

import (
	"reflect"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Track applies an animated value to one component of an entity. The
// weight blends the sampled value with what the component holds, which
// is how crossfades and layered clips mix; 1 replaces outright.
type Track interface {
	// Apply samples t seconds into the track and blends into the component.
	Apply(w *ecs.World, e ecs.Entity, t, weight float32)
	// Duration reports the final key time in seconds.
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
	p.write(c, t, weight)
}

func (p property[C, V]) write(c *C, t, weight float32) {
	v := p.curve.Sample(t)
	if weight < 1 && p.curve.Lerp != nil {
		v = p.curve.Lerp(p.get(c), v, weight)
	}
	p.set(c, v)
}

func (p property[C, V]) Duration() float32 { return p.curve.Duration() }

// componentType, fetch and applyTo make a property a boundTrack: a clip
// looks the component up once and applies every track that writes it.
func (p property[C, V]) componentType() reflect.Type { return reflect.TypeFor[C]() }

func (p property[C, V]) fetch(w *ecs.World, e ecs.Entity) (any, bool) {
	c, ok := ecs.Get[C](w, e)
	return c, ok
}

func (p property[C, V]) applyTo(c any, t, weight float32) { p.write(c.(*C), t, weight) }

// boundTrack is a track that can hand its component pointer over so
// sibling tracks on the same component reuse it. Property makes one;
// a track written by hand is applied on its own.
type boundTrack interface {
	Track
	componentType() reflect.Type
	fetch(w *ecs.World, e ecs.Entity) (any, bool)
	applyTo(c any, t, weight float32)
}

// Property makes a track over any component field: get reads the field
// so crossfades can blend from it, set writes the animated value.
// The entity must already have C; a missing component is skipped. A
// curve without a Lerper replaces the value even during a crossfade.
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

// Clip is a named set of tracks that play together. The zero clip has
// no tracks and finishes on its first update. Mode defaults to Once.
type Clip struct {
	Name   string
	Tracks []Track
	Mode   LoopMode
	// Length, when positive, overrides the duration in seconds; otherwise
	// the longest track supplies it.
	Length float32

	// The duration and the grouping of tracks by component are worked out
	// once and kept, since playback asks for both several times a frame.
	// Both are rebuilt when the number of tracks changes; AddTrack rebuilds
	// them whatever changed.
	dur    float32
	durN   int
	durOK  bool
	groups []trackGroup
	loose  []Track
	planN  int
	planOK bool
}

// trackGroup is the tracks of a clip that write one component.
type trackGroup struct {
	typ    reflect.Type
	tracks []boundTrack
}

// NewClip bundles tracks into a clip.
func NewClip(name string, mode LoopMode, tracks ...Track) *Clip {
	return &Clip{Name: name, Tracks: tracks, Mode: mode}
}

// AddTrack appends tracks to the clip and rebuilds what it caches about
// them. Assigning to Tracks directly works too as long as the number of
// tracks changes; replacing a track in place needs this call with no
// arguments for the change to be seen.
func (c *Clip) AddTrack(tracks ...Track) {
	c.Tracks = append(c.Tracks, tracks...)
	c.durOK, c.planOK = false, false
}

// Duration is the clip's length in seconds.
func (c *Clip) Duration() float32 {
	if c.Length > 0 {
		return c.Length
	}
	if c.durOK && c.durN == len(c.Tracks) {
		return c.dur
	}
	var d float32
	for _, t := range c.Tracks {
		d = max(d, t.Duration())
	}
	c.dur, c.durN, c.durOK = d, len(c.Tracks), true
	return d
}

// plan groups the tracks by the component they write, so Apply looks
// each component up once instead of once per track.
func (c *Clip) plan() {
	if c.planOK && c.planN == len(c.Tracks) {
		return
	}
	c.groups, c.loose = c.groups[:0], c.loose[:0]
	for _, tr := range c.Tracks {
		bt, ok := tr.(boundTrack)
		if !ok {
			c.loose = append(c.loose, tr)
			continue
		}
		typ := bt.componentType()
		found := false
		for i := range c.groups {
			if c.groups[i].typ == typ {
				c.groups[i].tracks = append(c.groups[i].tracks, bt)
				found = true
				break
			}
		}
		if !found {
			c.groups = append(c.groups, trackGroup{typ: typ, tracks: []boundTrack{bt}})
		}
	}
	c.planN, c.planOK = len(c.Tracks), true
}

// local maps an unbounded playback time onto the clip according to its
// loop mode, reporting whether a Once clip has ended.
// fold brings a player's clock back into the clip's first cycle, so a
// looping clip's time does not grow until float32 steps lose precision.
func (c *Clip) fold(time float32) float32 {
	d := c.Duration()
	if d <= 0 {
		return time
	}
	switch c.Mode {
	case Loop:
		if time >= d {
			return time - d*float32(int(time/d))
		}
	case PingPong:
		if cycle := 2 * d; time >= cycle {
			return time - cycle*float32(int(time/cycle))
		}
	}
	return time
}

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

// Apply samples every track at clip time t in seconds with the given weight.
// It does not wrap t according to Mode; Player handles looping. The
// tracks are grouped by the component they write, so a clip that
// animates three fields of one component looks that component up once.
// Tracks written by hand, which cannot be grouped, are applied last.
func (c *Clip) Apply(w *ecs.World, e ecs.Entity, t, weight float32) {
	c.plan()
	for i := range c.groups {
		g := &c.groups[i]
		p, ok := g.tracks[0].fetch(w, e)
		if !ok {
			continue
		}
		for _, tr := range g.tracks {
			tr.applyTo(p, t, weight)
		}
	}
	for _, tr := range c.loose {
		tr.Apply(w, e, t, weight)
	}
}
