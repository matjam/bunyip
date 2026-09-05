package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Occlusion culling rasterises the frame's occluder meshes into a small
// depth buffer on the CPU and drops the draws that lie behind it. The
// buffer holds the nearest depth an occluder wrote at each pixel, so a
// draw is culled when its nearest point is behind an occluder surface
// over its whole footprint. Occluders must be opaque and closed enough
// that nothing shows through their triangles: a wall, a hill, a
// building's shell, never a fence whose holes are a cutout texture.

const (
	// defaultOcclusionW and defaultOcclusionH size the buffer when a game
	// sets none. It is deliberately small: the cost is per pixel, and
	// something big enough to hide a draw covers many pixels at any
	// resolution.
	defaultOcclusionW = 256
	defaultOcclusionH = 144
	// maxOcclusionSize bounds what SetOcclusionSize accepts, so a typo
	// cannot ask for a buffer the frame cannot afford.
	maxOcclusionSize = 2048
	// occSubBits is the rasteriser's subpixel precision. Clipping keeps
	// coordinates inside the buffer, so edge functions fit in an int32 at
	// this scale.
	occSubBits  = 4
	occSubScale = 1 << occSubBits
)

// MaxOccluderTriangles is the most triangles an occluder mesh may have
// before AddOccluder3D ignores it. Occluders are rasterised on the CPU,
// so a blocking volume should be a box or a few quads, not the detailed
// geometry it stands for.
const MaxOccluderTriangles = 4096

// meshOccluder is one occluder queued for this frame.
type meshOccluder struct {
	mesh  *Mesh
	model lin.Mat4
}

// AddOccluder3D marks a mesh as blocking the camera's view for this
// frame: a wall, a hill, a building's shell. The engine rasterises the
// frame's occluders into a small depth buffer on the CPU and skips the
// draws that lie entirely behind them, which the frustum test cannot do;
// FrameStats.Occluded counts them and they still cast shadows. Adding an
// occluder does not draw it, so draw the mesh as well, or add a coarse
// stand-in for geometry that is drawn in detail. Occluders are cleared
// at the end of the frame, like lights. Keep them few and low-poly:
// every triangle is rasterised on the CPU, and a mesh with more than
// MaxOccluderTriangles triangles is ignored.
func (g *Graphics) AddOccluder3D(m *Mesh, model lin.Mat4) {
	if m == nil || len(m.verts) == 0 || m.IndexCount == 0 {
		return
	}
	g.cur.meshOccluders = append(g.cur.meshOccluders, meshOccluder{mesh: m, model: model})
}

// AddOccluder3DAt is AddOccluder3D with a Transform.
func (g *Graphics) AddOccluder3DAt(m *Mesh, t Transform) { g.AddOccluder3D(m, t.Matrix()) }

// SetOcclusionSize sizes the software occlusion buffer in pixels. A
// larger buffer culls more, because a gap narrower than a pixel counts
// as covered, and costs more to rasterise into and to test against. Zero
// restores the default of 256 by 144, and sizes are clamped to 2048.
func (g *Graphics) SetOcclusionSize(width, height int) {
	if width <= 0 || height <= 0 {
		width, height = defaultOcclusionW, defaultOcclusionH
	}
	g.occ.want = [2]int{min(width, maxOcclusionSize), min(height, maxOcclusionSize)}
}

// occlusionBuffer is the CPU depth buffer occluders rasterise into. depth
// holds the nearest depth written per pixel in normalised device terms,
// 1 where nothing was written, so an untouched pixel occludes nothing.
type occlusionBuffer struct {
	w, h  int
	want  [2]int // the size SetOcclusionSize asked for; zero means the default
	depth []float32
	clip  []lin.Vec4 // scratch polygons for the near and side clip
	next  []lin.Vec4
	on    bool // this frame's buffer holds an occluder
}

// begin sizes and clears the buffer for a frame.
func (o *occlusionBuffer) begin() {
	w, h := o.want[0], o.want[1]
	if w <= 0 || h <= 0 {
		w, h = defaultOcclusionW, defaultOcclusionH
	}
	if o.w != w || o.h != h || len(o.depth) != w*h {
		o.w, o.h = w, h
		o.depth = make([]float32, w*h)
	}
	for i := range o.depth {
		o.depth[i] = 1
	}
	o.on = false
}

