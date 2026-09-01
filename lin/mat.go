package lin

import "math"

// Mat4 is a 4x4 matrix stored column-major: element (row r, column c) is
// at index c*4+r, which is the layout GLSL expects in a uniform buffer.
type Mat4 [16]float32

// Identity returns the identity matrix.
func Identity() Mat4 {
	return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

// At returns element (row, col).
func (m Mat4) At(row, col int) float32 { return m[col*4+row] }

// Mul returns m × n, applying n first.
func (m Mat4) Mul(n Mat4) Mat4 {
	var out Mat4
	for c := range 4 {
		for r := range 4 {
			out[c*4+r] = m[r]*n[c*4] + m[4+r]*n[c*4+1] + m[8+r]*n[c*4+2] + m[12+r]*n[c*4+3]
		}
	}
	return out
}

// MulVec4 transforms v.
func (m Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		m[0]*v.X + m[4]*v.Y + m[8]*v.Z + m[12]*v.W,
		m[1]*v.X + m[5]*v.Y + m[9]*v.Z + m[13]*v.W,
		m[2]*v.X + m[6]*v.Y + m[10]*v.Z + m[14]*v.W,
		m[3]*v.X + m[7]*v.Y + m[11]*v.Z + m[15]*v.W,
	}
}

// MulPoint transforms a point (w = 1) and drops w.
func (m Mat4) MulPoint(p Vec3) Vec3 { return m.MulVec4(p.Vec4(1)).Vec3() }

// Transpose swaps rows and columns.
func (m Mat4) Transpose() Mat4 {
	var out Mat4
	for c := range 4 {
		for r := range 4 {
			out[r*4+c] = m[c*4+r]
		}
	}
	return out
}

func Translate(v Vec3) Mat4 {
	m := Identity()
	m[12], m[13], m[14] = v.X, v.Y, v.Z
	return m
}

func Scale(v Vec3) Mat4 {
	m := Identity()
	m[0], m[5], m[10] = v.X, v.Y, v.Z
	return m
}

// Rotate builds a rotation of angle radians about axis.
func Rotate(angle float32, axis Vec3) Mat4 {
	a := axis.Norm()
	s, c := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	t := 1 - c
	return Mat4{
		t*a.X*a.X + c, t*a.X*a.Y + s*a.Z, t*a.X*a.Z - s*a.Y, 0,
		t*a.X*a.Y - s*a.Z, t*a.Y*a.Y + c, t*a.Y*a.Z + s*a.X, 0,
		t*a.X*a.Z + s*a.Y, t*a.Y*a.Z - s*a.X, t*a.Z*a.Z + c, 0,
		0, 0, 0, 1,
	}
}

// Ortho maps the box [left,right]×[bottom,top]×[near,far] to Vulkan clip
// space, where bottom maps to clip Y = -1, which is the top of the screen.
// Ortho2D is the usual way to get screen coordinates with +Y down.
func Ortho(left, right, bottom, top, near, far float32) Mat4 {
	var m Mat4
	m[0] = 2 / (right - left)
	m[5] = 2 / (top - bottom)
	m[10] = -1 / (far - near) // right-handed view: points in front have negative z
	m[12] = -(right + left) / (right - left)
	m[13] = -(top + bottom) / (top - bottom)
	m[14] = -near / (far - near)
	m[15] = 1
	return m
}

// Ortho2D maps pixel coordinates with the origin at the top-left and +Y
// down onto the screen, with depth from -1 (front) to 1 (back).
func Ortho2D(width, height float32) Mat4 {
	return Ortho(0, width, 0, height, -1, 1)
}

// Perspective builds a right-handed projection with depth in [0,1] and
// +Y up in view space, flipped for Vulkan's +Y-down clip space.
func Perspective(fovy, aspect, near, far float32) Mat4 {
	f := float32(1 / math.Tan(float64(fovy)/2))
	var m Mat4
	m[0] = f / aspect
	m[5] = -f
	m[10] = far / (near - far)
	m[11] = -1
	m[14] = near * far / (near - far)
	return m
}

