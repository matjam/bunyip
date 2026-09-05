package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// How a convex describes its core.
const (
	convexPoint   = iota // one point: a sphere's centre
	convexSegment        // a segment: a capsule's axis
	convexPoints         // a vertex set: a box, a hull, a triangle
)

// convexBuf is how many core vertices a convex holds by value. A box has
// eight and a triangle three, so every shape but a large hull is placed
// without touching the heap.
const convexBuf = 8

// convex is a placed convex volume: a core (a point, a segment or a set
// of vertices) grown by a margin. Spheres and capsules are all margin;
// boxes, hulls and triangles are all core, so their flat faces can be
// found for contact manifolds.
//
// The core sits in buf where it fits and in ext otherwise, and offset
// shifts the whole volume, which is how a sweep moves a shape without
// placing it again. A convex is copied by value, and a copy shares the
// original's ext, which is read only.
type convex struct {
	kind   uint8
	n      uint8
	buf    [convexBuf]lin.Vec3
	ext    []lin.Vec3 // core vertices when there are more than buf holds
	offset lin.Vec3
	margin float32
	center lin.Vec3
	size   float32 // bounding radius, for tolerances
}

// points are the core vertices before the offset, empty for a point or a
// segment core.
func (c *convex) points() []lin.Vec3 {
	if c.kind != convexPoints {
		return nil
	}
	if c.ext != nil {
		return c.ext
	}
	return c.buf[:c.n]
}

// sup is the furthest core point along dir.
func (c *convex) sup(dir lin.Vec3) lin.Vec3 {
	switch c.kind {
	case convexPoint:
		return c.buf[0].Add(c.offset)
	case convexSegment:
		if dir.Dot(c.buf[1].Sub(c.buf[0])) > 0 {
			return c.buf[1].Add(c.offset)
		}
		return c.buf[0].Add(c.offset)
	}
	pts := c.points()
	best, bestDot := pts[0], float32(math.Inf(-1))
	for _, p := range pts {
		if d := p.Dot(dir); d > bestDot {
			best, bestDot = p, d
		}
	}
	return best.Add(c.offset)
}

// support is the furthest point of the grown shape along dir.
func (c *convex) support(dir lin.Vec3) lin.Vec3 {
	p := c.sup(dir)
	if c.margin > 0 {
		p = p.Add(dir.Norm().Mul(c.margin))
	}
	return p
}

// endCount and end give a capsule's cap centres, which earn resting
// contacts of their own so a capsule lying on a face rests on two points.
func (c *convex) endCount() int {
	if c.kind == convexSegment {
		return 2
	}
	return 0
}

func (c *convex) end(i int) lin.Vec3 { return c.buf[i].Add(c.offset) }

func pointConvex(p lin.Vec3, margin float32) convex {
	c := convex{kind: convexPoint, n: 1, margin: margin, center: p, size: margin}
	c.buf[0] = p
	return c
}

func segmentConvex(a, b lin.Vec3, margin float32) convex {
	e := b.Sub(a)
	c := convex{kind: convexSegment, n: 2, margin: margin, center: a.Add(b).Mul(0.5), size: e.Len()/2 + margin}
	c.buf[0], c.buf[1] = a, b
	return c
}

// pointsConvex places a vertex core from at most convexBuf points, which
// it copies in. The caller's slice may be a local array, because nothing
// keeps a reference to it; points past convexBuf are dropped.
func pointsConvex(pts []lin.Vec3, center lin.Vec3) convex {
	c := convex{kind: convexPoints, center: center}
	c.n = uint8(copy(c.buf[:], pts))
	for _, p := range c.buf[:c.n] {
		c.size = max(c.size, p.Sub(center).Len())
	}
	return c
}

// hullConvex places a vertex core that is too large to copy in. The
// slice is kept by reference and must not change while the convex is in
// use.
func hullConvex(pts []lin.Vec3, center lin.Vec3) convex {
	c := convex{kind: convexPoints, center: center, ext: pts}
	for _, p := range pts {
		c.size = max(c.size, p.Sub(center).Len())
	}
	return c
}

// supporter is anything a ray can be cast at: a placed convex, or the
// difference of two of them for a sweep.
type supporter interface {
	support(dir lin.Vec3) lin.Vec3
}