// clipPlanes are the clip-space half-spaces a triangle is cut against
// before it is rasterised: the near plane and the four sides. Each is
// non-negative inside.
var clipPlanes = [5]func(v lin.Vec4) float32{
	func(v lin.Vec4) float32 { return v.Z },       // near: z >= 0
	func(v lin.Vec4) float32 { return v.W + v.X }, // left
	func(v lin.Vec4) float32 { return v.W - v.X }, // right
	func(v lin.Vec4) float32 { return v.W + v.Y }, // bottom
	func(v lin.Vec4) float32 { return v.W - v.Y }, // top
}

// clipPoly cuts the polygon in o.clip against the near and side planes,
// so screen coordinates stay inside the buffer and the perspective
// divide is safe. It returns o's own scratch, valid until the next call.
func (o *occlusionBuffer) clipPoly() []lin.Vec4 {
	for _, inside := range clipPlanes {
		if len(o.clip) < 3 {
			return nil
		}
		o.next = o.next[:0]
		prev := o.clip[len(o.clip)-1]
		dp := inside(prev)
		for _, cur := range o.clip {
			dc := inside(cur)
			if (dp < 0) != (dc < 0) {
				t := dp / (dp - dc)
				o.next = append(o.next, prev.Add(cur.Sub(prev).Mul(t)))
			}
			if dc >= 0 {
				o.next = append(o.next, cur)
			}
			prev, dp = cur, dc
		}
		o.clip, o.next = o.next, o.clip
	}
	return o.clip
}

// occVertex is a clipped vertex in buffer pixels at subpixel precision,
// with its depth.
type occVertex struct {
	x, y int32
	z    float32
}

// project maps a clipped vertex to the buffer.
func (o *occlusionBuffer) project(v lin.Vec4) occVertex {
	inv := float32(1)
	if v.W > 1e-9 {
		inv = 1 / v.W
	}
	sx := (v.X*inv*0.5 + 0.5) * float32(o.w)
	sy := (v.Y*inv*0.5 + 0.5) * float32(o.h)
	return occVertex{
		x: int32(math.Round(float64(lin.Clamp(sx, 0, float32(o.w)) * occSubScale))),
		y: int32(math.Round(float64(lin.Clamp(sy, 0, float32(o.h)) * occSubScale))),
		z: lin.Clamp(v.Z*inv, 0, 1),
	}
}

// triangle rasterises one clipped triangle, keeping the nearest depth per
// pixel. Coverage uses the pixel centre, so a triangle over more than
// half a pixel claims all of it.
func (o *occlusionBuffer) triangle(v0, v1, v2 lin.Vec4) {
	a, b, c := o.project(v0), o.project(v1), o.project(v2)
	area := int64(b.x-a.x)*int64(c.y-a.y) - int64(b.y-a.y)*int64(c.x-a.x)
	if area == 0 {
		return
	}
	if area < 0 { // both faces block the view, so wind the back ones forward
		b, c = c, b
		area = -area
	}
	minX := int(min(a.x, b.x, c.x)) >> occSubBits
	maxX := int(max(a.x, b.x, c.x)) >> occSubBits
	minY := int(min(a.y, b.y, c.y)) >> occSubBits
	maxY := int(max(a.y, b.y, c.y)) >> occSubBits
	minX, minY = max(minX, 0), max(minY, 0)
	maxX, maxY = min(maxX, o.w-1), min(maxY, o.h-1)
	if minX > maxX || minY > maxY {
		return
	}
	invArea := float32(1) / float32(area)
	// Edge functions at the first pixel centre, stepped by whole pixels.
	// Each is twice the signed area of the triangle the edge makes with
	// the point, positive inside a forward-wound triangle, so the three
	// of them sum to the triangle's own area and serve as the barycentric
	// weights the depth is interpolated with.
	edge := func(p, q occVertex, px, py int32) (val, dx, dy int32) {
		ax, ay := p.y-q.y, q.x-p.x
		val = ax*(px-p.x) + ay*(py-p.y)
		return val, ax * occSubScale, ay * occSubScale
	}
	px := int32(minX)*occSubScale + occSubScale/2
	py := int32(minY)*occSubScale + occSubScale/2
	w0, w0dx, w0dy := edge(b, c, px, py)
	w1, w1dx, w1dy := edge(c, a, px, py)
	w2, w2dx, w2dy := edge(a, b, px, py)
	for y := minY; y <= maxY; y++ {
		r0, r1, r2 := w0, w1, w2
		row := y * o.w
		for x := minX; x <= maxX; x++ {
			if r0 >= 0 && r1 >= 0 && r2 >= 0 {
				z := (float32(r0)*a.z + float32(r1)*b.z + float32(r2)*c.z) * invArea
				if z < o.depth[row+x] {
					o.depth[row+x] = z
				}
			}
			r0, r1, r2 = r0+w0dx, r1+w1dx, r2+w2dx
		}
		w0, w1, w2 = w0+w0dy, w1+w1dy, w2+w2dy
	}
}

