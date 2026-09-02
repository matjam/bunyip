package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Part3 is one shape of a Compound3, placed in the body's frame.
type Part3 struct {
	Shape    Shape3
	Offset   lin.Vec3
	Rotation lin.Quat // zero means none
}

// place returns the part's world position and rotation.
func (p Part3) place(pos lin.Vec3, rot mat3) (lin.Vec3, mat3) {
	return pos.Add(rot.mulVec(p.Offset)), rot.mul(mat3FromQuat(p.Rotation))
}

// Compound3 is several shapes moving as one body, for things a single
// convex shape cannot describe: a table, an L-shaped wall, a hammer.
// Parts may overlap each other.
type Compound3 struct{ Parts []Part3 }

func (c Compound3) bounds(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	lo := lin.V3(float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1)))
	hi := lo.Neg()
	for _, p := range c.Parts {
		if p.Shape == nil {
			continue
		}
		pp, pr := p.place(pos, rot)
		plo, phi := p.Shape.bounds(pp, pr)
		lo, hi = lo.Min(plo), hi.Max(phi)
	}
	if len(c.Parts) == 0 {
		return pos, pos
	}
	return lo, hi
}

func (c Compound3) inertia(mass float32) lin.Vec3 {
	if len(c.Parts) == 0 {
		return lin.Vec3{}
	}
	// Mass is shared evenly between parts; each contributes its own
	// inertia plus the parallel-axis term for its offset.
	m := mass / float32(len(c.Parts))
	var total lin.Vec3
	for _, p := range c.Parts {
		if p.Shape == nil {
			continue
		}
		i := p.Shape.inertia(m)
		o := p.Offset
		total = total.Add(lin.V3(i.X+m*(o.Y*o.Y+o.Z*o.Z), i.Y+m*(o.X*o.X+o.Z*o.Z), i.Z+m*(o.X*o.X+o.Y*o.Y)))
	}
	return total
}