// minkowski is b minus a, whose surface a sweep of a against b reaches
// at the moment the two shapes touch.
type minkowski struct{ a, b *convex }

func (m minkowski) support(dir lin.Vec3) lin.Vec3 {
	return m.b.support(dir).Sub(m.a.support(dir.Neg()))
}

// face appends the core vertices furthest along dir to dst: one for a
// vertex, two for an edge, three or more for a flat face, ordered
// anticlockwise around dir. Nothing is added when the shape has no
// vertices. dst must not be the scratch's angle or index buffer.
func (c *convex) face(sc *scratch3, dst []lin.Vec3, dir lin.Vec3) []lin.Vec3 {
	pts := c.points()
	if len(pts) == 0 {
		return dst
	}
	best := float32(math.Inf(-1))
	for _, p := range pts {
		best = max(best, p.Dot(dir))
	}
	tol := 1e-3*c.size + 1e-6
	start := len(dst)
	for _, p := range pts {
		if p.Dot(dir) >= best-tol {
			dst = append(dst, p.Add(c.offset))
		}
	}
	out := dst[start:]
	if len(out) < 3 {
		return dst
	}
	var centroid lin.Vec3
	for _, p := range out {
		centroid = centroid.Add(p)
	}
	centroid = centroid.Mul(1 / float32(len(out)))
	u := perpendicular(dir)
	v := dir.Cross(u)
	sc.angles = sc.angles[:0]
	sc.idx = sc.idx[:0]
	for i, p := range out {
		d := p.Sub(centroid)
		sc.angles = append(sc.angles, math.Atan2(float64(d.Dot(v)), float64(d.Dot(u))))
		sc.idx = append(sc.idx, i)
	}
	angles, idx := sc.angles, sc.idx
	// Stable, to match the order a stable sort of equal angles gives.
	for i := 1; i < len(idx); i++ {
		vi := idx[i]
		j := i - 1
		for j >= 0 && angles[vi] < angles[idx[j]] {
			idx[j+1] = idx[j]
			j--
		}
		idx[j+1] = vi
	}
	sc.faceSort = append(sc.faceSort[:0], out...)
	for i, j := range idx {
		out[i] = sc.faceSort[j]
	}
	return dst
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// perpendicular returns a unit vector at right angles to v.
func perpendicular(v lin.Vec3) lin.Vec3 {
	ref := lin.V3(1, 0, 0)
	if abs32(v.X) > 0.9*v.Len() {
		ref = lin.V3(0, 1, 0)
	}
	return v.Cross(ref).Norm()
}

// gjkVert is one vertex of the GJK simplex: a point of the Minkowski
// difference and the two core points it came from.
type gjkVert struct{ w, a, b lin.Vec3 }

// closestSimplex reduces the simplex to the part nearest the origin and
// returns that point with its barycentric weights. inside reports that
// the origin lies within a full tetrahedron.
func closestSimplex(s *[4]gjkVert, n *int) (v lin.Vec3, lam [4]float32, inside bool) {
	switch *n {
	case 1:
		return s[0].w, [4]float32{1}, false
	case 2:
		l0, l1 := closestSegment(s[0].w, s[1].w)
		lam = [4]float32{l0, l1}
	case 3:
		l0, l1, l2 := closestTriangle(s[0].w, s[1].w, s[2].w)
		lam = [4]float32{l0, l1, l2}
	default:
		var ok bool
		lam, ok = closestTetra(s)
		if !ok {
			return lin.Vec3{}, [4]float32{0.25, 0.25, 0.25, 0.25}, true
		}
	}
	// Keep the vertices that contribute and recompute the point.
	k := 0
	var out [4]float32
	for i := 0; i < *n; i++ {
		if lam[i] > 0 {
			s[k] = s[i]
			out[k] = lam[i]
			k++
		}
	}
	if k == 0 {
		k = 1
		out[0] = 1
	}
	*n = k
	for i := range k {
		v = v.Add(s[i].w.Mul(out[i]))
	}
	return v, out, false
}

// closestSegment gives the weights of the point on segment ab nearest
// the origin.
func closestSegment(a, b lin.Vec3) (float32, float32) {
	e := b.Sub(a)
	ee := e.Dot(e)
	if ee <= 1e-12 {
		return 1, 0
	}
	t := -a.Dot(e) / ee
	if t <= 0 {
		return 1, 0
	}
	if t >= 1 {
		return 0, 1
	}
	return 1 - t, t
}

// closestTriangle gives the weights of the point on triangle abc nearest
// the origin (Ericson, Real-Time Collision Detection 5.1.5).
func closestTriangle(a, b, c lin.Vec3) (float32, float32, float32) {
	ab, ac := b.Sub(a), c.Sub(a)
	ap := a.Neg()
	d1, d2 := ab.Dot(ap), ac.Dot(ap)
	if d1 <= 0 && d2 <= 0 {
		return 1, 0, 0
	}
	bp := b.Neg()
	d3, d4 := ab.Dot(bp), ac.Dot(bp)
	if d3 >= 0 && d4 <= d3 {
		return 0, 1, 0
	}
	vc := d1*d4 - d3*d2
	if vc <= 0 && d1 >= 0 && d3 <= 0 {
		t := d1 / (d1 - d3)
		return 1 - t, t, 0
	}
	cp := c.Neg()
	d5, d6 := ab.Dot(cp), ac.Dot(cp)
	if d6 >= 0 && d5 <= d6 {
		return 0, 0, 1
	}
	vb := d5*d2 - d1*d6
	if vb <= 0 && d2 >= 0 && d6 <= 0 {
		t := d2 / (d2 - d6)
		return 1 - t, 0, t
	}
	va := d3*d6 - d5*d4
	if va <= 0 && d4-d3 >= 0 && d5-d6 >= 0 {
		t := (d4 - d3) / ((d4 - d3) + (d5 - d6))
		return 0, 1 - t, t
	}
	denom := va + vb + vc
	if denom == 0 {
		return 1, 0, 0
	}
	v, w := vb/denom, vc/denom
	return 1 - v - w, v, w
}

// closestTetra gives the weights of the point on the tetrahedron's
// surface nearest the origin, or ok false when the origin is inside.
func closestTetra(s *[4]gjkVert) ([4]float32, bool) {
	faces := [4][4]int{{0, 1, 2, 3}, {0, 3, 1, 2}, {0, 2, 3, 1}, {1, 3, 2, 0}}
	var best [4]float32
	bestDist := float32(math.Inf(1))
	outside := false
	for _, f := range faces {
		a, b, c, d := s[f[0]].w, s[f[1]].w, s[f[2]].w, s[f[3]].w
		n := b.Sub(a).Cross(c.Sub(a))
		sideO := n.Dot(a.Neg())
		sideD := n.Dot(d.Sub(a))
		// A flat tetrahedron cannot contain the origin; every face counts.
		if math.Abs(float64(sideD)) > 1e-9 && sideO*sideD >= 0 {
			continue
		}
		outside = true
		l0, l1, l2 := closestTriangle(a, b, c)
		p := a.Mul(l0).Add(b.Mul(l1)).Add(c.Mul(l2))
		if dist := p.Dot(p); dist < bestDist {
			bestDist = dist
			best = [4]float32{}
			best[f[0]], best[f[1]], best[f[2]] = l0, l1, l2
		}
	}
	return best, outside
}

// gjkResult is the outcome of a distance query between two cores.
type gjkResult struct {
	pa, pb  lin.Vec3 // closest core points when not overlapping
	dist    float32
	overlap bool
	simplex [4]gjkVert
	n       int
}

// gjk finds the closest points between two convex cores, or that they
// overlap.
func gjk(a, b *convex) gjkResult {
	var s [4]gjkVert
	n := 0
	v := a.center.Sub(b.center)
	if v.Dot(v) < 1e-12 {
		v = lin.V3(1, 0, 0)
	}
	var lam [4]float32
	var res gjkResult
	// Closer than this and the cores touch: float error swamps the
	// direction, and EPA handles the boundary case.
	touch := 1e-4 * max(a.size+b.size, 1e-3)
	touch *= touch
	for range 64 {
		vv := v.Dot(v)
		if n > 0 && vv < touch {
			res.overlap = true
			break
		}
		sa, sb := a.sup(v.Neg()), b.sup(v)
		w := sa.Sub(sb)
		if n > 0 && vv-v.Dot(w) <= 1e-4*vv {
			break
		}
		dup := false
		for i := range n {
			if s[i].w == w {
				dup = true
			}
		}
		if dup {
			break
		}
		s[n] = gjkVert{w, sa, sb}
		n++
		var inside bool
		v, lam, inside = closestSimplex(&s, &n)
		if inside {
			res.overlap = true
			break
		}
	}
	for i := range n {
		res.pa = res.pa.Add(s[i].a.Mul(lam[i]))
		res.pb = res.pb.Add(s[i].b.Mul(lam[i]))
	}
	res.dist = res.pa.Sub(res.pb).Len()
	res.simplex, res.n = s, n
	return res
}

// epaEdge is one edge of the horizon left when faces are removed.
type epaEdge struct{ a, b int }

// epaFace is one triangle of the expanding polytope.
type epaFace struct {
	v [3]int
	n lin.Vec3 // unit, outward
	d float32  // distance from the origin
}

// epa finds the penetration of two overlapping cores: the shortest
// translation of B that separates them, and the deepest points.
func epa(sc *scratch3, a, b *convex, r gjkResult) (normal lin.Vec3, depth float32, pa, pb lin.Vec3, ok bool) {
	verts := append(sc.epaVerts[:0], r.simplex[:r.n]...)
	defer func() { sc.epaVerts = verts }()
	csoSupport := func(dir lin.Vec3) gjkVert {
		sa, sb := a.sup(dir), b.sup(dir.Neg())
		return gjkVert{sa.Sub(sb), sa, sb}
	}
	size := max(a.size+b.size, 1e-3)
	eps := 1e-4 * size
	// Grow a degenerate simplex into a tetrahedron.
	for len(verts) < 4 {
		var buf [6]lin.Vec3
		var dirs []lin.Vec3
		switch len(verts) {
		case 1:
			buf = [6]lin.Vec3{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1}}
			dirs = buf[:6]
		case 2:
			e := verts[1].w.Sub(verts[0].w)
			p := perpendicular(e)
			q := e.Cross(p).Norm()
			buf[0], buf[1], buf[2], buf[3] = p, p.Neg(), q, q.Neg()
			dirs = buf[:4]
		default:
			n := verts[1].w.Sub(verts[0].w).Cross(verts[2].w.Sub(verts[0].w)).Norm()
			buf[0], buf[1] = n, n.Neg()
			dirs = buf[:2]
		}
		added := false
		for _, d := range dirs {
			w := csoSupport(d)
			degenerate := false
			switch len(verts) {
			case 1:
				degenerate = w.w.Sub(verts[0].w).Len() < eps
			case 2:
				degenerate = w.w.Sub(verts[0].w).Cross(verts[1].w.Sub(verts[0].w)).Len() < eps*eps
			default:
				n := verts[1].w.Sub(verts[0].w).Cross(verts[2].w.Sub(verts[0].w)).Norm()
				degenerate = abs32(n.Dot(w.w.Sub(verts[0].w))) < eps
			}
			if !degenerate {
				verts = append(verts, w)
				added = true
				break
			}
		}
		if !added {
			return lin.Vec3{}, 0, lin.Vec3{}, lin.Vec3{}, false
		}
	}
	makeFace := func(i, j, k int) epaFace {
		f := epaFace{v: [3]int{i, j, k}}
		vi, vj, vk := verts[i].w, verts[j].w, verts[k].w
		f.n = vj.Sub(vi).Cross(vk.Sub(vi))
		if l := f.n.Len(); l > 1e-12 {
			f.n = f.n.Mul(1 / l)
		}
		f.d = f.n.Dot(vi)
		return f
	}
	// Orient each face of the tetrahedron away from the opposite vertex.
	faces := sc.epaFaces[:0]
	defer func() { sc.epaFaces = faces }()
	for _, t := range [4][4]int{{0, 1, 2, 3}, {0, 3, 1, 2}, {0, 2, 3, 1}, {1, 3, 2, 0}} {
		f := makeFace(t[0], t[1], t[2])
		if f.n.Dot(verts[t[3]].w.Sub(verts[t[0]].w)) > 0 {
			f = makeFace(t[0], t[2], t[1])
		}
		faces = append(faces, f)
	}
	var best epaFace
	for range 64 {
		bi := 0
		for i, f := range faces {
			if f.d < faces[bi].d {
				bi = i
			}
		}
		best = faces[bi]
		w := csoSupport(best.n)
		if w.w.Dot(best.n)-best.d < eps {
			break
		}
		// Remove the faces that see the new vertex and stitch it to the
		// horizon edges left behind.
		horizon := sc.horizon[:0]
		kept := faces[:0]
		for _, f := range faces {
			if f.n.Dot(w.w.Sub(verts[f.v[0]].w)) > 0 {
				for k := range 3 {
					e := epaEdge{f.v[k], f.v[(k+1)%3]}
					found := false
					for i, h := range horizon {
						if h.a == e.b && h.b == e.a {
							horizon = append(horizon[:i], horizon[i+1:]...)
							found = true
							break
						}
					}
					if !found {
						horizon = append(horizon, e)
					}
				}
			} else {
				kept = append(kept, f)
			}
		}
		sc.horizon = horizon
		if len(horizon) == 0 {
			break
		}
		verts = append(verts, w)
		wi := len(verts) - 1
		faces = kept
		for _, e := range horizon {
			f := makeFace(e.a, e.b, wi)
			if f.d < 0 {
				f = makeFace(e.b, e.a, wi)
			}
			faces = append(faces, f)
		}
	}
	// The origin's projection onto the closest face, in barycentrics,
	// locates the deepest points of each core.
	p := best.n.Mul(best.d)
	va, vb, vc := verts[best.v[0]], verts[best.v[1]], verts[best.v[2]]
	l0, l1, l2 := barycentric(p, va.w, vb.w, vc.w)
	pa = va.a.Mul(l0).Add(vb.a.Mul(l1)).Add(vc.a.Mul(l2))
	pb = va.b.Mul(l0).Add(vb.b.Mul(l1)).Add(vc.b.Mul(l2))
	return best.n, best.d, pa, pb, true
}

