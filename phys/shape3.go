package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// mat3 is a row-major 3×3 matrix for inertia and rotations.
type mat3 [9]float32

func mat3FromQuat(q lin.Quat) mat3 {
	if q == (lin.Quat{}) {
		q = lin.QuatIdentity()
	}
	x, y, z, w := q.X, q.Y, q.Z, q.W
	return mat3{
		1 - 2*(y*y+z*z), 2 * (x*y - z*w), 2 * (x*z + y*w),
		2 * (x*y + z*w), 1 - 2*(x*x+z*z), 2 * (y*z - x*w),
		2 * (x*z - y*w), 2 * (y*z + x*w), 1 - 2*(x*x+y*y),
	}
}

func (m mat3) mulVec(v lin.Vec3) lin.Vec3 {
	return lin.V3(m[0]*v.X+m[1]*v.Y+m[2]*v.Z, m[3]*v.X+m[4]*v.Y+m[5]*v.Z, m[6]*v.X+m[7]*v.Y+m[8]*v.Z)
}

func (m mat3) transpose() mat3 {
	return mat3{m[0], m[3], m[6], m[1], m[4], m[7], m[2], m[5], m[8]}
}

func (m mat3) mul(n mat3) mat3 {
	var o mat3
	for r := range 3 {
		for c := range 3 {
			o[r*3+c] = m[r*3]*n[c] + m[r*3+1]*n[3+c] + m[r*3+2]*n[6+c]
		}
	}
	return o
}

func (m mat3) axis(i int) lin.Vec3 { return lin.V3(m[i], m[3+i], m[6+i]) }

// diag3 makes a diagonal matrix.
func diag3(x, y, z float32) mat3 { return mat3{x, 0, 0, 0, y, 0, 0, 0, z} }

// Shape3 is a collider volume in the body's local frame.
type Shape3 interface {
	bounds(pos lin.Vec3, rot mat3) (lo, hi lin.Vec3)
	// inertia returns the diagonal local inertia for the given mass.
	inertia(mass float32) lin.Vec3
}

// Sphere is a ball centred on the body.
type Sphere struct{ Radius float32 }

func (s Sphere) bounds(pos lin.Vec3, _ mat3) (lin.Vec3, lin.Vec3) {
	r := lin.V3(s.Radius, s.Radius, s.Radius)
	return pos.Sub(r), pos.Add(r)
}

func (s Sphere) inertia(mass float32) lin.Vec3 {
	i := 0.4 * mass * s.Radius * s.Radius
	return lin.V3(i, i, i)
}

// Box3 is a box centred on the body with the given half extents.
type Box3 struct{ Half lin.Vec3 }

func (b Box3) bounds(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	// Project the half extents onto each world axis.
	ex := float32(math.Abs(float64(rot[0])))*b.Half.X + float32(math.Abs(float64(rot[1])))*b.Half.Y + float32(math.Abs(float64(rot[2])))*b.Half.Z
	ey := float32(math.Abs(float64(rot[3])))*b.Half.X + float32(math.Abs(float64(rot[4])))*b.Half.Y + float32(math.Abs(float64(rot[5])))*b.Half.Z
	ez := float32(math.Abs(float64(rot[6])))*b.Half.X + float32(math.Abs(float64(rot[7])))*b.Half.Y + float32(math.Abs(float64(rot[8])))*b.Half.Z
	e := lin.V3(ex, ey, ez)
	return pos.Sub(e), pos.Add(e)
}

func (b Box3) inertia(mass float32) lin.Vec3 {
	w, h, d := 2*b.Half.X, 2*b.Half.Y, 2*b.Half.Z
	return lin.V3(mass*(h*h+d*d)/12, mass*(w*w+d*d)/12, mass*(w*w+h*h)/12)
}

// contact3 is one contact point between two colliders.
type contact3 struct {
	point  lin.Vec3
	normal lin.Vec3 // from A to B
	depth  float32
}

