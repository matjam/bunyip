package phys

import (
	"math"
	"sort"
	"sync"

	"github.com/matjam/bunyip/lin"
)

// MeshShape is a triangle mesh for static geometry such as terrain and
// level walls: Indices are triples into Vertices, and either winding is
// fine since triangles collide from both sides. Bodies do not carry
// meshes; dynamic shapes collide against them. NewMeshShape builds the
// triangle tree once. A literal MeshShape builds it on first use and
// keeps it by the identity of the slices, so do not change them after.
type MeshShape struct {
	Vertices []lin.Vec3
	Indices  []uint32

	tree *meshTree
}

// NewMeshShape makes a mesh collider with its triangle tree built.
func NewMeshShape(vertices []lin.Vec3, indices []uint32) MeshShape {
	m := MeshShape{Vertices: vertices, Indices: indices}
	m.tree = buildMeshTree(m)
	return m
}

// meshTree is a bounding volume hierarchy over the triangles, in the
// mesh's own frame.
type meshTree struct {
	nodes  []meshNode
	tris   []int
	lo, hi lin.Vec3
}

type meshNode struct {
	lo, hi       lin.Vec3
	left, right  int // -1 on a leaf
	start, count int
}

type meshKey struct {
	v  *lin.Vec3
	nv int
	i  *uint32
	ni int
}

var meshTrees sync.Map

// treeOf returns the mesh's tree, building and caching it on first use.
func (m MeshShape) treeOf() *meshTree {
	if m.tree != nil {
		return m.tree
	}
	if len(m.Vertices) == 0 || len(m.Indices) < 3 {
		return &meshTree{}
	}
	key := meshKey{&m.Vertices[0], len(m.Vertices), &m.Indices[0], len(m.Indices)}
	if t, ok := meshTrees.Load(key); ok {
		return t.(*meshTree)
	}
	t := buildMeshTree(m)
	meshTrees.Store(key, t)
	return t
}

func (m MeshShape) triangleCount() int { return len(m.Indices) / 3 }

// triangle returns the corners of triangle i in the mesh's frame.
func (m MeshShape) triangle(i int) (lin.Vec3, lin.Vec3, lin.Vec3) {
	n := uint32(len(m.Vertices))
	a, b, c := m.Indices[i*3], m.Indices[i*3+1], m.Indices[i*3+2]
	if a >= n || b >= n || c >= n {
		return lin.Vec3{}, lin.Vec3{}, lin.Vec3{}
	}
	return m.Vertices[a], m.Vertices[b], m.Vertices[c]
}

func buildMeshTree(m MeshShape) *meshTree {
	n := m.triangleCount()
	t := &meshTree{tris: make([]int, n)}
	if n == 0 {
		return t
	}
	los, his, mids := make([]lin.Vec3, n), make([]lin.Vec3, n), make([]lin.Vec3, n)
	for i := range n {
		a, b, c := m.triangle(i)
		los[i], his[i] = a.Min(b).Min(c), a.Max(b).Max(c)
		mids[i] = los[i].Add(his[i]).Mul(0.5)
		t.tris[i] = i
	}
	var build func(start, count int) int
	build = func(start, count int) int {
		node := meshNode{left: -1, right: -1, start: start, count: count}
		node.lo = lin.V3(float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1)))
		node.hi = node.lo.Neg()
		for _, ti := range t.tris[start : start+count] {
			node.lo, node.hi = node.lo.Min(los[ti]), node.hi.Max(his[ti])
		}
		id := len(t.nodes)
		t.nodes = append(t.nodes, node)
		if count <= 4 {
			return id
		}
		ext := node.hi.Sub(node.lo)
		axis := 0
		if ext.Y > ext.X && ext.Y >= ext.Z {
			axis = 1
		} else if ext.Z > ext.X && ext.Z > ext.Y {
			axis = 2
		}
		part := t.tris[start : start+count]
		sort.Slice(part, func(a, b int) bool {
			return component(mids[part[a]], axis) < component(mids[part[b]], axis)
		})
		half := count / 2
		l := build(start, half)
		r := build(start+half, count-half)
		t.nodes[id].left, t.nodes[id].right = l, r
		t.nodes[id].count = 0
		return id
	}
	build(0, n)
	t.lo, t.hi = t.nodes[0].lo, t.nodes[0].hi
	return t
}