// rasterise draws one occluder mesh under a matrix that already carries
// its model transform.
func (o *occlusionBuffer) rasterise(m *Mesh, mvp lin.Mat4) {
	idx, verts := m.indices, m.verts
	if len(idx)/3 > MaxOccluderTriangles {
		return
	}
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := idx[i], idx[i+1], idx[i+2]
		if int(a) >= len(verts) || int(b) >= len(verts) || int(c) >= len(verts) {
			continue
		}
		o.clip = append(o.clip[:0],
			mvp.MulVec4(verts[a].Pos.Vec4(1)),
			mvp.MulVec4(verts[b].Pos.Vec4(1)),
			mvp.MulVec4(verts[c].Pos.Vec4(1)))
		poly := o.clipPoly()
		for k := 2; k < len(poly); k++ { // fan the clipped polygon
			o.triangle(poly[0], poly[k-1], poly[k])
		}
	}
}

// hides reports whether a world bounding sphere lies entirely behind the
// occluders. It takes the screen rectangle the sphere's box covers and
// its nearest depth, and asks every pixel of that rectangle to hold a
// nearer occluder. A box that crosses the near plane is never hidden,
// since its screen extent is then unbounded.
func (o *occlusionBuffer) hides(viewProj lin.Mat4, centre lin.Vec3, radius float32) bool {
	if !o.on {
		return false
	}
	lo, hi := centre.Sub(lin.V3(radius, radius, radius)), centre.Add(lin.V3(radius, radius, radius))
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -float32(math.MaxFloat32), -float32(math.MaxFloat32)
	near := float32(math.MaxFloat32)
	for i := range 8 {
		p := lin.V3(pick(i&1 == 0, lo.X, hi.X), pick(i&2 == 0, lo.Y, hi.Y), pick(i&4 == 0, lo.Z, hi.Z))
		v := viewProj.MulVec4(p.Vec4(1))
		if v.Z < 0 || v.W <= 1e-6 {
			return false // it crosses the near plane
		}
		inv := 1 / v.W
		sx := (v.X*inv*0.5 + 0.5) * float32(o.w)
		sy := (v.Y*inv*0.5 + 0.5) * float32(o.h)
		minX, maxX = min(minX, sx), max(maxX, sx)
		minY, maxY = min(minY, sy), max(maxY, sy)
		near = min(near, v.Z*inv)
	}
	x0, y0 := int(math.Floor(float64(minX))), int(math.Floor(float64(minY)))
	x1, y1 := int(math.Ceil(float64(maxX))), int(math.Ceil(float64(maxY)))
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, o.w-1), min(y1, o.h-1)
	if x0 > x1 || y0 > y1 {
		return false // off screen, which is the frustum test's business
	}
	for y := y0; y <= y1; y++ {
		row := y * o.w
		for x := x0; x <= x1; x++ {
			if o.depth[row+x] >= near {
				return false // this pixel shows through
			}
		}
	}
	return true
}

// rasteriseOccluders fills the occlusion buffer from the queue's
// occluders for this frame's camera, and reports whether it holds any.
func (g *Graphics) rasteriseOccluders(q *drawQueue, viewProj lin.Mat4) bool {
	if len(q.meshOccluders) == 0 {
		return false
	}
	o := &g.occ
	o.begin()
	for _, oc := range q.meshOccluders {
		o.rasterise(oc.mesh, viewProj.Mul(oc.model))
	}
	o.on = true
	return true
}
