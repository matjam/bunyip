// Package lin is the engine's small linear algebra library: vectors,
// matrices and quaternions in float32, column-major, right-handed, with
// Vulkan's clip space (depth 0..1, +Y down).
package lin

import "math"

type Vec2 struct{ X, Y float32 }

type Vec3 struct{ X, Y, Z float32 }

type Vec4 struct{ X, Y, Z, W float32 }

func V2(x, y float32) Vec2       { return Vec2{x, y} }
func V3(x, y, z float32) Vec3    { return Vec3{x, y, z} }
func V4(x, y, z, w float32) Vec4 { return Vec4{x, y, z, w} }

func (a Vec2) Add(b Vec2) Vec2             { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2             { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Mul(s float32) Vec2          { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float32          { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Len() float32                { return float32(math.Sqrt(float64(a.Dot(a)))) }
func (a Vec2) Norm() Vec2                  { return a.Mul(1 / a.Len()) }
func (a Vec2) Lerp(b Vec2, t float32) Vec2 { return a.Add(b.Sub(a).Mul(t)) }

func (a Vec3) Add(b Vec3) Vec3    { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3    { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Mul(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Neg() Vec3          { return Vec3{-a.X, -a.Y, -a.Z} }
func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a Vec3) Len() float32 { return float32(math.Sqrt(float64(a.Dot(a)))) }
func (a Vec3) Norm() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Mul(1 / l)
}
func (a Vec3) Lerp(b Vec3, t float32) Vec3 { return a.Add(b.Sub(a).Mul(t)) }
func (a Vec3) Vec4(w float32) Vec4         { return Vec4{a.X, a.Y, a.Z, w} }

func (a Vec4) Add(b Vec4) Vec4    { return Vec4{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }
func (a Vec4) Mul(s float32) Vec4 { return Vec4{a.X * s, a.Y * s, a.Z * s, a.W * s} }
func (a Vec4) Dot(b Vec4) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }
func (a Vec4) Vec3() Vec3         { return Vec3{a.X, a.Y, a.Z} }

// Radians converts degrees.
func Radians(deg float32) float32 { return deg * math.Pi / 180 }

// Clamp limits v to [lo, hi].
func Clamp(v, lo, hi float32) float32 { return min(max(v, lo), hi) }
