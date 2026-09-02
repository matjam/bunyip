package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Sheet cuts a texture into a grid of equal frames, numbered row-major
// from the top-left, for tilesets and sprite sheets.
type Sheet struct {
	Texture       *Texture
	FrameW        int
	FrameH        int
	Columns, Rows int
	Margin        int // pixels around the whole grid
	Spacing       int // pixels between frames
}

// NewSheet describes a grid of frameW x frameH cells over the texture.
func NewSheet(tex *Texture, frameW, frameH int) *Sheet {
	s := &Sheet{Texture: tex, FrameW: frameW, FrameH: frameH}
	s.Columns = max(tex.Width/frameW, 1)
	s.Rows = max(tex.Height/frameH, 1)
	return s
}

// Count is the number of frames.
func (s *Sheet) Count() int { return s.Columns * s.Rows }

// drawTiledSlices draws a nine-slice whose edges and centre repeat their
// source pixels; the last repeat along each axis is cut to fit.
func (g *Graphics) drawTiledSlices(tex *Texture, x, y float32, xs, ys, us, vs [4]float32, tw, th float32, tint Color) {
	for row := range 3 {
		for col := range 3 {
			dw, dh := xs[col+1]-xs[col], ys[row+1]-ys[row]
			if dw <= 0 || dh <= 0 {
				continue
			}
			sw, sh := (us[col+1]-us[col])*tw, (vs[row+1]-vs[row])*th // source size in pixels
			stepX, stepY := sw, sh
			if col != 1 {
				stepX = dw // corners and vertical edges keep their width
			}
			if row != 1 {
				stepY = dh
			}
			for py := float32(0); py < dh; py += stepY {
				ph := min(stepY, dh-py)
				for px := float32(0); px < dw; px += stepX {
					pw := min(stepX, dw-px)
					g.Draw(tex, Sprite{
						Pos: lin.V2(x+xs[col]+px, y+ys[row]+py), Size: lin.V2(pw, ph),
						UV0:   lin.V2(us[col], vs[row]),
						UV1:   lin.V2(us[col]+(us[col+1]-us[col])*pw/stepX, vs[row]+(vs[row+1]-vs[row])*ph/stepY),
						Color: tint,
					})
				}
			}
		}
	}
}

// Tile flip bits, stored above the frame index in a Tilemap cell so a
// tile can be mirrored or turned without another sheet frame. They match
// the Tiled map editor's convention.
const (
	TileFlipX    = 1 << 28
	TileFlipY    = 1 << 29
	TileFlipDiag = 1 << 30 // swap the axes: with FlipX a quarter turn clockwise
	tileFrame    = TileFlipX - 1
)

// TileFlipped combines a frame index with flip bits for Tilemap.Set.
func TileFlipped(frame int, flipX, flipY, diagonal bool) int {
	if flipX {
		frame |= TileFlipX
	}
	if flipY {
		frame |= TileFlipY
	}
	if diagonal {
		frame |= TileFlipDiag
	}
	return frame
}

// TileFrame splits a cell value into its frame and flips.
func TileFrame(cell int) (frame int, flipX, flipY, diagonal bool) {
	if cell < 0 {
		return -1, false, false, false
	}
	return cell & tileFrame, cell&TileFlipX != 0, cell&TileFlipY != 0, cell&TileFlipDiag != 0
}

// Region returns a frame as a Region, for DrawRegion and atlases.
func (s *Sheet) Region(frame int) Region {
	uv0, uv1 := s.UV(frame)
	return Region{Tex: s.Texture, UV0: uv0, UV1: uv1}
}

// UV returns the texture rectangle of a frame in 0..1.
func (s *Sheet) UV(frame int) (uv0, uv1 lin.Vec2) {
	if s.Columns == 0 {
		return lin.V2(0, 0), lin.V2(1, 1)
	}
	col, row := frame%s.Columns, frame/s.Columns
	x := s.Margin + col*(s.FrameW+s.Spacing)
	y := s.Margin + row*(s.FrameH+s.Spacing)
	w, h := float32(s.Texture.Width), float32(s.Texture.Height)
	return lin.V2(float32(x)/w, float32(y)/h), lin.V2(float32(x+s.FrameW)/w, float32(y+s.FrameH)/h)
}

// DrawFrame draws one frame of a sheet with the sprite's placement; the
// sprite's UV fields are filled in and a zero Size means the frame size.
func (g *Graphics) DrawFrame(sheet *Sheet, frame int, s Sprite) {
	s.UV0, s.UV1 = sheet.UV(frame)
	if s.Size == (lin.Vec2{}) {
		s.Size = lin.V2(float32(sheet.FrameW), float32(sheet.FrameH))
	}
	g.Draw(sheet.Texture, s)
}

// Tilemap is a grid of frame indices into a sheet; -1 is empty.
type Tilemap struct {
	Sheet         *Sheet
	Width, Height int     // in tiles
	Tiles         []int   // row-major, len Width*Height; frames with optional flip bits
	TileW, TileH  float32 // drawn size of one tile; zero means the frame size

	anims map[int]*tileAnim
}

// TileAnimation cycles a frame through others: water, torches, grass in
// the wind. Durations are seconds per frame; one value applies to all.
type TileAnimation struct {
	Frames    []int
	Durations []float32
}

