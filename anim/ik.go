package anim

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TwoBoneIK solves a chain of two bones so its end reaches a target: a
// leg (hip, knee, foot) planted on uneven ground, an arm (shoulder,
// elbow, hand) reaching a handle. root, mid and end are the joints'
// current positions, in any one space; target is where the end should
// be, in the same space, and pole is a point the middle joint bends
// towards (in front of a knee, behind an elbow). A target out of reach
// straightens the chain towards it.
//
// The result is two rotations in that space: turn the middle joint by
// lower about its own position first, then the root joint by upper about
// its position, and the end lands on the target. SolveTwoBoneIK does
// this on an AnimPlayer's nodes.
func TwoBoneIK(root, mid, end, target, pole lin.Vec3) (upper, lower lin.Quat) {
	upper, lower = lin.QuatIdentity(), lin.QuatIdentity()
	ab, bc := mid.Sub(root), end.Sub(mid)
	lab, lbc := ab.Len(), bc.Len()
	if lab == 0 || lbc == 0 {
		return
	}
	const eps = 1e-4
	at := target.Sub(root)
	lat := lin.Clamp(at.Len(), eps, lab+lbc-eps)
	ac := end.Sub(root)
	// The chain bends in the plane of its three joints about this axis.
	axis := ac.Cross(ab)
	if axis.Len() < 1e-6 {
		axis = ac.Cross(pole.Sub(root))
	}
	if axis.Len() < 1e-6 {
		axis = anyPerpendicular(ac)
	}
	axis = axis.Norm()
	// Interior angles now and as the law of cosines wants them.
	angRoot0 := angleBetween(ac, ab)
	angMid0 := angleBetween(root.Sub(mid), bc)
	angRoot1 := acos((lbc*lbc - lab*lab - lat*lat) / (-2 * lab * lat))
	angMid1 := acos((lat*lat - lab*lab - lbc*lbc) / (-2 * lab * lbc))
	r0 := lin.AxisAngle(axis, angRoot1-angRoot0)
	lower = lin.AxisAngle(axis, angMid1-angMid0)
	// Bent, the end sits at this offset from the root; swing it onto the target.
	bent := r0.Rotate(ab.Add(lower.Rotate(bc)))
	upper = rotationBetween(bent.Norm(), at.Norm()).Mul(r0)
	// Turn the chain about the root-to-target line so the middle joint
	// leans towards the pole.
	n := at.Norm()
	knee := upper.Rotate(ab)
	knee = knee.Sub(n.Mul(knee.Dot(n)))
	want := pole.Sub(root)
	want = want.Sub(n.Mul(want.Dot(n)))
	if knee.Len() > 1e-6 && want.Len() > 1e-6 {
		upper = lin.AxisAngle(n, signedAngle(knee.Norm(), want.Norm(), n)).Mul(upper)
	}
	return upper.Norm(), lower.Norm()
}

// SolveTwoBoneIK turns three of a player's nodes so the end node reaches
// target, a point in model space, with the middle joint bending towards
// pole. Call it from the player's PostPose, or after Advance, every
// frame the target matters.
func SolveTwoBoneIK(p *gfx.AnimPlayer, root, mid, end int, target, pole lin.Vec3) {
	upper, lower := TwoBoneIK(p.NodePosition(root), p.NodePosition(mid), p.NodePosition(end), target, pole)
	p.RotateNode(mid, lower)
	p.RotateNode(root, upper)
}

// LookAt returns the rotation that turns direction from towards
// direction to along the shortest arc, by at most limit radians; a zero
// limit means all the way. It is the maths under LookAtNode.
func LookAt(from, to lin.Vec3, limit float32) lin.Quat {
	q := rotationBetween(from.Norm(), to.Norm())
	if limit <= 0 {
		return q
	}
	angle := 2 * acos(abs(q.W))
	if angle <= limit {
		return q
	}
	return lin.QuatIdentity().Slerp(q, limit/angle)
}

// LookAtNode turns a node so that forward, the node's own axis that
// should face things (often +Z or -Z; check the model), points at
// target, a point in model space, turning by at most limit radians from
// the pose's own rotation. A head following the player, a turret
// tracking a ship. Call it from PostPose or after Advance.
func LookAtNode(p *gfx.AnimPlayer, node int, forward, target lin.Vec3, limit float32) {
	dir := target.Sub(p.NodePosition(node))
	if dir.Len() == 0 {
		return
	}
	facing := p.NodeRotation(node).Rotate(forward)
	p.RotateNode(node, LookAt(facing, dir, limit))
}

// rotationBetween is the shortest rotation taking unit vector a to b.
func rotationBetween(a, b lin.Vec3) lin.Quat {
	d := a.Dot(b)
	if d < -0.999999 {
		// Opposite directions: half a turn about any perpendicular.
		return lin.AxisAngle(anyPerpendicular(a), math.Pi)
	}
	c := a.Cross(b)
	return lin.Quat{X: c.X, Y: c.Y, Z: c.Z, W: 1 + d}.Norm()
}

// anyPerpendicular returns a unit vector at right angles to v.
func anyPerpendicular(v lin.Vec3) lin.Vec3 {
	p := v.Cross(lin.V3(0, 1, 0))
	if p.Len() < 1e-6 {
		p = v.Cross(lin.V3(1, 0, 0))
	}
	return p.Norm()
}

// angleBetween is the unsigned angle between two vectors.
func angleBetween(a, b lin.Vec3) float32 {
	return acos(a.Norm().Dot(b.Norm()))
}

// signedAngle is the angle from a to b, both unit and perpendicular to
// n, positive anticlockwise seen from n.
func signedAngle(a, b, n lin.Vec3) float32 {
	return float32(math.Atan2(float64(a.Cross(b).Dot(n)), float64(a.Dot(b))))
}

func acos(x float32) float32 {
	return float32(math.Acos(float64(lin.Clamp(x, -1, 1))))
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