// barycentric gives the weights of p in triangle abc (p on its plane).
func barycentric(p, a, b, c lin.Vec3) (float32, float32, float32) {
	v0, v1, v2 := b.Sub(a), c.Sub(a), p.Sub(a)
	d00, d01, d11 := v0.Dot(v0), v0.Dot(v1), v1.Dot(v1)
	d20, d21 := v2.Dot(v0), v2.Dot(v1)
	denom := d00*d11 - d01*d01
	if math.Abs(float64(denom)) < 1e-12 {
		return 1, 0, 0
	}
	v := (d11*d20 - d01*d21) / denom
	w := (d00*d21 - d01*d20) / denom
	return 1 - v - w, v, w
}

// castRay finds where the ray origin + t·dir, t in [0, 1], first reaches
// the grown convex shape, with the surface normal there. A ray that starts
// inside reports t 0 and a zero normal.
func castRay[S supporter](sup S, origin, dir lin.Vec3, size float32) (t float32, normal lin.Vec3, ok bool) {
	x := origin
	v := x.Sub(sup.support(lin.V3(0, 1, 0)))
	var s [4]gjkVert
	n := 0
	eps := 1e-4 * max(size, 1e-3)
	eps *= eps
	for range 64 {
		if v.Dot(v) <= eps {
			break
		}
		p := sup.support(v)
		w := x.Sub(p)
		if vw := v.Dot(w); vw > 0 {
			vr := v.Dot(dir)
			if vr >= 0 {
				return 0, lin.Vec3{}, false
			}
			t -= vw / vr
			if t > 1 {
				return 0, lin.Vec3{}, false
			}
			x = origin.Add(dir.Mul(t))
			normal = v
		}
		if n == 4 {
			n = 3
		}
		s[n] = gjkVert{a: p}
		n++
		for i := range n {
			s[i].w = x.Sub(s[i].a)
		}
		var inside bool
		v, _, inside = closestSimplex(&s, &n)
		if inside {
			break
		}
	}
	if normal == (lin.Vec3{}) {
		return 0, lin.Vec3{}, true
	}
	return t, normal.Norm(), true
}
