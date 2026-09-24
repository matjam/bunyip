package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// shadowMaps is how many shadow maps the atlas holds: the cascades, the
// spot maps and every cube face.
const shadowMaps = pointFaceBase + maxPointShadows*6

// shadowCasters is what the shadow pass culls and draws with. The opaque
// draws' bounding spheres are packed in the shadow order, so the per-map
// loops read sixteen bytes a draw rather than whole draw records, and
// each map gets its own list of the draws it records and its own run of
// instance records, so a map's draws of one mesh are one instanced draw
// however sparsely they sit in the lit order.
type shadowCasters struct {
	// spheres holds xyz the centre and w the radius of each opaque draw,
	// in the shadow order, and ids the index of the draw in the queue. A
	// draw that is never culled has an infinite radius, which every test
	// passes.
	spheres []lin.Vec4
	ids     []int32
	near    []int32 // scratch: positions in spheres inside one light's range
	// lists holds each map's draws, as indices into the queue's draws in
	// the shadow order, and base where the map's instance records start
	// in the stream. maps names the maps the frame renders, in order.
	lists   [shadowMaps][]int32
	base    [shadowMaps]uint32
	maps    []int
	mapsArr [shadowMaps]int
}

// setSpheres packs the spheres of the given draws, in that order.
func (c *shadowCasters) setSpheres(draws []meshDraw, ids []int32) {
	n := len(ids)
	if cap(c.spheres) < n {
		c.spheres = make([]lin.Vec4, n)
	}
	c.spheres = c.spheres[:n]
	c.ids = ids
	inf := float32(math.Inf(1))
	for i, id := range ids {
		d := &draws[id]
		if d.cullable {
			c.spheres[i] = d.centre.Vec4(d.radius)
		} else {
			c.spheres[i] = lin.V4(0, 0, 0, inf)
		}
	}
}

// cullShadowMaps fills the list of every map the frame renders: the
// cascades when cascades is set, then each shadowed spot light's map and
// each shadowed point light's six faces. A cascade tests every caster
// and ignores its near plane, since the shadow pipelines clamp depth and
// a caster in front of a cascade still writes into it. A spot or point
// light first keeps the casters whose spheres reach its range sphere,
// because nothing outside that sphere lies between the light and a point
// it lights, and tests only those against its map or its six cube faces,
// near plane included.
func (q *drawQueue) cullShadowMaps(cascades bool) {
	c := &q.casters
	c.maps = c.mapsArr[:0]
	if cascades {
		for k := range shadowCascades {
			f := FrustumOf(q.cascadeMats[k])
			c.lists[k] = cullSpheres(c.lists[k][:0], &f, c.spheres, c.ids, nil, true)
			c.maps = append(c.maps, k)
		}
	}
	s := &q.shadow
	for k, li := range s.spots {
		p := &q.points[li]
		c.near = spheresInRange(c.near[:0], c.spheres, p.pos, max(p.rng, 0.5))
		f := FrustumOf(s.spotMats[k])
		at := shadowCascades + k
		c.lists[at] = cullSpheres(c.lists[at][:0], &f, c.spheres, c.ids, c.near, false)
		c.maps = append(c.maps, at)
	}
	for k, li := range s.points {
		p := &q.points[li]
		c.near = spheresInRange(c.near[:0], c.spheres, p.pos, max(p.rng, 0.5))
		for face := range 6 {
			at := pointFaceBase + k*6 + face
			f := FrustumOf(s.pointMats[k*6+face])
			c.lists[at] = cullSpheres(c.lists[at][:0], &f, c.spheres, c.ids, c.near, false)
			c.maps = append(c.maps, at)
		}
	}
}

// spheresInRange appends the positions of the spheres that reach a light's
// range sphere.
func spheresInRange(dst []int32, spheres []lin.Vec4, pos lin.Vec3, rng float32) []int32 {
	for i, s := range spheres {
		dx, dy, dz := s.X-pos.X, s.Y-pos.Y, s.Z-pos.Z
		r := (rng+s.W)*(1+1e-5) + 1e-4
		// Written so a sphere that compares with nothing, a NaN, is kept.
		if !(dx*dx+dy*dy+dz*dz > r*r) {
			dst = append(dst, int32(i))
		}
	}
	return dst
}

// cullSpheres appends ids[i] for each sphere i any of which lies inside a
// frustum: every sphere, or only those at the positions in only when it
// is not nil. ignoreNear skips the near plane, as Frustum.containsSphere
// does.
func cullSpheres(dst []int32, f *Frustum, spheres []lin.Vec4, ids, only []int32, ignoreNear bool) []int32 {
	p := f.planes
	if ignoreNear {
		// A plane that nothing can fail stands in for the near plane.
		p[nearPlane] = lin.V4(0, 0, 0, float32(math.Inf(1)))
	}
	// The same test as containsSphere, a sphere is out when it is wholly
	// behind a plane, written the same way round so a NaN stays in. The
	// margin keeps a sphere touching a plane in whatever order the
	// products round, so the lists hold at least what containsSphere
	// keeps.
	in := func(s lin.Vec4) bool {
		r := -(s.W*(1+1e-5) + 1e-4)
		return !(p[0].X*s.X+p[0].Y*s.Y+p[0].Z*s.Z+p[0].W < r ||
			p[1].X*s.X+p[1].Y*s.Y+p[1].Z*s.Z+p[1].W < r ||
			p[2].X*s.X+p[2].Y*s.Y+p[2].Z*s.Z+p[2].W < r ||
			p[3].X*s.X+p[3].Y*s.Y+p[3].Z*s.Z+p[3].W < r ||
			p[4].X*s.X+p[4].Y*s.Y+p[4].Z*s.Z+p[4].W < r ||
			p[5].X*s.X+p[5].Y*s.Y+p[5].Z*s.Z+p[5].W < r)
	}
	if only == nil {
		for i, s := range spheres {
			if in(s) {
				dst = append(dst, ids[i])
			}
		}
		return dst
	}
	for _, i := range only {
		if in(spheres[i]) {
			dst = append(dst, ids[i])
		}
	}
	return dst
}