// obb is a placed box.
type obb struct {
	center lin.Vec3
	rot    mat3
	half   lin.Vec3
}

// collide3 appends the contacts between two placed shapes to out and
// returns it; normals point from A to B. Compounds split into their
// parts, meshes into the triangles near the other shape; sphere and box
// pairs have exact tests and every other pair goes through the support
// functions. The scratch carries the working buffers so a step allocates
// nothing.
func collide3(sc *scratch3, out []contact3, sa Shape3, pa lin.Vec3, ra mat3, sb Shape3, pb lin.Vec3, rb mat3) []contact3 {
	if c, ok := sa.(Compound3); ok {
		for _, p := range c.Parts {
			if p.Shape == nil {
				continue
			}
			pp, pr := p.place(pa, ra)
			out = collide3(sc, out, p.Shape, pp, pr, sb, pb, rb)
		}
		return out
	}
	if c, ok := sb.(Compound3); ok {
		for _, p := range c.Parts {
			if p.Shape == nil {
				continue
			}
			pp, pr := p.place(pb, rb)
			out = collide3(sc, out, sa, pa, ra, p.Shape, pp, pr)
		}
		return out
	}
	if m, ok := sa.(MeshShape); ok {
		start := len(out)
		out = meshContacts(sc, out, m, pa, ra, sb, pb, rb)
		for i := start; i < len(out); i++ {
			out[i].normal = out[i].normal.Neg()
		}
		return out
	}
	if m, ok := sb.(MeshShape); ok {
		return meshContacts(sc, out, m, pb, rb, sa, pa, ra)
	}
	switch a := sa.(type) {
	case Sphere:
		switch b := sb.(type) {
		case Sphere:
			return sphereSphere(out, a, pa, b, pb)
		case Box3:
			return sphereBox(out, a, pa, obb{pb, rb, b.Half})
		}
	case Box3:
		switch b := sb.(type) {
		case Sphere:
			start := len(out)
			out = sphereBox(out, b, pb, obb{pa, ra, a.Half})
			for i := start; i < len(out); i++ {
				out[i].normal = out[i].normal.Mul(-1)
			}
			return out
		case Box3:
			return boxBox(sc, out, obb{pa, ra, a.Half}, obb{pb, rb, b.Half})
		}
	}
	return convexPair(sc, out, sa, pa, ra, sb, pb, rb)
}

func sphereSphere(out []contact3, a Sphere, pa lin.Vec3, b Sphere, pb lin.Vec3) []contact3 {
	d := pb.Sub(pa)
	dist := d.Len()
	r := a.Radius + b.Radius
	if dist >= r {
		return out
	}
	n := lin.V3(0, 1, 0)
	if dist > 1e-6 {
		n = d.Mul(1 / dist)
	}
	return append(out, contact3{point: pa.Add(n.Mul(a.Radius)), normal: n, depth: r - dist})
}

func sphereBox(out []contact3, s Sphere, ps lin.Vec3, b obb) []contact3 {
	local := b.rot.transpose().mulVec(ps.Sub(b.center))
	closest := lin.V3(lin.Clamp(local.X, -b.half.X, b.half.X), lin.Clamp(local.Y, -b.half.Y, b.half.Y), lin.Clamp(local.Z, -b.half.Z, b.half.Z))
	d := local.Sub(closest)
	dist := d.Len()
	if dist > 1e-6 {
		if dist >= s.Radius {
			return out
		}
		nLocal := d.Mul(1 / dist)
		n := b.rot.mulVec(nLocal) // from box toward sphere
		point := b.center.Add(b.rot.mulVec(closest))
		return append(out, contact3{point: point, normal: n.Mul(-1), depth: s.Radius - dist})
	}
	// Centre inside the box: exit through the nearest face.
	best := 0
	bestGap := b.half.X - float32(math.Abs(float64(local.X)))
	if gap := b.half.Y - float32(math.Abs(float64(local.Y))); gap < bestGap {
		best, bestGap = 1, gap
	}
	if gap := b.half.Z - float32(math.Abs(float64(local.Z))); gap < bestGap {
		best, bestGap = 2, gap
	}
	var nLocal lin.Vec3
	switch best {
	case 0:
		nLocal = lin.V3(sign(local.X), 0, 0)
	case 1:
		nLocal = lin.V3(0, sign(local.Y), 0)
	default:
		nLocal = lin.V3(0, 0, sign(local.Z))
	}
	n := b.rot.mulVec(nLocal)
	return append(out, contact3{point: ps, normal: n.Mul(-1), depth: s.Radius + bestGap})
}