// LookAt builds a right-handed view matrix.
func LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye).Norm()
	s := f.Cross(up).Norm()
	u := s.Cross(f)
	return Mat4{
		s.X, u.X, -f.X, 0,
		s.Y, u.Y, -f.Y, 0,
		s.Z, u.Z, -f.Z, 0,
		-s.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

// Inverse returns the inverse, or the identity for a singular matrix.
func (m Mat4) Inverse() Mat4 {
	var inv Mat4
	inv[0] = m[5]*m[10]*m[15] - m[5]*m[11]*m[14] - m[9]*m[6]*m[15] + m[9]*m[7]*m[14] + m[13]*m[6]*m[11] - m[13]*m[7]*m[10]
	inv[4] = -m[4]*m[10]*m[15] + m[4]*m[11]*m[14] + m[8]*m[6]*m[15] - m[8]*m[7]*m[14] - m[12]*m[6]*m[11] + m[12]*m[7]*m[10]
	inv[8] = m[4]*m[9]*m[15] - m[4]*m[11]*m[13] - m[8]*m[5]*m[15] + m[8]*m[7]*m[13] + m[12]*m[5]*m[11] - m[12]*m[7]*m[9]
	inv[12] = -m[4]*m[9]*m[14] + m[4]*m[10]*m[13] + m[8]*m[5]*m[14] - m[8]*m[6]*m[13] - m[12]*m[5]*m[10] + m[12]*m[6]*m[9]
	inv[1] = -m[1]*m[10]*m[15] + m[1]*m[11]*m[14] + m[9]*m[2]*m[15] - m[9]*m[3]*m[14] - m[13]*m[2]*m[11] + m[13]*m[3]*m[10]
	inv[5] = m[0]*m[10]*m[15] - m[0]*m[11]*m[14] - m[8]*m[2]*m[15] + m[8]*m[3]*m[14] + m[12]*m[2]*m[11] - m[12]*m[3]*m[10]
	inv[9] = -m[0]*m[9]*m[15] + m[0]*m[11]*m[13] + m[8]*m[1]*m[15] - m[8]*m[3]*m[13] - m[12]*m[1]*m[11] + m[12]*m[3]*m[9]
	inv[13] = m[0]*m[9]*m[14] - m[0]*m[10]*m[13] - m[8]*m[1]*m[14] + m[8]*m[2]*m[13] + m[12]*m[1]*m[10] - m[12]*m[2]*m[9]
	inv[2] = m[1]*m[6]*m[15] - m[1]*m[7]*m[14] - m[5]*m[2]*m[15] + m[5]*m[3]*m[14] + m[13]*m[2]*m[7] - m[13]*m[3]*m[6]
	inv[6] = -m[0]*m[6]*m[15] + m[0]*m[7]*m[14] + m[4]*m[2]*m[15] - m[4]*m[3]*m[14] - m[12]*m[2]*m[7] + m[12]*m[3]*m[6]
	inv[10] = m[0]*m[5]*m[15] - m[0]*m[7]*m[13] - m[4]*m[1]*m[15] + m[4]*m[3]*m[13] + m[12]*m[1]*m[7] - m[12]*m[3]*m[5]
	inv[14] = -m[0]*m[5]*m[14] + m[0]*m[6]*m[13] + m[4]*m[1]*m[14] - m[4]*m[2]*m[13] - m[12]*m[1]*m[6] + m[12]*m[2]*m[5]
	inv[3] = -m[1]*m[6]*m[11] + m[1]*m[7]*m[10] + m[5]*m[2]*m[11] - m[5]*m[3]*m[10] - m[9]*m[2]*m[7] + m[9]*m[3]*m[6]
	inv[7] = m[0]*m[6]*m[11] - m[0]*m[7]*m[10] - m[4]*m[2]*m[11] + m[4]*m[3]*m[10] + m[8]*m[2]*m[7] - m[8]*m[3]*m[6]
	inv[11] = -m[0]*m[5]*m[11] + m[0]*m[7]*m[9] + m[4]*m[1]*m[11] - m[4]*m[3]*m[9] - m[8]*m[1]*m[7] + m[8]*m[3]*m[5]
	inv[15] = m[0]*m[5]*m[10] - m[0]*m[6]*m[9] - m[4]*m[1]*m[10] + m[4]*m[2]*m[9] + m[8]*m[1]*m[6] - m[8]*m[2]*m[5]
	det := m[0]*inv[0] + m[1]*inv[4] + m[2]*inv[8] + m[3]*inv[12]
	if det == 0 {
		return Identity()
	}
	for i := range inv {
		inv[i] /= det
	}
	return inv
}