type tileAnim struct {
	TileAnimation
	clock float64
	index int
}

// Animate makes every cell showing frame play the animation instead.
func (t *Tilemap) Animate(frame int, a TileAnimation) {
	if len(a.Frames) == 0 {
		return
	}
	if t.anims == nil {
		t.anims = map[int]*tileAnim{}
	}
	t.anims[frame] = &tileAnim{TileAnimation: a}
}

// Advance moves the map's animations forward by dt seconds.
func (t *Tilemap) Advance(dt float64) {
	for _, a := range t.anims {
		a.clock += dt
		for {
			d := float64(0.1)
			if len(a.Durations) > 0 {
				d = float64(a.Durations[min(a.index, len(a.Durations)-1)])
			}
			if d <= 0 || a.clock < d {
				break
			}
			a.clock -= d
			a.index = (a.index + 1) % len(a.Frames)
		}
	}
}

// current returns the frame a cell shows now, with its animation applied.
func (t *Tilemap) current(frame int) int {
	if a, ok := t.anims[frame]; ok {
		return a.Frames[a.index]
	}
	return frame
}

// NewTilemap makes an empty map of the given size.
func NewTilemap(sheet *Sheet, width, height int) *Tilemap {
	t := &Tilemap{Sheet: sheet, Width: width, Height: height, Tiles: make([]int, width*height)}
	for i := range t.Tiles {
		t.Tiles[i] = -1
	}
	return t
}

// Set places a frame at a cell; out-of-range cells are ignored.
func (t *Tilemap) Set(x, y, frame int) {
	if x >= 0 && y >= 0 && x < t.Width && y < t.Height {
		t.Tiles[y*t.Width+x] = frame
	}
}

// Get returns the frame at a cell, or -1.
func (t *Tilemap) Get(x, y int) int {
	if x < 0 || y < 0 || x >= t.Width || y >= t.Height {
		return -1
	}
	return t.Tiles[y*t.Width+x]
}

// DrawTilemap draws the map with its top-left at (x, y), skipping tiles
// outside the active 2D camera's view.
func (g *Graphics) DrawTilemap(t *Tilemap, x, y float32, tint Color) {
	tw, th := t.TileW, t.TileH
	if tw == 0 {
		tw = float32(t.Sheet.FrameW)
	}
	if th == 0 {
		th = float32(t.Sheet.FrameH)
	}
	x0, y0, x1, y1 := 0, 0, t.Width, t.Height
	if cam, ok := g.Camera2D(); ok {
		vis := cam.VisibleRect(g.cur.viewW, g.cur.viewH)
		x0 = max(0, int((vis.X-x)/tw)-1)
		y0 = max(0, int((vis.Y-y)/th)-1)
		x1 = min(t.Width, int((vis.X+vis.W-x)/tw)+2)
		y1 = min(t.Height, int((vis.Y+vis.H-y)/th)+2)
	}
	for ty := y0; ty < y1; ty++ {
		for tx := x0; tx < x1; tx++ {
			frame, flipX, flipY, diag := TileFrame(t.Tiles[ty*t.Width+tx])
			if frame < 0 {
				continue
			}
			s := Sprite{Pos: lin.V2(x+float32(tx)*tw, y+float32(ty)*th), Size: lin.V2(tw, th), Color: tint, FlipX: flipX, FlipY: flipY}
			if diag {
				// A diagonal flip is a quarter turn clockwise with the
				// mirrors moved into texture space.
				s.Rotation, s.Origin = math.Pi/2, lin.V2(0.5, 0.5)
				s.FlipX, s.FlipY = flipY, !flipX
			}
			g.DrawFrame(t.Sheet, t.current(frame), s)
		}
	}
}

// DrawNineSlice draws a nine-slice stretched over r while keeping its
// corners at their pixel size; a zero tint means white.
func (g *Graphics) DrawNineSlice(ns NineSlice, r lin.Rect, tint Color) {
	tex := ns.Tex
	if tex == nil {
		tex = g.white
	}
	if tint == (Color{}) {
		tint = White
	}
	x, y, w, h := r.X, r.Y, r.W, r.H
	tw, th := float32(tex.Width), float32(tex.Height)
	xs := [4]float32{0, ns.Left, w - ns.Right, w}
	ys := [4]float32{0, ns.Top, h - ns.Bottom, h}
	us := [4]float32{0, ns.Left / tw, 1 - ns.Right/tw, 1}
	vs := [4]float32{0, ns.Top / th, 1 - ns.Bottom/th, 1}
	if ns.Tile {
		g.drawTiledSlices(tex, x, y, xs, ys, us, vs, tw, th, tint)
		return
	}
	for row := range 3 {
		for col := range 3 {
			sw, sh := xs[col+1]-xs[col], ys[row+1]-ys[row]
			if sw <= 0 || sh <= 0 {
				continue
			}
			g.Draw(tex, Sprite{
				Pos: lin.V2(x+xs[col], y+ys[row]), Size: lin.V2(sw, sh),
				UV0: lin.V2(us[col], vs[row]), UV1: lin.V2(us[col+1], vs[row+1]), Color: tint,
			})
		}
	}
}
