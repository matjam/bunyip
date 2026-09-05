package gfx

import "github.com/matjam/bunyip/lin"

// Frustum is the volume a camera sees, as six planes facing inward. The
// engine culls every mesh draw against the camera's frustum on its own;
// a game uses one to skip whole chunks, regions or units before it asks
// to draw them, which saves the work of building their draws at all.
type Frustum struct {
	planes [6]lin.Vec4 // xyz normal pointing inward, w offset: inside when dot(p, xyz) + w >= 0
}

// FrustumOf extracts the frustum of a view-projection matrix.
func FrustumOf(viewProj lin.Mat4) Frustum {
	row := func(i int) lin.Vec4 {
		return lin.V4(viewProj.At(i, 0), viewProj.At(i, 1), viewProj.At(i, 2), viewProj.At(i, 3))
	}
	r0, r1, r2, r3 := row(0), row(1), row(2), row(3)
	f := Frustum{planes: [6]lin.Vec4{
		r3.Add(r0), // left
		r3.Sub(r0), // right
		r3.Add(r1), // bottom
		r3.Sub(r1), // top
		r2,         // near: clip z >= 0
		r3.Sub(r2), // far
	}}
	for i, p := range f.planes {
		if l := p.Vec3().Len(); l > 0 {
			f.planes[i] = p.Mul(1 / l)
		}
	}
	return f
}

// Frustum returns the camera's frustum for a view of the given aspect
// ratio (width over height).
func (c Camera) Frustum(aspect float32) Frustum { return FrustumOf(c.ViewProj(aspect)) }

// Frustum returns the frustum of the camera set for this frame, for the
// current view's aspect ratio.
func (g *Graphics) Frustum() Frustum {
	q := g.cur
	cam := q.camera
	if !q.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	aspect := float32(1)
	if q.viewW > 0 && q.viewH > 0 {
		aspect = q.viewW / q.viewH
	}
	return cam.Frustum(aspect)
}

// ContainsSphere reports whether any of a sphere lies inside the frustum.
func (f Frustum) ContainsSphere(centre lin.Vec3, radius float32) bool {
	return f.containsSphere(centre, radius, false)
}

// containsSphere is ContainsSphere with the option of ignoring the near
// plane, for a shadow volume whose pipelines clamp depth: a caster in
// front of the near plane still writes its depth, so it must not be
// culled.
func (f Frustum) containsSphere(centre lin.Vec3, radius float32, ignoreNear bool) bool {
	for i, p := range f.planes {
		if ignoreNear && i == nearPlane {
			continue
		}
		if p.Vec3().Dot(centre)+p.W < -radius {
			return false
		}
	}
	return true
}

// nearPlane is where FrustumOf puts the near plane in planes.
const nearPlane = 4

// ContainsPoint reports whether a point lies inside the frustum.
func (f Frustum) ContainsPoint(p lin.Vec3) bool { return f.ContainsSphere(p, 0) }

// ContainsBox reports whether any of an axis-aligned box lies inside the
// frustum. It errs on the side of visible: a box near a corner may pass
// while being just outside, which only costs a draw.
func (f Frustum) ContainsBox(min, max lin.Vec3) bool {
	for _, p := range f.planes {
		// The corner furthest along the plane's normal is the last to leave.
		n := p.Vec3()
		corner := lin.V3(pick(n.X >= 0, min.X, max.X), pick(n.Y >= 0, min.Y, max.Y), pick(n.Z >= 0, min.Z, max.Z))
		if n.Dot(corner)+p.W < 0 {
			return false
		}
	}
	return true
}

// Corners returns the frustum's eight corners for a view-projection
// matrix: the near plane's four then the far plane's, each as
// bottom-left, bottom-right, top-right, top-left.
func FrustumCorners(viewProj lin.Mat4) [8]lin.Vec3 {
	inv := viewProj.Inverse()
	var out [8]lin.Vec3
	i := 0
	for _, z := range []float32{0, 1} {
		for _, xy := range [4][2]float32{{-1, 1}, {1, 1}, {1, -1}, {-1, -1}} {
			v := inv.MulVec4(lin.V4(xy[0], xy[1], z, 1))
			out[i] = v.Vec3().Mul(1 / v.W)
			i++
		}
	}
	return out
}

// DrawWireFrustum outlines what a camera sees, for the given aspect
// ratio: another camera's view, a light's shadow box, a culling volume
// being debugged.
func (g *Graphics) DrawWireFrustum(cam Camera, aspect float32, c Color) {
	p := FrustumCorners(cam.ViewProj(aspect))
	for i := range 4 {
		j := (i + 1) % 4
		g.DrawLine3D(p[i], p[j], c)
		g.DrawLine3D(p[4+i], p[4+j], c)
		g.DrawLine3D(p[i], p[4+i], c)
	}
}
