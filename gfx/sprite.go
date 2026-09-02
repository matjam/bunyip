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

// NineSlice is a texture drawn stretched to any size while its corners
// keep their size and its edges stretch along one axis: panels, buttons
// and speech bubbles from one small image. The borders are in texture
// pixels.
type NineSlice struct {
	Tex                      *Texture
	Left, Top, Right, Bottom float32
}

// Region is a rectangle of a texture, the piece an atlas or a sheet frame
// refers to. DrawRegion draws it; Sheet.Region and NewRegion make them.
type Region struct {
	Tex      *Texture
	UV0, UV1 lin.Vec2
}

// NewRegion takes a rectangle of a texture in pixels.
func NewRegion(tex *Texture, r lin.Rect) Region {
	w, h := float32(tex.Width), float32(tex.Height)
	return Region{Tex: tex, UV0: lin.V2(r.X/w, r.Y/h), UV1: lin.V2((r.X+r.W)/w, (r.Y+r.H)/h)}
}

// Size is the region's size in texture pixels.
func (r Region) Size() lin.Vec2 {
	if r.Tex == nil {
		return lin.Vec2{}
	}
	return lin.V2((r.UV1.X-r.UV0.X)*float32(r.Tex.Width), (r.UV1.Y-r.UV0.Y)*float32(r.Tex.Height))
}

// DrawRegion draws a region with the sprite's placement; the sprite's UVs
// are taken from the region, and a zero Size means the region's own.
func (g *Graphics) DrawRegion(r Region, s Sprite) {
	s.UV0, s.UV1 = r.UV0, r.UV1
	if s.Size == (lin.Vec2{}) {
		s.Size = r.Size()
	}
	g.Draw(r.Tex, s)
}