func component(v lin.Vec3, axis int) float32 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	}
	return v.Z
}

// query calls fn for every triangle whose bounds overlap the box, in
// the mesh's frame. It walks the tree by recursion, whose depth is the
// tree's, so a query allocates nothing.
func (t *meshTree) query(lo, hi lin.Vec3, fn func(tri int)) {
	if len(t.nodes) == 0 {
		return
	}
	t.queryNode(0, lo, hi, fn)
}

func (t *meshTree) queryNode(id int, lo, hi lin.Vec3, fn func(tri int)) {
	n := &t.nodes[id]
	if n.lo.X > hi.X || lo.X > n.hi.X || n.lo.Y > hi.Y || lo.Y > n.hi.Y || n.lo.Z > hi.Z || lo.Z > n.hi.Z {
		return
	}
	if n.left < 0 {
		for _, ti := range t.tris[n.start : n.start+n.count] {
			fn(ti)
		}
		return
	}
	t.queryNode(n.right, lo, hi, fn)
	t.queryNode(n.left, lo, hi, fn)
}

func (m MeshShape) bounds(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	t := m.treeOf()
	return rotatedBounds(t.lo, t.hi, pos, rot)
}

func (m MeshShape) inertia(float32) lin.Vec3 { return lin.Vec3{} }

// rotatedBounds is the world box around a local box placed by pos and rot.
func rotatedBounds(lo, hi, pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	c := lo.Add(hi).Mul(0.5)
	return Box3{Half: hi.Sub(lo).Mul(0.5)}.bounds(pos.Add(rot.mulVec(c)), rot)
}

// localBounds is the box in the mesh's frame around a world box.
func localBounds(lo, hi, pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	inv := rot.transpose()
	c := inv.mulVec(lo.Add(hi).Mul(0.5).Sub(pos))
	return Box3{Half: hi.Sub(lo).Mul(0.5)}.bounds(c, inv)
}

// meshContacts collides a placed shape with the mesh's triangles; normals
// point from the shape into the mesh.
func meshContacts(sc *scratch3, out []contact3, m MeshShape, mpos lin.Vec3, mrot mat3, s Shape3, spos lin.Vec3, srot mat3) []contact3 {
	var ok bool
	sc.convA, sc.hullA, ok = placeConvex(sc.hullA, s, spos, srot)
	if !ok {
		return out
	}
	a := &sc.convA
	slo, shi := s.bounds(spos, srot)
	lo, hi := localBounds(slo, shi, mpos, mrot)
	m.treeOf().query(lo, hi, func(ti int) {
		p, q, r := m.triangle(ti)
		p, q, r = mpos.Add(mrot.mulVec(p)), mpos.Add(mrot.mulVec(q)), mpos.Add(mrot.mulVec(r))
		tn := q.Sub(p).Cross(r.Sub(p))
		if tn.Len() < 1e-12 {
			return
		}
		tn = tn.Norm()
		if tn.Dot(a.center.Sub(p)) < 0 {
			tn = tn.Neg()
		}
		sc.convB = triangleConvex(p, q, r)
		into := tn.Neg()
		out = pairContacts(sc, out, a, &sc.convB, &into, p)
	})
	return out
}

