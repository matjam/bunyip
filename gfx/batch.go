package gfx

import "github.com/matjam/bunyip/lin"

// batchLeaf is how many items a hierarchy node holds before it splits.
// Testing eight boxes costs about what one more level of the tree does.
const batchLeaf = 8

// BatchItem is one draw of a static batch: a mesh, its material and where
// it sits in the world, exactly what DrawMesh takes.
type BatchItem struct {
	Mesh     *Mesh
	Material Material
	Model    lin.Mat4
}

// StaticBatch is a set of mesh draws that never move, held behind a
// bounding volume hierarchy built once. Drawing the batch tests the
// hierarchy against the camera's frustum and the frame's occluders and
// queues only the items that survive, so a level's ten thousand rocks,
// crates and lamp posts cost a few dozen box tests instead of ten
// thousand. Items keep their own meshes and materials, so draws that
// share both are still merged into one instanced call. Items the camera
// cannot see still cast shadows: a subtree the camera rejects is walked
// again against the frame's shadow maps.
//
// A batch does not own its meshes or textures; destroy those as usual.
// Its meshes, materials and shader belong to the Graphics that built it;
// constructing or drawing with resources from another Graphics panics.
// Build one with NewStaticBatch and draw it with DrawBatch. Anything
// that moves belongs in DrawMesh instead: the hierarchy is built from
// the models given and is not rebuilt. Mesh geometry and bounds must also
// remain fixed; rebuild the batch after changing either. Include any
// shader displacement in mesh bounds before building the hierarchy.
type StaticBatch struct {
	g     *Graphics
	items []batchItem // prepared draws, in hierarchy order
	mats  []Material  // the items' distinct materials, with the defaults filled in
	nodes []batchNode
	lo    lin.Vec3
	hi    lin.Vec3
	// mapped holds, for each of mats, its index plus one in the table of
	// the queue and frame named by mapQueue and mapFrame, or zero when
	// that frame has not used it yet.
	mapped   []uint32
	mapQueue *drawQueue
	mapFrame uint64
}

// batchItem is one item of a static batch, with what a frame would
// otherwise work out per draw done once when the batch is built: its
// shader and its world bounding sphere.
type batchItem struct {
	mesh   *Mesh
	shader *Shader
	model  lin.Mat4
	centre lin.Vec3
	radius float32
	mat    int32 // index into the batch's mats
}

// batchNode is one node of the hierarchy. It covers total items from
// start, which are contiguous because build reorders them. A leaf has no
// right child; an inner node's left child is its own index plus one.
type batchNode struct {
	lo, hi lin.Vec3
	start  int32
	total  int32
	right  int32 // zero for a leaf, since no child can be the root
}

// NewStaticBatch builds the hierarchy over a set of draws that never
// move. Items with no mesh are skipped, and an empty set gives a batch
// that draws nothing. Building costs one pass over the items per level
// of the tree, so do it at load rather than every frame.
func (g *Graphics) NewStaticBatch(items []BatchItem) *StaticBatch {
	for i := range items {
		if it := &items[i]; it.Mesh != nil {
			g.requireMeshOwner(it.Mesh, &it.Material)
		}
	}
	b := &StaticBatch{g: g}
	// The items' materials are interned into a table of the batch's own,
	// so each frame looks up each distinct material once.
	var table drawQueue
	for i := range items {
		it := &items[i]
		if it.Mesh == nil {
			continue
		}
		var tmp Material
		mat := defaultedMaterial(&it.Material, &tmp)
		shader := mat.Shader
		if shader == nil {
			shader = g.meshes.defaultShader
		} else if !shader.mesh {
			panic("gfx: Material.Shader wants a mesh shader from NewMeshShader")
		}
		at, h := table.findMaterial(mat)
		if at < 0 {
			at = table.addMaterial(mat, shader, h)
			b.mats = append(b.mats, *mat)
		}
		centre, radius := it.Mesh.boundingSphere(it.Model)
		b.items = append(b.items, batchItem{mesh: it.Mesh, shader: shader, model: it.Model, centre: centre, radius: radius, mat: at})
	}
	b.mapped = make([]uint32, len(b.mats))
	if len(b.items) == 0 {
		return b
	}
	los := make([]lin.Vec3, len(b.items))
	his := make([]lin.Vec3, len(b.items))
	for i := range b.items {
		los[i], his[i] = itemBox(&b.items[i])
	}
	b.build(los, his, 0, len(b.items))
	b.lo, b.hi = b.nodes[0].lo, b.nodes[0].hi
	return b
}

// itemBox is an item's world bounds, the box its mesh bounds fill under
// its model matrix.
func itemBox(d *batchItem) (lo, hi lin.Vec3) {
	c, e := boxUnder(d.model, d.mesh.Min, d.mesh.Max)
	return c.Sub(e), c.Add(e)
}

