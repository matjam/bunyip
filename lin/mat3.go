package lin

// Mat3 is a 3x3 matrix stored column-major, as Mat4 is: element (row r,
// column c) is at index c*3+r. It carries rotations and scales without
// translation, which is what normals need.
type Mat3 [9]float32

// Identity3 returns the 3x3 identity.
func Identity3() Mat3 { return Mat3{1, 0, 0, 0, 1, 0, 0, 0, 1} }

// At returns element (row, col).
func (m Mat3) At(row, col int) float32 { return m[col*3+row] }

// Mul returns m × n, applying n first.
func (m Mat3) Mul(n Mat3) Mat3 {
	var out Mat3
	for c := range 3 {
		for r := range 3 {
			out[c*3+r] = m.At(r, 0)*n.At(0, c) + m.At(r, 1)*n.At(1, c) + m.At(r, 2)*n.At(2, c)
		}
	}
	return out
}

// MulVec transforms v.
func (m Mat3) MulVec(v Vec3) Vec3 {
	return Vec3{
		m[0]*v.X + m[3]*v.Y + m[6]*v.Z,
		m[1]*v.X + m[4]*v.Y + m[7]*v.Z,
		m[2]*v.X + m[5]*v.Y + m[8]*v.Z,
	}
}

// Transpose swaps rows and columns.
func (m Mat3) Transpose() Mat3 {
	return Mat3{m[0], m[3], m[6], m[1], m[4], m[7], m[2], m[5], m[8]}
}

// Inverse returns the inverse, or the identity for a singular matrix.
func (m Mat3) Inverse() Mat3 {
	a, b, c := m[0], m[3], m[6]
	d, e, f := m[1], m[4], m[7]
	g, h, i := m[2], m[5], m[8]
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if det == 0 {
		return Identity3()
	}
	inv := 1 / det
	return Mat3{
		(e*i - f*h) * inv, (f*g - d*i) * inv, (d*h - e*g) * inv,
		(c*h - b*i) * inv, (a*i - c*g) * inv, (b*g - a*h) * inv,
		(b*f - c*e) * inv, (c*d - a*f) * inv, (a*e - b*d) * inv,
	}
}

// Mat3 returns the upper-left 3x3 of m: its rotation and scale.
func (m Mat4) Mat3() Mat3 {
	return Mat3{m[0], m[1], m[2], m[4], m[5], m[6], m[8], m[9], m[10]}
}

// NormalMatrix is the inverse transpose of the upper 3x3, which carries
// normals correctly through non-uniform scales.
func (m Mat4) NormalMatrix() Mat3 { return m.Mat3().Inverse().Transpose() }

// Translation is the position the matrix moves the origin to.
func (m Mat4) Translation() Vec3 { return Vec3{m[12], m[13], m[14]} }

// Decompose splits an affine matrix into translation, rotation and scale,
// assuming it was built as TRS with positive scales.
func (m Mat4) Decompose() (t Vec3, r Quat, s Vec3) {
	t = m.Translation()
	x := Vec3{m[0], m[1], m[2]}
	y := Vec3{m[4], m[5], m[6]}
	z := Vec3{m[8], m[9], m[10]}
	s = Vec3{x.Len(), y.Len(), z.Len()}
	if s.X == 0 || s.Y == 0 || s.Z == 0 {
		return t, QuatIdentity(), s
	}
	x, y, z = x.Mul(1/s.X), y.Mul(1/s.Y), z.Mul(1/s.Z)
	rot := Mat4{x.X, x.Y, x.Z, 0, y.X, y.Y, y.Z, 0, z.X, z.Y, z.Z, 0, 0, 0, 0, 1}
	return t, QuatFromMat4(rot).Norm(), s
}