func sign(v float32) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

// boxBox is the separating-axis test over the fifteen candidate axes,
// with contact points from the incident box's vertices inside the
// reference face.
func boxBox(sc *scratch3, out []contact3, a, b obb) []contact3 {
	var axes [15]lin.Vec3
	for i := range 3 {
		axes[i] = a.rot.axis(i)
		axes[3+i] = b.rot.axis(i)
	}
	k := 6
	for i := range 3 {
		for j := range 3 {
			axes[k] = a.rot.axis(i).Cross(b.rot.axis(j))
			k++
		}
	}
	d := b.center.Sub(a.center)
	bestAxis, bestOverlap := -1, float32(math.Inf(1))
	var bestN lin.Vec3
	for i, ax := range axes {
		if ax.Len() < 1e-6 {
			continue // parallel edges
		}
		ax = ax.Norm()
		ra := projectHalf(a, ax)
		rb := projectHalf(b, ax)
		dist := d.Dot(ax)
		overlap := ra + rb - float32(math.Abs(float64(dist)))
		if overlap <= 0 {
			return out
		}
		// Edge axes get a slight penalty so faces win ties, for stability.
		weight := overlap
		if i >= 6 {
			weight *= 1.05
		}
		if weight < bestOverlap {
			bestOverlap = weight
			bestAxis = i
			bestN = ax
			if dist < 0 {
				bestN = ax.Mul(-1)
			}
		}
	}
	if bestAxis < 0 {
		return out
	}
	n := bestN // from A to B
	if bestAxis >= 6 {
		// Edge against edge: the contact is between the closest points
		// of the two support edges, not their support vertices, which on
		// a long box can sit far from where the edges actually cross.
		ia, ib := (bestAxis-6)/3, (bestAxis-6)%3
		pa, da, ha := a.supportEdge(n, ia)
		pb, db, hb := b.supportEdge(n.Mul(-1), ib)
		ca, cb := closestOnSegments(pa, da, ha, pb, db, hb)
		overlap := ca.Sub(cb).Dot(n)
		return append(out, contact3{point: ca.Add(cb).Mul(0.5), normal: n, depth: max(overlap, 0)})
	}
	// Face axis: reference box is the one whose face it is.
	ref, inc := a, b
	refN := n
	if bestAxis >= 3 {
		ref, inc = b, a
		refN = n.Mul(-1) // reference normal points out of the reference box
	}
	refAxis := bestAxis % 3
	// The incident face is the face of the other box most opposed to the
	// reference normal. Its polygon is clipped against the four side
	// planes of the reference face, so only the part above the face
	// counts: a corner hanging past the edge of a ledge must not become a
	// deep contact that launches the body.
	poly := inc.faceInto(sc.polyA[:0], refN.Mul(-1))
	buf := sc.polyB[:0]
	for i := range 3 {
		if i == refAxis {
			continue
		}
		u := ref.rot.axis(i)
		h := [3]float32{ref.half.X, ref.half.Y, ref.half.Z}[i]
		cu := ref.center.Dot(u)
		buf = clipPolygon(buf[:0], poly, u, cu+h)
		poly, buf = buf, poly
		buf = clipPolygon(buf[:0], poly, u.Mul(-1), -cu+h)
		poly, buf = buf, poly
	}
	sc.polyA, sc.polyB = poly, buf
	// Reference face plane: point on face = ref.center + refN * extent.
	planeD := ref.center.Dot(refN) + projectHalf(ref, refN)
	start := len(out)
	var deepest contact3
	deepest.depth = float32(math.Inf(-1))
	for _, v := range poly {
		depth := planeD - v.Dot(refN) // positive when v is inside the reference face
		c := contact3{point: v, normal: n, depth: depth}
		if depth > deepest.depth {
			deepest = c
		}
		if depth >= 0 && len(out)-start < 4 {
			out = append(out, c)
		}
	}
	if len(out) == start {
		if deepest.depth == float32(math.Inf(-1)) {
			return out
		}
		out = append(out, deepest)
	}
	return out
}

