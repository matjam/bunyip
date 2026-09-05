package gfx

import "github.com/matjam/bunyip/lin"

// Sprite is one textured quad. Size is in view units; UV0 and UV1 select
// the texture region in 0..1; Origin is the rotation pivot as a fraction
// of Size.
type Sprite struct {
	Pos      lin.Vec2 // pivot position in view units before the transform stack
	Size     lin.Vec2 // dimensions in view units; DrawRegion/DrawFrame fill a zero size
	UV0, UV1 lin.Vec2 // normalized texture bounds; zero UV1 selects the full texture
	Color    Color    // straight linear tint; zero means white
	Rotation float32  // radians clockwise on a Y-down screen
	Origin   lin.Vec2 // pivot fraction: zero is top-left, (0.5, 0.5) is center
	// FlipX and FlipY mirror the image, for a character facing the other
	// way.
	FlipX, FlipY bool
	// Filter overrides the texture's own filtering for this draw.
	Filter Filter
}

// Corners returns the four corners in texture order: top-left, top-right,
// bottom-right, bottom-left, after placement, origin and rotation. Negative
// sizes reverse the corresponding axis. The graphics transform and camera
// are not included, and texture flips do not move the corners.
func (s Sprite) Corners() [4]lin.Vec2 {
	ox, oy := s.Origin.X*s.Size.X, s.Origin.Y*s.Size.Y
	x0, y0, x1, y1 := -ox, -oy, s.Size.X-ox, s.Size.Y-oy
	if s.Rotation == 0 {
		return [4]lin.Vec2{{X: s.Pos.X + x0, Y: s.Pos.Y + y0}, {X: s.Pos.X + x1, Y: s.Pos.Y + y0}, {X: s.Pos.X + x1, Y: s.Pos.Y + y1}, {X: s.Pos.X + x0, Y: s.Pos.Y + y1}}
	}
	sn, cs := sin32(s.Rotation), cos32(s.Rotation)
	rot := func(x, y float32) lin.Vec2 { return lin.V2(s.Pos.X+x*cs-y*sn, s.Pos.Y+x*sn+y*cs) }
	return [4]lin.Vec2{rot(x0, y0), rot(x1, y0), rot(x1, y1), rot(x0, y1)}
}

// Bounds returns the axis-aligned rectangle enclosing Corners. It includes
// placement, origin and rotation, but not the graphics transform or camera.
func (s Sprite) Bounds() lin.Rect {
	p := s.Corners()
	lo, hi := p[0], p[0]
	for _, v := range p[1:] {
		lo = lin.V2(min(lo.X, v.X), min(lo.Y, v.Y))
		hi = lin.V2(max(hi.X, v.X), max(hi.Y, v.Y))
	}
	return lin.R(lo.X, lo.Y, hi.X-lo.X, hi.Y-lo.Y)
}

// Filter is how a draw samples its texture.
type Filter uint8

const (
	FilterDefault Filter = iota // the texture's own choice
	FilterNearest               // sharp pixels
	FilterLinear                // smooth
)

// NineSlice is a texture drawn stretched to any size while its corners
// keep their size and its edges stretch along one axis: panels, buttons
// and speech bubbles from one small image. The borders are in texture
// pixels.
type NineSlice struct {
	Tex                      *Texture
	Left, Top, Right, Bottom float32
	// Tile repeats the edge and centre pieces at their own size instead
	// of stretching them, for patterned borders and textured fills.
	Tile bool
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
