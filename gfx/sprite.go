package gfx

import "github.com/matjam/bunyip/lin"

// Sprite is one textured quad. Size is in view units; UV0 and UV1 select
// the texture region in 0..1; Origin is the rotation pivot as a fraction
// of Size.
type Sprite struct {
	Pos      lin.Vec2
	Size     lin.Vec2
	UV0, UV1 lin.Vec2
	Color    Color
	Rotation float32
	Origin   lin.Vec2
}

// ClipRect limits drawing to a rectangle in view units; a zero rect means
// no clipping.
type ClipRect struct{ X, Y, W, H float32 }