// supportEdge returns the box edge parallel to local axis i that is
// farthest along dir: its midpoint, unit direction and half-length.
func (o obb) supportEdge(dir lin.Vec3, i int) (mid, d lin.Vec3, half float32) {
	halves := [3]float32{o.half.X, o.half.Y, o.half.Z}
	mid = o.center
	for j := range 3 {
		if j == i {
			continue
		}
		ax := o.rot.axis(j)
		mid = mid.Add(ax.Mul(sign(ax.Dot(dir)) * halves[j]))
	}
	return mid, o.rot.axis(i), halves[i]
}

// closestOnSegments finds the closest points between two segments given
// by midpoint, unit direction and half-length.
func closestOnSegments(pa, da lin.Vec3, ha float32, pb, db lin.Vec3, hb float32) (lin.Vec3, lin.Vec3) {
	r := pa.Sub(pb)
	c := da.Dot(db)
	e := da.Dot(r)
	f := db.Dot(r)
	denom := 1 - c*c
	var s, t float32
	if denom > 1e-6 {
		s = lin.Clamp((c*f-e)/denom, -ha, ha)
	}
	t = c*s + f
	if t < -hb {
		t = -hb
		s = lin.Clamp(-e-c*hb, -ha, ha)
	} else if t > hb {
		t = hb
		s = lin.Clamp(-e+c*hb, -ha, ha)
	}
	return pa.Add(da.Mul(s)), pb.Add(db.Mul(t))
}

// faceInto appends the four corners of the box face whose outward normal
// is closest to dir, in order around the face, to dst.
func (o obb) faceInto(dst []lin.Vec3, dir lin.Vec3) []lin.Vec3 {
	best, bestDot := 0, float32(math.Inf(-1))
	for i := range 3 {
		if d := float32(math.Abs(float64(o.rot.axis(i).Dot(dir)))); d > bestDot {
			best, bestDot = i, d
		}
	}
	half := [3]float32{o.half.X, o.half.Y, o.half.Z}
	n := o.rot.axis(best)
	if n.Dot(dir) < 0 {
		n = n.Mul(-1)
	}
	centre := o.center.Add(n.Mul(half[best]))
	j, k := (best+1)%3, (best+2)%3
	u := o.rot.axis(j).Mul(half[j])
	v := o.rot.axis(k).Mul(half[k])
	return append(dst, centre.Add(u).Add(v), centre.Add(u).Sub(v), centre.Sub(u).Sub(v), centre.Sub(u).Add(v))
}

// clipPolygon appends the part of a convex polygon on the inside of the
// plane n·p <= d to out and returns it (Sutherland-Hodgman). out must
// not share storage with poly.
func clipPolygon(out, poly []lin.Vec3, n lin.Vec3, d float32) []lin.Vec3 {
	if len(poly) == 0 {
		return out
	}
	prev := poly[len(poly)-1]
	prevD := prev.Dot(n) - d
	for _, cur := range poly {
		curD := cur.Dot(n) - d
		if prevD <= 0 && curD <= 0 {
			out = append(out, cur)
		} else if prevD <= 0 && curD > 0 {
			t := prevD / (prevD - curD)
			out = append(out, prev.Add(cur.Sub(prev).Mul(t)))
		} else if prevD > 0 && curD <= 0 {
			t := prevD / (prevD - curD)
			out = append(out, prev.Add(cur.Sub(prev).Mul(t)), cur)
		}
		prev, prevD = cur, curD
	}
	return out
}