// rayTriangle intersects a ray with a two-sided triangle, returning t in
// [0, 1] and the face normal turned against the ray.
func rayTriangle(r Ray3, a, b, c lin.Vec3) (float32, lin.Vec3, bool) {
	e1, e2 := b.Sub(a), c.Sub(a)
	p := r.Dir.Cross(e2)
	det := e1.Dot(p)
	if math.Abs(float64(det)) < 1e-12 {
		return 0, lin.Vec3{}, false
	}
	inv := 1 / det
	s := r.Origin.Sub(a)
	u := s.Dot(p) * inv
	if u < 0 || u > 1 {
		return 0, lin.Vec3{}, false
	}
	q := s.Cross(e1)
	v := r.Dir.Dot(q) * inv
	if v < 0 || u+v > 1 {
		return 0, lin.Vec3{}, false
	}
	t := e2.Dot(q) * inv
	if t < 0 || t > 1 {
		return 0, lin.Vec3{}, false
	}
	n := e1.Cross(e2).Norm()
	if n.Dot(r.Dir) > 0 {
		n = n.Neg()
	}
	return t, n, true
}

// rayMesh casts a ray at a placed mesh.
func rayMesh(r Ray3, m MeshShape, pos lin.Vec3, rot mat3) (float32, lin.Vec3, bool) {
	inv := rot.transpose()
	local := Ray3{Origin: inv.mulVec(r.Origin.Sub(pos)), Dir: inv.mulVec(r.Dir)}
	end := local.Origin.Add(local.Dir)
	lo, hi := local.Origin.Min(end), local.Origin.Max(end)
	best, found := float32(math.Inf(1)), false
	var bestN lin.Vec3
	m.treeOf().query(lo, hi, func(ti int) {
		a, b, c := m.triangle(ti)
		if t, n, ok := rayTriangle(local, a, b, c); ok && t < best {
			best, bestN, found = t, n, true
		}
	})
	if !found {
		return 0, lin.Vec3{}, false
	}
	return best, rot.mulVec(bestN), true
}

// sweepMesh moves a convex shape by delta and finds the first triangle
// it touches.
func sweepMesh(m MeshShape, pos lin.Vec3, rot mat3, a *convex, slo, shi, delta lin.Vec3) (t float32, normal, point lin.Vec3, ok bool) {
	end0, end1 := slo.Add(delta), shi.Add(delta)
	lo, hi := localBounds(slo.Min(end0), shi.Max(end1), pos, rot)
	best := float32(math.Inf(1))
	m.treeOf().query(lo, hi, func(ti int) {
		p, q, r := m.triangle(ti)
		tri := triangleConvex(pos.Add(rot.mulVec(p)), pos.Add(rot.mulVec(q)), pos.Add(rot.mulVec(r)))
		if tt, n, pt, hit := sweepConvex(a, &tri, delta); hit && tt < best {
			best, normal, point, ok = tt, n, pt, true
		}
	})
	return best, normal, point, ok
}

// closestPointTriangle is the point of triangle abc nearest p.
func closestPointTriangle(p, a, b, c lin.Vec3) lin.Vec3 {
	// Shift so p is the origin and reuse the simplex solver.
	l0, l1, l2 := closestTriangle(a.Sub(p), b.Sub(p), c.Sub(p))
	return a.Mul(l0).Add(b.Mul(l1)).Add(c.Mul(l2))
}

// closestPointMesh finds the mesh point nearest p within radius.
func closestPointMesh(m MeshShape, pos lin.Vec3, rot mat3, p lin.Vec3, radius float32) (lin.Vec3, float32, bool) {
	inv := rot.transpose()
	lp := inv.mulVec(p.Sub(pos))
	r := lin.V3(radius, radius, radius)
	best, found := radius, false
	var bestP lin.Vec3
	m.treeOf().query(lp.Sub(r), lp.Add(r), func(ti int) {
		a, b, c := m.triangle(ti)
		q := closestPointTriangle(lp, a, b, c)
		if d := q.Sub(lp).Len(); d <= best {
			best, bestP, found = d, q, true
		}
	})
	if !found {
		return lin.Vec3{}, 0, false
	}
	return pos.Add(rot.mulVec(bestP)), best, true
}
