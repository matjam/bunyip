package tween

import "github.com/matjam/bunyip/lin"

// Of animates any value that can be blended: a position, a colour, a
// size. It wraps a Tween for the timing (delay, repeats, yo-yo, easing)
// and a Lerp function for the blend, so gfx.Color.Lerp, lin.Vec3.Lerp or
// a game's own mix all work:
//
//	fade := tween.NewOf(gfx.Transparent, gfx.White, 0.5, tween.OutQuad, gfx.Color.Lerp)
//	tint := fade.Update(dt)
type Of[V any] struct {
	*Tween
	From, To V                         // blend endpoints
	Lerp     func(a, b V, t float32) V // required blend function, called with eased progress
}

// NewOf makes a tween over any value with a blend function; a nil ease
// is linear. The blend function must be non-nil. Delay, Repeat and YoYo
// have the same behavior as on a scalar Tween.
func NewOf[V any](from, to V, seconds float32, ease Ease, lerp func(a, b V, t float32) V) *Of[V] {
	return &Of[V]{Tween: New(0, 1, seconds, ease), From: from, To: to, Lerp: lerp}
}

// NewVec2 tweens a 2D vector.
func NewVec2(from, to lin.Vec2, seconds float32, ease Ease) *Of[lin.Vec2] {
	return NewOf(from, to, seconds, ease, lin.Vec2.Lerp)
}

// NewVec3 tweens a 3D vector.
func NewVec3(from, to lin.Vec3, seconds float32, ease Ease) *Of[lin.Vec3] {
	return NewOf(from, to, seconds, ease, lin.Vec3.Lerp)
}

// OnDone replaces the completion callback and returns the same typed
// tween for chaining. It follows Tween.OnDone's callback timing.
func (o *Of[V]) OnDone(f func()) *Of[V] {
	o.Tween.OnDone(f)
	return o
}

// Update advances the tween and returns the blended value.
func (o *Of[V]) Update(dt float32) V {
	o.Tween.Update(dt)
	return o.Value()
}

// Value is the blended value at the tween's current progress.
func (o *Of[V]) Value() V {
	if o.reversed {
		return o.Lerp(o.To, o.From, o.Tween.Progress())
	}
	return o.Lerp(o.From, o.To, o.Tween.Progress())
}