// projectHalf is the box's half-length along a unit axis.
func projectHalf(o obb, axis lin.Vec3) float32 {
	return float32(math.Abs(float64(o.rot.axis(0).Dot(axis))))*o.half.X +
		float32(math.Abs(float64(o.rot.axis(1).Dot(axis))))*o.half.Y +
		float32(math.Abs(float64(o.rot.axis(2).Dot(axis))))*o.half.Z
}

// Ray3 is a ray for casts.
type Ray3 struct {
	Origin, Dir lin.Vec3 // Dir need not be unit length
}

func rayShape3(r Ray3, s Shape3, pos lin.Vec3, rot mat3) (t float32, normal lin.Vec3, ok bool) {
	switch sh := s.(type) {
	case Sphere:
		m := r.Origin.Sub(pos)
		a := r.Dir.Dot(r.Dir)
		b := 2 * m.Dot(r.Dir)
		c := m.Dot(m) - sh.Radius*sh.Radius
		disc := b*b - 4*a*c
		if disc < 0 || a == 0 {
			return 0, lin.Vec3{}, false
		}
		t = (-b - float32(math.Sqrt(float64(disc)))) / (2 * a)
		if t < 0 || t > 1 {
			return 0, lin.Vec3{}, false
		}
		return t, r.Origin.Add(r.Dir.Mul(t)).Sub(pos).Norm(), true
	case Box3:
		// Slab test in the box's frame.
		inv := rot.transpose()
		o := inv.mulVec(r.Origin.Sub(pos))
		d := inv.mulVec(r.Dir)
		tEnter, tExit := float32(0), float32(1)
		var axis int
		var enterSign float32
		for i, oi := range []float32{o.X, o.Y, o.Z} {
			di := []float32{d.X, d.Y, d.Z}[i]
			hi := []float32{sh.Half.X, sh.Half.Y, sh.Half.Z}[i]
			if math.Abs(float64(di)) < 1e-8 {
				if oi < -hi || oi > hi {
					return 0, lin.Vec3{}, false
				}
				continue
			}
			t1 := (-hi - oi) / di
			t2 := (hi - oi) / di
			s := float32(-1)
			if t1 > t2 {
				t1, t2 = t2, t1
				s = 1
			}
			if t1 > tEnter {
				tEnter, axis, enterSign = t1, i, s
			}
			tExit = min(tExit, t2)
			if tEnter > tExit {
				return 0, lin.Vec3{}, false
			}
		}
		if tEnter <= 0 {
			return 0, lin.Vec3{}, false
		}
		var nLocal lin.Vec3
		switch axis {
		case 0:
			nLocal = lin.V3(enterSign, 0, 0)
		case 1:
			nLocal = lin.V3(0, enterSign, 0)
		default:
			nLocal = lin.V3(0, 0, enterSign)
		}
		return tEnter, rot.mulVec(nLocal), true
	case MeshShape:
		return rayMesh(r, sh, pos, rot)
	case Compound3:
		best, found := float32(math.Inf(1)), false
		var bestN lin.Vec3
		for _, p := range sh.Parts {
			if p.Shape == nil {
				continue
			}
			pp, pr := p.place(pos, rot)
			if t, n, ok := rayShape3(r, p.Shape, pp, pr); ok && t < best {
				best, bestN, found = t, n, true
			}
		}
		return best, bestN, found
	}
	if c, ok := placeConvex(s, pos, rot); ok {
		return rayConvex(r, &c)
	}
	return 0, lin.Vec3{}, false
}
