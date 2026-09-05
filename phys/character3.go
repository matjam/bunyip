package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// CharacterController3 moves an upright capsule through the world the
// way a player expects, without rigid-body dynamics: Move sweeps it
// along a velocity, slides it along whatever it hits, climbs ledges no
// taller than StepHeight, walks slopes up to MaxSlope and reports
// whether it stands on ground. The entity needs a gfx.Transform, which
// Move updates; a Collider3 on the same entity is ignored by its own
// sweeps, so other bodies can still bump into it.
type CharacterController3 struct {
	Radius     float32 // capsule radius; zero means 0.4
	HalfHeight float32 // centre to the centre of each cap; zero means 0.5
	StepHeight float32 // tallest ledge it climbs; zero means none
	MaxSlope   float32 // steepest walkable slope in degrees; zero means 45
	Skin       float32 // gap kept from surfaces; zero means 0.02
	Mask       uint32  // which collider layers block it; zero means all

	// Set by Move.
	Grounded     bool
	GroundNormal lin.Vec3

	shape Shape3 // the capsule as a Shape3, kept so a move boxes it once
}

func (c *CharacterController3) capsule() Capsule {
	r, h := c.Radius, c.HalfHeight
	if r == 0 {
		r = 0.4
	}
	if h == 0 {
		h = 0.5
	}
	return Capsule{Radius: r, HalfHeight: h}
}

// shapeOf is the capsule as a Shape3, rebuilt only when the size
// changes. Putting a capsule in an interface copies it onto the heap, and
// a move that did that for each of its half dozen sweeps would allocate
// every frame.
func (c *CharacterController3) shapeOf() Shape3 {
	cap := c.capsule()
	if s, ok := c.shape.(Capsule); !ok || s != cap {
		c.shape = cap
	}
	return c.shape
}

func (c *CharacterController3) params() (skin, cosSlope float32) {
	skin = c.Skin
	if skin == 0 {
		skin = 0.02
	}
	slope := c.MaxSlope
	if slope == 0 {
		slope = 45
	}
	return skin, float32(math.Cos(float64(lin.Radians(slope))))
}

// sweep casts the controller's capsule from pos along delta, ignoring
// the controller's own entity. It is a method rather than a closure
// because a closure handed to stepUp would go on the heap once per move.
func (c *CharacterController3) sweep(w *ecs.World, e ecs.Entity, pos, delta lin.Vec3) (Hit3, bool) {
	return shapeCast3(w, c.shapeOf(), pos, mat3FromQuat(lin.QuatIdentity()), delta, c.Mask, e)
}

// backoff shortens a sweep that hit at fraction f so a skin's gap remains.
func backoff(f, length, skin float32) float32 {
	if length <= 0 {
		return 0
	}
	return max(0, f-skin/length)
}

// Move advances the character by velocity·dt: horizontally with sliding
// and stepping, then vertically, then out of anything it still overlaps.
// dt is seconds and velocity is world units per second. It applies no
// gravity or impulses; the game supplies vertical velocity. A missing
// transform leaves the controller unchanged. Use nonnegative dt; even a
// zero timestep may resolve overlaps and update ground contact.
func (c *CharacterController3) Move(w *ecs.World, e ecs.Entity, velocity lin.Vec3, dt float32) {
	t, ok := ecs.Get[gfx.Transform](w, e)
	if !ok {
		return
	}
	skin, cosSlope := c.params()
	id := mat3FromQuat(lin.QuatIdentity())
	pos := t.Position
	c.Grounded = false
	// Horizontal.
	horiz := lin.V3(velocity.X, 0, velocity.Z).Mul(dt)
	remaining := horiz
	for i := 0; i < 4 && remaining.Len() > 1e-5; i++ {
		hit, ok := c.sweep(w, e, pos, remaining)
		if !ok {
			pos = pos.Add(remaining)
			break
		}
		pos = pos.Add(remaining.Mul(backoff(hit.Distance, remaining.Len(), skin)))
		rest := remaining.Mul(1 - hit.Distance)
		n := hit.Normal
		if n.Y >= cosSlope {
			// Walkable: slide up the slope.
			remaining = rest.Sub(n.Mul(rest.Dot(n)))
			continue
		}
		if c.StepHeight > 0 {
			if np, ok := c.stepUp(w, e, pos, rest, skin, cosSlope); ok {
				pos = np
				break
			}
		}
		// A wall or steep slope: slide along it without climbing.
		nh := lin.V3(n.X, 0, n.Z)
		if nh.Len() < 1e-6 {
			nh = n
		} else {
			nh = nh.Norm()
		}
		remaining = rest.Sub(nh.Mul(rest.Dot(nh)))
		if remaining.Dot(horiz) <= 0 {
			break
		}
	}
	// Vertical, with a short probe below to find the ground.
	vert := velocity.Y * dt
	if vert <= 0 {
		down := -vert + 2*skin
		if hit, ok := c.sweep(w, e, pos, lin.V3(0, -down, 0)); ok {
			dist := hit.Distance * down
			move := lin.Clamp(dist-skin, 0, -vert)
			pos.Y -= move
			if hit.Normal.Y >= cosSlope {
				c.Grounded, c.GroundNormal = true, hit.Normal
			} else if gn, ok := c.groundBelow(w, e, pos, skin, cosSlope); ok {
				// Resting a rounded end on a ledge's corner with walkable
				// ground under the centre: standing, not sliding.
				c.Grounded, c.GroundNormal = true, gn
			} else if left := -vert - move; left > 1e-5 {
				// Too steep to stand on: slide down along it.
				n := hit.Normal
				rem := lin.V3(0, -left, 0)
				rem = rem.Sub(n.Mul(rem.Dot(n)))
				if h2, ok := c.sweep(w, e, pos, rem); ok {
					pos = pos.Add(rem.Mul(backoff(h2.Distance, rem.Len(), skin)))
				} else {
					pos = pos.Add(rem)
				}
			}
		} else {
			pos.Y += vert
		}
	} else {
		up := lin.V3(0, vert, 0)
		if hit, ok := c.sweep(w, e, pos, up); ok {
			pos = pos.Add(up.Mul(backoff(hit.Distance, vert, skin)))
		} else {
			pos = pos.Add(up)
		}
	}
	// Push out of anything still overlapping.
	st := stateOf3(w)
	for range 2 {
		st.hits = overlapShape3(st.hits[:0], w, c.shapeOf(), pos, id, c.Mask, false, e)
		for _, h := range st.hits {
			if h.Distance > 0 {
				pos = pos.Add(h.Normal.Mul(h.Distance))
			}
		}
	}
	t.Position = pos
}

