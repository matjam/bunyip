package lin

import "math"

// FromEuler builds a rotation from yaw about +Y, pitch about +X and roll
// about +Z, in radians, applied roll first, then pitch, then yaw: the
// order a camera or a ship expects.
func FromEuler(yaw, pitch, roll float32) Quat {
	return AxisAngle(Vec3{0, 1, 0}, yaw).Mul(AxisAngle(Vec3{1, 0, 0}, pitch)).Mul(AxisAngle(Vec3{0, 0, 1}, roll))
}

// Euler returns the yaw, pitch and roll that FromEuler would take to make
// q. Pitch is in [-π/2, π/2]; at the poles yaw and roll share one angle.
func (q Quat) Euler() (yaw, pitch, roll float32) {
	m := q.Norm().Mat4()
	sp := Clamp(-m.At(1, 2), -1, 1)
	pitch = float32(math.Asin(float64(sp)))
	if abs(sp) > 0.9999 {
		// Looking straight up or down: roll folds into yaw.
		yaw = float32(math.Atan2(float64(-m.At(2, 0)), float64(m.At(0, 0))))
		return yaw, pitch, 0
	}
	yaw = float32(math.Atan2(float64(m.At(0, 2)), float64(m.At(2, 2))))
	roll = float32(math.Atan2(float64(m.At(1, 0)), float64(m.At(1, 1))))
	return yaw, pitch, roll
}

// QuatLookAt is the rotation that turns the local -Z axis, the engine's
// forward, to face along forward with up as near to up as it can be.
// Zero or parallel vectors give the identity.
func QuatLookAt(forward, up Vec3) Quat {
	f := forward.Norm()
	if f == (Vec3{}) {
		return QuatIdentity()
	}
	if up == (Vec3{}) {
		up = Vec3{0, 1, 0}
	}
	if abs(f.Dot(up.Norm())) > 0.9999 {
		up = Vec3{0, 0, 1}
		if abs(f.Z) > 0.9999 {
			up = Vec3{0, 1, 0}
		}
	}
	// LookAt is world-to-view; its rotation transposed is the pose.
	return QuatFromMat4(LookAt(Vec3{}, f, up).Transpose()).Norm()
}

// AxisAngle returns the unit axis and the angle in radians of the
// rotation, the inverse of the AxisAngle constructor. The identity gives
// the +Y axis and zero.
func (q Quat) AxisAngle() (axis Vec3, angle float32) {
	q = q.Norm()
	if q.W < 0 {
		q = Quat{-q.X, -q.Y, -q.Z, -q.W}
	}
	angle = 2 * float32(math.Acos(float64(Clamp(q.W, -1, 1))))
	s := float32(math.Sqrt(float64(max(0, 1-q.W*q.W))))
	if s < 1e-6 {
		return Vec3{0, 1, 0}, 0
	}
	return Vec3{q.X / s, q.Y / s, q.Z / s}, angle
}