// build adds a node covering items[from:to], splitting at the median of
// the longest axis of their centres and recursing. It reorders the items
// and their boxes together, so the tree's leaves are contiguous runs.
func (b *StaticBatch) build(los, his []lin.Vec3, from, to int) int32 {
	lo, hi := los[from], his[from]
	for i := from + 1; i < to; i++ {
		lo, hi = lo.Min(los[i]), hi.Max(his[i])
	}
	at := int32(len(b.nodes))
	b.nodes = append(b.nodes, batchNode{lo: lo, hi: hi, start: int32(from), total: int32(to - from)})
	if to-from <= batchLeaf {
		return at
	}
	// Split at the median centre along the box's longest axis. A median
	// split keeps the tree balanced whatever the items' spacing, which
	// matters more here than the tighter boxes a surface area heuristic
	// would find.
	size := hi.Sub(lo)
	axis := 0
	if size.Y > size.X {
		axis = 1
	}
	if size.Z > max(size.X, size.Y) {
		axis = 2
	}
	mid := (from + to) / 2
	b.nthCentre(los, his, from, to, mid, axis)
	b.build(los, his, from, mid)
	b.nodes[at].right = b.build(los, his, mid, to)
	return at
}

// axisOf reads one component of a vector, 0 for x, 1 for y and 2 for z.
func axisOf(v lin.Vec3, axis int) float32 {
	switch axis {
	case 0:
		return v.X
	case 1:
		return v.Y
	}
	return v.Z
}

// nthCentre partially sorts items[from:to] so that the item at n has the
// median centre along axis and everything before it is no greater. It is
// quickselect, so a batch costs one linear pass per level rather than a
// full sort per level.
func (b *StaticBatch) nthCentre(los, his []lin.Vec3, from, to, n, axis int) {
	centre := func(i int) float32 { return (axisOf(los[i], axis) + axisOf(his[i], axis)) * 0.5 }
	swap := func(i, j int) {
		los[i], los[j] = los[j], los[i]
		his[i], his[j] = his[j], his[i]
		b.items[i], b.items[j] = b.items[j], b.items[i]
	}
	for to-from > 1 {
		pivot := centre((from + to) / 2)
		i, j := from, to-1
		for i <= j {
			for centre(i) < pivot {
				i++
			}
			for centre(j) > pivot {
				j--
			}
			if i <= j {
				swap(i, j)
				i, j = i+1, j-1
			}
		}
		switch {
		case n <= j:
			to = j + 1
		case n >= i:
			from = i
		default:
			return
		}
	}
}

// Len is how many draws the batch holds.
func (b *StaticBatch) Len() int {
	if b == nil {
		return 0
	}
	return len(b.items)
}

// Bounds is the world box every item of the batch fits inside. An empty
// batch reports a zero box.
func (b *StaticBatch) Bounds() (min, max lin.Vec3) {
	if b == nil {
		return lin.Vec3{}, lin.Vec3{}
	}
	return b.lo, b.hi
}

// DrawBatch queues the items of a static batch the camera can see. The
// hierarchy is walked when the frame's draws are prepared, so occluders
// added after this call still cull it, and the items that survive are
// ordinary draws: instanced together, sorted, shadowed and lit like any
// others. Items the camera cannot see but a shadow map can are queued
// for the shadow pass alone, so they cast shadows as culled DrawMesh
// draws do.
func (g *Graphics) DrawBatch(b *StaticBatch) {
	if b == nil || len(b.items) == 0 {
		return
	}
	if b.g != g {
		panic("gfx: static batch belongs to another Graphics")
	}
	g.cur.batches = append(g.cur.batches, b)
}

// expandBatches walks each queued batch's hierarchy against the frustum
// and the occlusion buffer and appends the surviving items to the
// queue's draws, where the ordinary per-draw culling then sees them. A
// subtree the camera does not see is walked again against the frame's
// shadow volumes (q.volumes), and the items that may reach one are
// queued as culled draws, which only the shadow pass records, so a
// batch casts the same shadows as its items drawn one by one.
func (g *Graphics) expandBatches(q *drawQueue, frustum Frustum, viewProj lin.Mat4, occluding bool) {
	for _, b := range q.batches {
		g.walkBatch(q, b, frustum, viewProj, occluding, 0)
	}
}

// walkBatch visits one node, its children when it is inner, and its
// items when it is a leaf the camera can see.
func (g *Graphics) walkBatch(q *drawQueue, b *StaticBatch, frustum Frustum, viewProj lin.Mat4, occluding bool, at int32) {
	n := &b.nodes[at]
	g.stats.CullTests++
	if !frustum.ContainsBox(n.lo, n.hi) {
		g.stats.Culled += int(n.total)
		b.walkShadow(q, at, 1<<len(q.volumes)-1)
		return
	}
	if occluding {
		c := n.lo.Add(n.hi).Mul(0.5)
		if g.occ.hides(viewProj, c, n.hi.Sub(c).Len()) {
			g.stats.Culled += int(n.total)
			g.stats.Occluded += int(n.total)
			b.walkShadow(q, at, 1<<len(q.volumes)-1)
			return
		}
	}
	if n.right != 0 {
		g.walkBatch(q, b, frustum, viewProj, occluding, at+1)
		g.walkBatch(q, b, frustum, viewProj, occluding, n.right)
		return
	}
	b.queueItems(q, n.start, n.start+n.total, false)
}