// groundBelow looks straight down from the centre for walkable ground
// within reach of the capsule's foot.
func (c *CharacterController3) groundBelow(w *ecs.World, e ecs.Entity, pos lin.Vec3, skin, cosSlope float32) (lin.Vec3, bool) {
	cap := c.capsule()
	reach := cap.HalfHeight + cap.Radius
	length := reach + max(c.StepHeight, 0) + skin
	hit, ok := raycast3(w, Ray3{Origin: pos, Dir: lin.V3(0, -length, 0)}, c.Mask, e)
	if !ok || hit.Normal.Y < cosSlope {
		return lin.Vec3{}, false
	}
	return hit.Normal, true
}

// stepUp tries to climb an obstacle: when a ray just ahead finds
// walkable ground no higher than a step, move up by StepHeight, forward,
// then back down, keeping the result if it ended up higher.
func (c *CharacterController3) stepUp(w *ecs.World, e ecs.Entity, pos, forward lin.Vec3, skin, cosSlope float32) (lin.Vec3, bool) {
	length := forward.Len()
	if length < 1e-6 {
		return pos, false
	}
	cap := c.capsule()
	foot := pos.Y - cap.HalfHeight - cap.Radius
	origin := pos.Add(forward.Mul((cap.Radius + 2*skin) / length))
	origin.Y = foot + c.StepHeight + skin
	probe, ok := raycast3(w, Ray3{Origin: origin, Dir: lin.V3(0, -(c.StepHeight + skin), 0)}, c.Mask, e)
	if !ok || probe.Normal.Y < cosSlope || probe.Point.Y <= foot+1e-3 {
		return pos, false
	}
	// A few skins of forward travel at least, or the rounded foot never
	// reaches past the ledge's corner and the climb stalls.
	if length < 4*skin {
		forward = forward.Mul(4 * skin / length)
		length = 4 * skin
	}
	p := pos
	up := lin.V3(0, c.StepHeight, 0)
	if hit, ok := c.sweep(w, e, p, up); ok {
		p = p.Add(up.Mul(backoff(hit.Distance, c.StepHeight, skin)))
	} else {
		p = p.Add(up)
	}
	climbed := p.Y - pos.Y
	if climbed <= 1e-4 {
		return pos, false
	}
	if hit, ok := c.sweep(w, e, p, forward); ok {
		if hit.Distance*length < min(2*skin, length/2) {
			return pos, false // still blocked: taller than a step
		}
		p = p.Add(forward.Mul(backoff(hit.Distance, length, skin)))
	} else {
		p = p.Add(forward)
	}
	down := lin.V3(0, -climbed, 0)
	hit, ok := c.sweep(w, e, p, down)
	if !ok {
		return pos, false
	}
	p = p.Add(down.Mul(backoff(hit.Distance, climbed, skin)))
	if p.Y <= pos.Y+1e-3 {
		return pos, false
	}
	return p, true
}
