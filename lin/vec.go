// Package lin is the engine's small linear algebra library: vectors,
// matrices and quaternions in float32, column-major, right-handed, with
// Vulkan's clip space (depth 0..1, +Y down).
//
// Values are plain structs passed by value; every operation returns a
// new value and never modifies its receiver, so expressions read like
// arithmetic:
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

// Clamp limits v to [lo, hi].
func Clamp(v, lo, hi float32) float32 { return min(max(v, lo), hi) }
