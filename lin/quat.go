package lin

import "math"

// Quat is a quaternion (X, Y, Z, W) representing a rotation when it has
// unit length. Use QuatIdentity for no rotation and Norm after repeated
// composition; the zero quaternion is not a unit quaternion.
type Quat struct{ X, Y, Z, W float32 }

// QuatIdentity is the rotation that leaves vectors unchanged.
func QuatIdentity() Quat { return Quat{0, 0, 0, 1} }

// AxisAngle builds a rotation of angle radians about axis.
func AxisAngle(axis Vec3, angle float32) Quat {
	a := axis.Norm()
	s := float32(math.Sin(float64(angle) / 2))
	return Quat{a.X * s, a.Y * s, a.Z * s, float32(math.Cos(float64(angle) / 2))}
}

// Mul composes rotations: q.Mul(p) applies p first.
func (q Quat) Mul(p Quat) Quat {
	return Quat{
		q.W*p.X + q.X*p.W + q.Y*p.Z - q.Z*p.Y,
		q.W*p.Y - q.X*p.Z + q.Y*p.W + q.Z*p.X,
		q.W*p.Z + q.X*p.Y - q.Y*p.X + q.Z*p.W,
		q.W*p.W - q.X*p.X - q.Y*p.Y - q.Z*p.Z,
	}
}

// Norm returns the unit quaternion, which rotations must be to stay
// rigid after repeated multiplication. A zero quaternion becomes identity.
func (q Quat) Norm() Quat {
	l := float32(math.Sqrt(float64(q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W)))
	if l == 0 {
		return QuatIdentity()
	}
	return Quat{q.X / l, q.Y / l, q.Z / l, q.W / l}
}

// Mat4 converts the rotation to a matrix.
func (q Quat) Mat4() Mat4 {
	x, y, z, w := q.X, q.Y, q.Z, q.W
	return Mat4{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
}

// Rotate applies the rotation to v.
func (q Quat) Rotate(v Vec3) Vec3 { return q.Mat4().MulPoint(v) }

// Slerp interpolates unit rotations along the shortest arc. Both inputs
// must be normalized; t is not clamped, so values outside 0..1 extrapolate.
func (q Quat) Slerp(p Quat, t float32) Quat {
	cos := q.X*p.X + q.Y*p.Y + q.Z*p.Z + q.W*p.W
	if cos < 0 {
		p = Quat{-p.X, -p.Y, -p.Z, -p.W}
		cos = -cos
	}
	if cos > 0.9995 {
		return Quat{q.X + (p.X-q.X)*t, q.Y + (p.Y-q.Y)*t, q.Z + (p.Z-q.Z)*t, q.W + (p.W-q.W)*t}.Norm()
	}
	theta := math.Acos(float64(cos))
	sin := math.Sin(theta)
	a := float32(math.Sin((1-float64(t))*theta) / sin)
	b := float32(math.Sin(float64(t)*theta) / sin)
	return Quat{a*q.X + b*p.X, a*q.Y + b*p.Y, a*q.Z + b*p.Z, a*q.W + b*p.W}
}

// TRS composes translation, rotation and scale into one matrix.
func TRS(t Vec3, r Quat, s Vec3) Mat4 {
	return Translate(t).Mul(r.Mat4()).Mul(Scale(s))
}

// QuatFromMat4 extracts the rotation of an orthonormal matrix.
func QuatFromMat4(m Mat4) Quat {
	m00, m11, m22 := m[0], m[5], m[10]
	trace := m00 + m11 + m22
	var q Quat
	switch {
	case trace > 0:
		s := float32(math.Sqrt(float64(trace+1))) * 2
		q = Quat{(m[6] - m[9]) / s, (m[8] - m[2]) / s, (m[1] - m[4]) / s, 0.25 * s}
	case m00 > m11 && m00 > m22:
		s := float32(math.Sqrt(float64(1+m00-m11-m22))) * 2
		q = Quat{0.25 * s, (m[4] + m[1]) / s, (m[8] + m[2]) / s, (m[6] - m[9]) / s}
	case m11 > m22:
		s := float32(math.Sqrt(float64(1+m11-m00-m22))) * 2
		q = Quat{(m[4] + m[1]) / s, 0.25 * s, (m[9] + m[6]) / s, (m[8] - m[2]) / s}
	default:
		s := float32(math.Sqrt(float64(1+m22-m00-m11))) * 2
		q = Quat{(m[8] + m[2]) / s, (m[9] + m[6]) / s, 0.25 * s, (m[1] - m[4]) / s}
	}
	return q.Norm()
}
