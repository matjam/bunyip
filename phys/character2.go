package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// CharacterController2 is the 2D CharacterController3: an upright
// capsule moved by sweeps that slide along walls, climb ledges no taller
// than StepHeight, walk slopes up to MaxSlope and report ground contact.
// The entity needs a gfx.Transform2, which Move updates; a Collider2 on
// the same entity is ignored by its own sweeps.
type CharacterController2 struct {
	Radius     float32 // capsule radius; zero means 0.4
	HalfHeight float32 // centre to the centre of each cap; zero means 0.5
	StepHeight float32 // tallest ledge it climbs; zero means none
	MaxSlope   float32 // steepest walkable slope in degrees; zero means 45
	Skin       float32 // gap kept from surfaces; zero means 0.02
	Mask       uint32  // which collider layers block it; zero means all

	// Set by Move.
	Grounded     bool
	GroundNormal lin.Vec2
}

func (c *CharacterController2) capsule() Capsule2 {
	r, h := c.Radius, c.HalfHeight
	if r == 0 {
		r = 0.4
	}
	if h == 0 {
		h = 0.5
	}
	return Capsule2{Radius: r, HalfHeight: h}
}

func (c *CharacterController2) params() (skin, cosSlope float32) {
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

// Move advances the character by velocity·dt: horizontally with sliding
// and stepping, then vertically, then out of anything it still overlaps.
func (c *CharacterController2) Move(w *ecs.World, e ecs.Entity, velocity lin.Vec2, dt float32) {
	t, ok := ecs.Get[gfx.Transform2](w, e)
	if !ok {
		return
	}
	cap := c.capsule()
	skin, cosSlope := c.params()
	sweep := func(pos, delta lin.Vec2) (Hit2, bool) {
		return shapeCast2(w, cap, pos, 0, delta, c.Mask, e)
	}
	pos := t.Position
	c.Grounded = false
	horiz := lin.V2(velocity.X, 0).Mul(dt)
	remaining := horiz
	for i := 0; i < 4 && remaining.Len() > 1e-5; i++ {
		hit, ok := sweep(pos, remaining)
		if !ok {
			pos = pos.Add(remaining)
			break
		}
		pos = pos.Add(remaining.Mul(backoff(hit.Distance, remaining.Len(), skin)))
		rest := remaining.Mul(1 - hit.Distance)
		n := hit.Normal
		if n.Y >= cosSlope {
			remaining = rest.Sub(n.Mul(rest.Dot(n)))
			continue
		}
		if c.StepHeight > 0 {
			if np, ok := c.stepUp(w, e, sweep, pos, rest, skin, cosSlope); ok {
				pos = np
				break
			}
		}
		nh := lin.V2(n.X, 0)
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
	vert := velocity.Y * dt
	if vert <= 0 {
		down := -vert + 2*skin
		if hit, ok := sweep(pos, lin.V2(0, -down)); ok {
			dist := hit.Distance * down
			move := lin.Clamp(dist-skin, 0, -vert)
			pos.Y -= move
			if hit.Normal.Y >= cosSlope {
				c.Grounded, c.GroundNormal = true, hit.Normal
			} else if gn, ok := c.groundBelow(w, e, pos, skin, cosSlope); ok {
				c.Grounded, c.GroundNormal = true, gn
			} else if left := -vert - move; left > 1e-5 {
				n := hit.Normal
				rem := lin.V2(0, -left)
				rem = rem.Sub(n.Mul(rem.Dot(n)))
				if h2, ok := sweep(pos, rem); ok {
					pos = pos.Add(rem.Mul(backoff(h2.Distance, rem.Len(), skin)))
				} else {
					pos = pos.Add(rem)
				}
			}
		} else {
			pos.Y += vert
		}
	} else {
		up := lin.V2(0, vert)
		if hit, ok := sweep(pos, up); ok {
			pos = pos.Add(up.Mul(backoff(hit.Distance, vert, skin)))
		} else {
			pos = pos.Add(up)
		}
	}
	for range 2 {
		for _, h := range overlapShape2(w, cap, pos, 0, c.Mask, false, e) {
			if h.Distance > 0 {
				pos = pos.Add(h.Normal.Mul(h.Distance))
			}
		}
	}
	t.Position = pos
}

// groundBelow looks straight down from the centre for walkable ground
// within reach of the capsule's foot.
func (c *CharacterController2) groundBelow(w *ecs.World, e ecs.Entity, pos lin.Vec2, skin, cosSlope float32) (lin.Vec2, bool) {
	cap := c.capsule()
	reach := cap.HalfHeight + cap.Radius
	length := reach + max(c.StepHeight, 0) + skin
	hit, ok := raycast2(w, Ray2{Origin: pos, Dir: lin.V2(0, -length)}, c.Mask, e)
	if !ok || hit.Normal.Y < cosSlope {
		return lin.Vec2{}, false
	}
	return hit.Normal, true
}

// stepUp tries to climb an obstacle: when a ray just ahead finds
// walkable ground no higher than a step, move up by StepHeight, forward,
// then back down, keeping the result if it ended up higher.
func (c *CharacterController2) stepUp(w *ecs.World, e ecs.Entity, sweep func(pos, delta lin.Vec2) (Hit2, bool), pos, forward lin.Vec2, skin, cosSlope float32) (lin.Vec2, bool) {
	length := forward.Len()
	if length < 1e-6 {
		return pos, false
	}
	cap := c.capsule()
	foot := pos.Y - cap.HalfHeight - cap.Radius
	origin := pos.Add(forward.Mul((cap.Radius + 2*skin) / length))
	origin.Y = foot + c.StepHeight + skin
	probe, ok := raycast2(w, Ray2{Origin: origin, Dir: lin.V2(0, -(c.StepHeight + skin))}, c.Mask, e)
	if !ok || probe.Normal.Y < cosSlope || probe.Point.Y <= foot+1e-3 {
		return pos, false
	}
	if length < 4*skin {
		forward = forward.Mul(4 * skin / length)
		length = 4 * skin
	}
	p := pos
	up := lin.V2(0, c.StepHeight)
	if hit, ok := sweep(p, up); ok {
		p = p.Add(up.Mul(backoff(hit.Distance, c.StepHeight, skin)))
	} else {
		p = p.Add(up)
	}
	climbed := p.Y - pos.Y
	if climbed <= 1e-4 {
		return pos, false
	}
	if hit, ok := sweep(p, forward); ok {
		if hit.Distance*length < min(2*skin, length/2) {
			return pos, false
		}
		p = p.Add(forward.Mul(backoff(hit.Distance, length, skin)))
	} else {
		p = p.Add(forward)
	}
	down := lin.V2(0, -climbed)
	hit, ok := sweep(p, down)
	if !ok {
		return pos, false
	}
	p = p.Add(down.Mul(backoff(hit.Distance, climbed, skin)))
	if p.Y <= pos.Y+1e-3 {
		return pos, false
	}
	return p, true
}
