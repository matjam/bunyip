package lin

import "math"

// Affine is a 2D affine transform: a 2×3 matrix in row-major order,
//
//	[A B C]
//	[D E F]
//
// mapping (x, y) to (A·x + B·y + C, D·x + E·y + F). The zero value is
// not a valid transform; use Identity2.
type Affine struct{ A, B, C, D, E, F float32 }

// Identity2 is the transform that leaves points where they are.
func Identity2() Affine { return Affine{A: 1, E: 1} }

// Translate2 moves by (x, y).
func Translate2(x, y float32) Affine { return Affine{A: 1, C: x, E: 1, F: y} }

// Scale2 scales by sx along x and sy along y.
func Scale2(sx, sy float32) Affine { return Affine{A: sx, E: sy} }

// Rotate2 rotates by angle radians, anticlockwise in a y-up space and
// clockwise on a y-down screen.
func Rotate2(angle float32) Affine {
	s, c := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Affine{A: c, B: -s, D: s, E: c}
}

// Shear2 skews x by kx·y and y by ky·x.
func Shear2(kx, ky float32) Affine { return Affine{A: 1, B: kx, D: ky, E: 1} }

// Mul composes transforms so that (m.Mul(n)).Apply(p) == m.Apply(n.Apply(p)):
// n is applied first.
func (m Affine) Mul(n Affine) Affine {
	return Affine{
		A: m.A*n.A + m.B*n.D, B: m.A*n.B + m.B*n.E, C: m.A*n.C + m.B*n.F + m.C,
		D: m.D*n.A + m.E*n.D, E: m.D*n.B + m.E*n.E, F: m.D*n.C + m.E*n.F + m.F,
	}
}

// Apply transforms a point.
func (m Affine) Apply(p Vec2) Vec2 {
	return Vec2{m.A*p.X + m.B*p.Y + m.C, m.D*p.X + m.E*p.Y + m.F}
}

// ApplyVec transforms a direction, ignoring translation.
func (m Affine) ApplyVec(v Vec2) Vec2 {
	return Vec2{m.A*v.X + m.B*v.Y, m.D*v.X + m.E*v.Y}
}

// Inverse returns the transform that undoes m; a singular transform
// (zero scale) returns the zero Affine.
func (m Affine) Inverse() Affine {
	det := m.A*m.E - m.B*m.D
	if det == 0 {
		return Affine{}
	}
	inv := 1 / det
	return Affine{
		A: m.E * inv, B: -m.B * inv, C: (m.B*m.F - m.C*m.E) * inv,
		D: -m.D * inv, E: m.A * inv, F: (m.C*m.D - m.A*m.F) * inv,
	}
}

// IsIdentity reports whether m leaves points unchanged.
func (m Affine) IsIdentity() bool { return m == Identity2() }

// Scale reports the transform's largest scale factor along any
// direction, the amount by which it can stretch a length.
func (m Affine) Scale() float32 {
	// The larger singular value of the 2×2 part.
	a, b, c, d := float64(m.A), float64(m.B), float64(m.D), float64(m.E)
	s1 := a*a + b*b + c*c + d*d
	s2 := math.Sqrt(math.Pow(a*a+b*b-c*c-d*d, 2) + 4*math.Pow(a*c+b*d, 2))
	return float32(math.Sqrt((s1 + s2) / 2))
}

// Mat4 lifts the transform to a 4×4 matrix acting on the x-y plane.
func (m Affine) Mat4() Mat4 {
	var out Mat4
	out[0], out[4], out[12] = m.A, m.B, m.C
	out[1], out[5], out[13] = m.D, m.E, m.F
	out[10], out[15] = 1, 1
	return out
}
