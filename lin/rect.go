package lin

// Rect is an axis-aligned rectangle: its top-left corner and its size, in
// whatever units the caller uses (view units for drawing and interface
// layout, world units for a camera). The zero Rect is empty. Clip
// rectangles, interface widgets, cameras and nine-slices all use it.
type Rect struct{ X, Y, W, H float32 }

// R makes a Rect.
func R(x, y, w, h float32) Rect { return Rect{x, y, w, h} }

// RectAround makes a Rect of the given size centred on a point.
func RectAround(center Vec2, w, h float32) Rect {
	return Rect{center.X - w/2, center.Y - h/2, w, h}
}

// RectBetween makes the Rect spanning two corners, in any order.
func RectBetween(a, b Vec2) Rect {
	x0, x1 := min(a.X, b.X), max(a.X, b.X)
	y0, y1 := min(a.Y, b.Y), max(a.Y, b.Y)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Min is the top-left corner.
func (r Rect) Min() Vec2 { return Vec2{r.X, r.Y} }

// Max is the bottom-right corner.
func (r Rect) Max() Vec2 { return Vec2{r.X + r.W, r.Y + r.H} }

// Center is the middle of the rectangle.
func (r Rect) Center() Vec2 { return Vec2{r.X + r.W/2, r.Y + r.H/2} }

// Size is the width and height.
func (r Rect) Size() Vec2 { return Vec2{r.W, r.H} }

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether the point lies inside, the top and left edges
// included and the bottom and right excluded, so adjacent rectangles do
// not both claim their shared edge.
func (r Rect) Contains(p Vec2) bool {
	return p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}

// Intersects reports whether the rectangles overlap with positive area.
func (r Rect) Intersects(s Rect) bool {
	return r.X < s.X+s.W && s.X < r.X+r.W && r.Y < s.Y+s.H && s.Y < r.Y+r.H
}

// Intersect returns the overlap of the rectangles, or an empty Rect at
// the would-be corner when they do not overlap.
func (r Rect) Intersect(s Rect) Rect {
	x0, y0 := max(r.X, s.X), max(r.Y, s.Y)
	x1, y1 := min(r.X+r.W, s.X+s.W), min(r.Y+r.H, s.Y+s.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: x0, Y: y0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Union returns the smallest rectangle holding both. An empty rectangle
// contributes nothing, so unions can start from the zero Rect.
func (r Rect) Union(s Rect) Rect {
	if r.Empty() {
		return s
	}
	if s.Empty() {
		return r
	}
	x0, y0 := min(r.X, s.X), min(r.Y, s.Y)
	x1, y1 := max(r.X+r.W, s.X+s.W), max(r.Y+r.H, s.Y+s.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Inset shrinks the rectangle by d on every side; a negative d grows it.
func (r Rect) Inset(d float32) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

// Offset moves the rectangle by d.
func (r Rect) Offset(d Vec2) Rect { return Rect{r.X + d.X, r.Y + d.Y, r.W, r.H} }

// Scaled multiplies the corner and size by s, as a camera zoom does.
func (r Rect) Scaled(s float32) Rect { return Rect{r.X * s, r.Y * s, r.W * s, r.H * s} }

// Clamp moves p to the nearest point inside the rectangle.
func (r Rect) Clamp(p Vec2) Vec2 {
	return Vec2{Clamp(p.X, r.X, r.X+r.W), Clamp(p.Y, r.Y, r.Y+r.H)}
}