// walkShadow visits a node the camera does not see, keeping in live the
// shadow volumes its box may reach, and queues a leaf's items as culled
// draws when any remain. The shadow pass then tests each item's sphere
// against each map as it does every other culled draw.
func (b *StaticBatch) walkShadow(q *drawQueue, at int32, live uint32) {
	n := &b.nodes[at]
	for v := range q.volumes {
		if live&(1<<v) != 0 && !q.volumes[v].touches(n.lo, n.hi) {
			live &^= 1 << v
		}
	}
	if live == 0 {
		return
	}
	if n.right != 0 {
		b.walkShadow(q, at+1, live)
		b.walkShadow(q, n.right, live)
		return
	}
	b.queueItems(q, n.start, n.start+n.total, true)
}

// queueItems appends items[from:to] to the queue's draws, with their
// cached bounds and their materials in the queue's table. shadowOnly
// queues them as culled draws for the shadow pass alone, and leaves out
// blended items, which cast no shadow.
func (b *StaticBatch) queueItems(q *drawQueue, from, to int32, shadowOnly bool) {
	if b.mapQueue != q || b.mapFrame != q.frame {
		b.mapQueue, b.mapFrame = q, q.frame
		clear(b.mapped)
	}
	for i := from; i < to; i++ {
		it := &b.items[i]
		m := b.mapped[it.mat]
		if m == 0 {
			// The batch checked the material's resources when it was built.
			mat := &b.mats[it.mat]
			at, h := q.findMaterial(mat)
			if at < 0 {
				at = q.addMaterial(mat, it.shader, h)
			}
			m = uint32(at) + 1
			b.mapped[it.mat] = m
		}
		if shadowOnly && q.mats[m-1].blended {
			continue
		}
		q.draws = append(q.draws, meshDraw{
			mesh: it.mesh, shader: it.shader, model: it.model,
			centre: it.centre, radius: it.radius, bounded: true, shadowOnly: shadowOnly,
			mat: m - 1, uniform: it.shader.uniformOffset(), // the arena moves every frame
			prev: -1, morph: -1,
		})
	}
}

// shadowVolume is the part of the world one shadow map can take casters
// from, for walking a static batch's hierarchy: a cascade's frustum
// without its near plane, a spot light's frustum within its range, or a
// point light's range sphere.
type shadowVolume struct {
	f          Frustum
	frustum    bool // f bounds the volume
	ignoreNear bool
	sphere     lin.Vec4 // xyz the light, w its range; a negative w for none
}

// touches reports whether a box may reach the volume. It errs towards
// yes, which costs a culled draw that the shadow pass then culls.
func (v *shadowVolume) touches(lo, hi lin.Vec3) bool {
	if v.frustum {
		for i, p := range v.f.planes {
			if v.ignoreNear && i == nearPlane {
				continue
			}
			n := p.Vec3()
			corner := lin.V3(pick(n.X >= 0, lo.X, hi.X), pick(n.Y >= 0, lo.Y, hi.Y), pick(n.Z >= 0, lo.Z, hi.Z))
			if n.Dot(corner)+p.W < -1e-4 {
				return false
			}
		}
	}
	if s := v.sphere; s.W >= 0 {
		c := s.Vec3()
		nearest := lin.V3(min(max(c.X, lo.X), hi.X), min(max(c.Y, lo.Y), hi.Y), min(max(c.Z, lo.Z), hi.Z))
		r := s.W*(1+1e-5) + 1e-4
		if d := nearest.Sub(c); d.Dot(d) > r*r {
			return false
		}
	}
	return true
}

// findShadowVolumes fills q.volumes with the frame's shadow maps'
// volumes: the cascades when the light casts shadows, whose matrices
// must be current, and each shadowed spot and point light.
func (q *drawQueue) findShadowVolumes() {
	q.volumes = q.volumesArr[:0]
	if q.light.Shadows {
		for k := range shadowCascades {
			q.volumes = append(q.volumes, shadowVolume{f: FrustumOf(q.cascadeMats[k]), frustum: true, ignoreNear: true, sphere: lin.V4(0, 0, 0, -1)})
		}
	}
	for k, li := range q.shadow.spots {
		p := &q.points[li]
		q.volumes = append(q.volumes, shadowVolume{f: FrustumOf(q.shadow.spotMats[k]), frustum: true, sphere: p.pos.Vec4(max(p.rng, 0.5))})
	}
	for _, li := range q.shadow.points {
		p := &q.points[li]
		q.volumes = append(q.volumes, shadowVolume{sphere: p.pos.Vec4(max(p.rng, 0.5))})
	}
}
