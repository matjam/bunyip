package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Ray is a half-line in world space.
type Ray struct {
	Origin, Dir lin.Vec3
}

// ScreenRay returns the world-space ray under a point in the current 2D
// view (the same units the mouse reports), using the queue's camera.
// Project maps a world point to the current 2D view: where a label or a
// health bar for it belongs. ok is false when the point is behind the
// camera; a point outside the view still projects, off the edges.
func (g *Graphics) Project(p lin.Vec3) (x, y float32, ok bool) {
	q := g.cur
	cam := q.camera
	if !q.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	clip := cam.ViewProj(q.viewW / q.viewH).MulVec4(p.Vec4(1))
	if clip.W <= 0 {
		return 0, 0, false
	}
	return (clip.X/clip.W*0.5 + 0.5) * q.viewW, (clip.Y/clip.W*0.5 + 0.5) * q.viewH, true
}

func (g *Graphics) ScreenRay(x, y float32) Ray {
	q := g.cur
	cam := q.camera
	if !q.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	aspect := q.viewW / q.viewH
	inv := cam.ViewProj(aspect).Inverse()
	nx := x/q.viewW*2 - 1
	ny := y/q.viewH*2 - 1 // Vulkan clip space has +Y down, like the screen
	near := inv.MulVec4(lin.V4(nx, ny, 0, 1))
	far := inv.MulVec4(lin.V4(nx, ny, 1, 1))
	n := near.Vec3().Mul(1 / near.W)
	f := far.Vec3().Mul(1 / far.W)
	return Ray{Origin: n, Dir: f.Sub(n).Norm()}
}

// Hit describes where a ray met geometry.
type Hit struct {
	Distance float32
	Point    lin.Vec3
	Normal   lin.Vec3
	Part     int // index of the model part, for models
}

// Intersect tests the ray against a mesh under a model matrix, first by
// bounding box and then triangle by triangle, returning the nearest hit.
func (m *Mesh) Intersect(model lin.Mat4, r Ray) (Hit, bool) {
	inv := model.Inverse()
	o := inv.MulPoint(r.Origin)
	// Direction transforms without translation; keep its scale so the
	// returned distance is in world units.
	d := inv.MulVec4(r.Dir.Vec4(0)).Vec3()
	if !rayBox(o, d, m.Min, m.Max) {
		return Hit{}, false
	}
	best := float32(math.MaxFloat32)
	var bestN lin.Vec3
	found := false
	for i := 0; i+2 < len(m.indices); i += 3 {
		a, b, c := m.verts[m.indices[i]].Pos, m.verts[m.indices[i+1]].Pos, m.verts[m.indices[i+2]].Pos
		if t, ok := rayTriangle(o, d, a, b, c); ok && t < best && t > 0 {
			best, found = t, true
			bestN = b.Sub(a).Cross(c.Sub(a))
		}
	}
	if !found {
		return Hit{}, false
	}
	local := o.Add(d.Mul(best))
	world := model.MulPoint(local)
	n := model.MulVec4(bestN.Vec4(0)).Vec3().Norm()
	return Hit{Distance: world.Sub(r.Origin).Len(), Point: world, Normal: n}, true
}

// Intersect tests every part of a model under a world matrix.
func (m *Model) Intersect(world lin.Mat4, r Ray) (Hit, bool) {
	var best Hit
	found := false
	for i, p := range m.Parts {
		if h, ok := p.Mesh.Intersect(world.Mul(p.World), r); ok && (!found || h.Distance < best.Distance) {
			best, found = h, true
			best.Part = i
		}
	}
	return best, found
}

// rayBox is the slab test against an axis-aligned box.
func rayBox(o, d, lo, hi lin.Vec3) bool {
	tmin, tmax := float32(-math.MaxFloat32), float32(math.MaxFloat32)
	for _, axis := range [3][3]float32{{o.X, d.X, 0}, {o.Y, d.Y, 1}, {o.Z, d.Z, 2}} {
		var l, h float32
		switch axis[2] {
		case 0:
			l, h = lo.X, hi.X
		case 1:
			l, h = lo.Y, hi.Y
		default:
			l, h = lo.Z, hi.Z
		}
		if axis[1] == 0 {
			if axis[0] < l || axis[0] > h {
				return false
			}
			continue
		}
		t1, t2 := (l-axis[0])/axis[1], (h-axis[0])/axis[1]
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		tmin, tmax = max(tmin, t1), min(tmax, t2)
		if tmin > tmax {
			return false
		}
	}
	return tmax >= 0
}

// rayTriangle is the Möller–Trumbore intersection.
func rayTriangle(o, d, a, b, c lin.Vec3) (float32, bool) {
	e1, e2 := b.Sub(a), c.Sub(a)
	p := d.Cross(e2)
	det := e1.Dot(p)
	if det > -1e-7 && det < 1e-7 {
		return 0, false
	}
	inv := 1 / det
	s := o.Sub(a)
	u := s.Dot(p) * inv
	if u < 0 || u > 1 {
		return 0, false
	}
	q := s.Cross(e1)
	v := d.Dot(q) * inv
	if v < 0 || u+v > 1 {
		return 0, false
	}
	return e2.Dot(q) * inv, true
}
