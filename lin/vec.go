// Package lin provides the engine's linear algebra: vectors, matrices
// and quaternions in float32. Mat3 and Mat4 use column-major storage;
// Affine uses a row-major 2x3 layout. Projection helpers use right-handed
// coordinates and Vulkan's clip space (depth 0..1, +Y down).
//
// Values are plain structs passed by value. Every operation returns a
// new value and never modifies its receiver. Angles are radians unless a
// function explicitly converts degrees. Matrices have all-zero zero
// values; use Identity, Identity3 or Identity2 for identity transforms.
//
//	eye := target.Add(lin.V3(0, 2, 5))
//	view := lin.LookAt(eye, target, lin.V3(0, 1, 0))
package lin

import "math"

// Vec2 is a point or direction in the plane.
type Vec2 struct{ X, Y float32 }

// Vec3 is a point or direction in space.
type Vec3 struct{ X, Y, Z float32 }

// Vec4 is a homogeneous point (W 1) or direction (W 0), or a colour.
type Vec4 struct{ X, Y, Z, W float32 }

// V2 makes a Vec2.
func V2(x, y float32) Vec2 { return Vec2{x, y} }

// V3 makes a Vec3.
func V3(x, y, z float32) Vec3 { return Vec3{x, y, z} }

// V4 makes a Vec4.
func V4(x, y, z, w float32) Vec4 { return Vec4{x, y, z, w} }

// Add returns a + b.
func (a Vec2) Add(b Vec2) Vec2 { return Vec2{a.X + b.X, a.Y + b.Y} }

// Sub returns a - b.
func (a Vec2) Sub(b Vec2) Vec2 { return Vec2{a.X - b.X, a.Y - b.Y} }

// Mul scales the vector.
func (a Vec2) Mul(s float32) Vec2 { return Vec2{a.X * s, a.Y * s} }

// Dot is the dot product.
func (a Vec2) Dot(b Vec2) float32 { return a.X*b.X + a.Y*b.Y }

// Len is the length.
func (a Vec2) Len() float32 { return float32(math.Sqrt(float64(a.Dot(a)))) }

// Norm returns the unit vector in a's direction; the zero vector stays zero.
func (a Vec2) Norm() Vec2 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Mul(1 / l)
}

// Lerp interpolates from a (t 0) to b (t 1).
func (a Vec2) Lerp(b Vec2, t float32) Vec2 { return a.Add(b.Sub(a).Mul(t)) }

// Add returns a + b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a - b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Mul scales the vector.
func (a Vec3) Mul(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Neg returns -a.
func (a Vec3) Neg() Vec3 { return Vec3{-a.X, -a.Y, -a.Z} }

// Dot is the dot product.
func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross is the cross product, perpendicular to both a and b.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// Len is the length.
func (a Vec3) Len() float32 { return float32(math.Sqrt(float64(a.Dot(a)))) }

// Norm returns the unit vector in a's direction; the zero vector stays zero.
func (a Vec3) Norm() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Mul(1 / l)
}

// Lerp interpolates from a (t 0) to b (t 1).
func (a Vec3) Lerp(b Vec3, t float32) Vec3 { return a.Add(b.Sub(a).Mul(t)) }

// Vec4 extends the vector with a W component: 1 for points, 0 for directions.
func (a Vec3) Vec4(w float32) Vec4 { return Vec4{a.X, a.Y, a.Z, w} }

// Add returns a + b.
func (a Vec4) Add(b Vec4) Vec4 { return Vec4{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }

// Sub returns a - b.
func (a Vec4) Sub(b Vec4) Vec4 { return Vec4{a.X - b.X, a.Y - b.Y, a.Z - b.Z, a.W - b.W} }

// Mul scales the vector.
func (a Vec4) Mul(s float32) Vec4 { return Vec4{a.X * s, a.Y * s, a.Z * s, a.W * s} }

// Dot is the dot product.
func (a Vec4) Dot(b Vec4) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// Vec3 drops the W component.
func (a Vec4) Vec3() Vec3 { return Vec3{a.X, a.Y, a.Z} }

// Radians converts degrees.
func Radians(deg float32) float32 { return deg * math.Pi / 180 }

// Degrees converts radians, for showing an angle to a person: the
// engine's own angles are radians throughout.
func Degrees(rad float32) float32 { return rad * 180 / math.Pi }

// Clamp limits v to [lo, hi].
func Clamp(v, lo, hi float32) float32 { return min(max(v, lo), hi) }

// Neg returns -a.
func (a Vec2) Neg() Vec2 { return Vec2{-a.X, -a.Y} }

// Perp returns a turned a quarter turn anticlockwise in a y-up space
// (clockwise on a y-down screen): (-y, x).
func (a Vec2) Perp() Vec2 { return Vec2{-a.Y, a.X} }

// Angle is the direction of a in radians, measured from +X towards +Y.
func (a Vec2) Angle() float32 { return float32(math.Atan2(float64(a.Y), float64(a.X))) }

// Rotate turns a by angle radians, from +X towards +Y.
func (a Vec2) Rotate(angle float32) Vec2 {
	s, c := math.Sincos(float64(angle))
	return Vec2{a.X*float32(c) - a.Y*float32(s), a.X*float32(s) + a.Y*float32(c)}
}

// Distance is the length of b - a.
func (a Vec2) Distance(b Vec2) float32 { return b.Sub(a).Len() }

// Abs takes each component's magnitude.
func (a Vec2) Abs() Vec2 { return Vec2{abs(a.X), abs(a.Y)} }

// Min takes the smaller of each component.
func (a Vec2) Min(b Vec2) Vec2 { return Vec2{min(a.X, b.X), min(a.Y, b.Y)} }

// Max takes the larger of each component.
func (a Vec2) Max(b Vec2) Vec2 { return Vec2{max(a.X, b.X), max(a.Y, b.Y)} }

// Distance is the length of b - a.
func (a Vec3) Distance(b Vec3) float32 { return b.Sub(a).Len() }

// Abs takes each component's magnitude.
func (a Vec3) Abs() Vec3 { return Vec3{abs(a.X), abs(a.Y), abs(a.Z)} }

// Min takes the smaller of each component.
func (a Vec3) Min(b Vec3) Vec3 { return Vec3{min(a.X, b.X), min(a.Y, b.Y), min(a.Z, b.Z)} }

// Max takes the larger of each component.
func (a Vec3) Max(b Vec3) Vec3 { return Vec3{max(a.X, b.X), max(a.Y, b.Y), max(a.Z, b.Z)} }

// Project returns the part of a that lies along b; a zero b gives zero.
func (a Vec3) Project(b Vec3) Vec3 {
	d := b.Dot(b)
	if d == 0 {
		return Vec3{}
	}
	return b.Mul(a.Dot(b) / d)
}

// Reflect bounces a off a surface with unit normal n.
func (a Vec3) Reflect(n Vec3) Vec3 { return a.Sub(n.Mul(2 * a.Dot(n))) }

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
