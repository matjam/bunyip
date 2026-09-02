// Package anim animates entities. A Curve interpolates keyframes of any
// value type; a Track applies a curve to one property of one component;
// a Clip bundles tracks with a loop mode; and a Player component plays
// clips on an entity, crossfading between them. The same machinery
// moves a 2D sprite's position and tint, a 3D transform's rotation and
// scale, or any field of your own component, and one System drives
// every player, sprite-sheet Flipbook and skeletal Skeleton in the
// world. For skeletons, BlendSpace1D, BlendSpace2D and BlendTree are
// data that turn parameters (a speed, a strafe direction) into clip
// weights, and a Blend plays them on a gfx.AnimPlayer with their cycles
// in step; TwoBoneIK and LookAt are the solvers behind SolveTwoBoneIK
// and LookAtNode, which plant feet and turn heads on the player's pose.
//
//	bounce := anim.NewClip("bounce", anim.Loop,
//		anim.Position2(anim.Vec2s(
//			anim.At(0, lin.V2(100, 300)),
//			anim.AtEased(0.5, lin.V2(100, 100), tween.OutQuad),
//			anim.AtEased(1, lin.V2(100, 300), tween.InQuad),
//		)),
//	)
//	e := w.SpawnWith(gfx.Sprite{...}, anim.Player{})
//	anim.PlayerOf(w, e).Play(bounce)
//	w.AddSystem("anim", anim.System)
package anim

import (
	"sort"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tween"
)

// Lerper interpolates between two values; t runs from 0 to 1.
type Lerper[V any] func(a, b V, t float32) V

// LerpFloat interpolates numbers.
func LerpFloat(a, b, t float32) float32 { return a + (b-a)*t }

// LerpVec2 interpolates 2D vectors.
func LerpVec2(a, b lin.Vec2, t float32) lin.Vec2 { return a.Lerp(b, t) }

// LerpVec3 interpolates 3D vectors.
func LerpVec3(a, b lin.Vec3, t float32) lin.Vec3 { return a.Lerp(b, t) }

// LerpColor interpolates colours channel by channel.
func LerpColor(a, b gfx.Color, t float32) gfx.Color {
	return gfx.Color{R: LerpFloat(a.R, b.R, t), G: LerpFloat(a.G, b.G, t), B: LerpFloat(a.B, b.B, t), A: LerpFloat(a.A, b.A, t)}
}

// SlerpQuat interpolates rotations along the shortest arc; a zero
// quaternion counts as no rotation.
func SlerpQuat(a, b lin.Quat, t float32) lin.Quat {
	if a == (lin.Quat{}) {
		a = lin.QuatIdentity()
	}
	if b == (lin.Quat{}) {
		b = lin.QuatIdentity()
	}
	return a.Slerp(b, t)
}

// Key is a value at a time. Ease shapes the approach to this key from
// the previous one; nil is linear.
type Key[V any] struct {
	Time  float32
	Value V
	Ease  tween.Ease
}

// At makes a linear key.
func At[V any](time float32, v V) Key[V] { return Key[V]{Time: time, Value: v} }

// AtEased makes a key reached along an easing curve.
func AtEased[V any](time float32, v V, ease tween.Ease) Key[V] {
	return Key[V]{Time: time, Value: v, Ease: ease}
}

// Num makes a linear key for a number curve; untyped constants would
// otherwise infer int.
func Num(time, v float32) Key[float32] { return Key[float32]{Time: time, Value: v} }

// NumEased makes an eased key for a number curve.
func NumEased(time, v float32, ease tween.Ease) Key[float32] {
	return Key[float32]{Time: time, Value: v, Ease: ease}
}

// Curve interpolates keys over time. Before the first key it holds the
// first value; after the last it holds the last.
type Curve[V any] struct {
	Keys []Key[V]
	Lerp Lerper[V]
}

// NewCurve makes a curve from keys, sorted by time.
func NewCurve[V any](lerp Lerper[V], keys ...Key[V]) Curve[V] {
	sorted := append([]Key[V](nil), keys...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time < sorted[j].Time })
	return Curve[V]{Keys: sorted, Lerp: lerp}
}

// Floats makes a curve of numbers.
func Floats(keys ...Key[float32]) Curve[float32] { return NewCurve(LerpFloat, keys...) }

// Vec2s makes a curve of 2D vectors.
func Vec2s(keys ...Key[lin.Vec2]) Curve[lin.Vec2] { return NewCurve(LerpVec2, keys...) }

// Vec3s makes a curve of 3D vectors.
func Vec3s(keys ...Key[lin.Vec3]) Curve[lin.Vec3] { return NewCurve(LerpVec3, keys...) }

// Quats makes a curve of rotations.
func Quats(keys ...Key[lin.Quat]) Curve[lin.Quat] { return NewCurve(SlerpQuat, keys...) }

// Colors makes a curve of colours.
func Colors(keys ...Key[gfx.Color]) Curve[gfx.Color] { return NewCurve(LerpColor, keys...) }

// Duration is the time of the last key.
func (c Curve[V]) Duration() float32 {
	if len(c.Keys) == 0 {
		return 0
	}
	return c.Keys[len(c.Keys)-1].Time
}

// Sample returns the value at time t.
func (c Curve[V]) Sample(t float32) V {
	var zero V
	n := len(c.Keys)
	if n == 0 {
		return zero
	}
	if t <= c.Keys[0].Time || n == 1 {
		return c.Keys[0].Value
	}
	if t >= c.Keys[n-1].Time {
		return c.Keys[n-1].Value
	}
	// Binary search for the segment.
	i := sort.Search(n, func(i int) bool { return c.Keys[i].Time > t }) - 1
	a, b := c.Keys[i], c.Keys[i+1]
	span := b.Time - a.Time
	f := float32(0)
	if span > 0 {
		f = (t - a.Time) / span
	}
	if b.Ease != nil {
		f = b.Ease(f)
	}
	if c.Lerp == nil {
		if f < 1 {
			return a.Value
		}
		return b.Value
	}
	return c.Lerp(a.Value, b.Value, f)
}
