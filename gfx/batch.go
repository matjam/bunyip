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
// share both are still merged into one instanced call.
//
// A batch does not own its meshes or textures; destroy those as usual.
// Build one with NewStaticBatch and draw it with DrawBatch. Anything
// that moves belongs in DrawMesh instead: the hierarchy is built from
// the models given and is not rebuilt. Mesh geometry and bounds must also
// remain fixed; rebuild the batch after changing either. Include any
// shader displacement in mesh bounds before building the hierarchy.
type StaticBatch struct {
	items []meshDraw // prepared draws, in hierarchy order
	nodes []batchNode
	lo    lin.Vec3
	hi    lin.Vec3
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
	b := &StaticBatch{}
	for _, it := range items {
		if it.Mesh == nil {
			continue
		}
		d := meshDraw{mesh: it.Mesh, mat: it.Material, model: it.Model}
		if d.mat.BaseColor == (Color{}) {
			d.mat.BaseColor = White
		}
		if d.mat.Roughness == 0 {
			d.mat.Roughness = 0.6
		}
		d.shader = d.mat.Shader
		if d.shader == nil {
			d.shader = g.meshes.defaultShader
		} else if !d.shader.mesh {
			panic("gfx: Material.Shader wants a mesh shader from NewMeshShader")
		}
		b.items = append(b.items, d)
	}
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

// itemBox is a draw's world bounds, the box its mesh bounds fill under
// its model matrix.
func itemBox(d *meshDraw) (lo, hi lin.Vec3) {
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
// others.
func (g *Graphics) DrawBatch(b *StaticBatch) {
	if b == nil || len(b.items) == 0 {
		return
	}
	g.cur.batches = append(g.cur.batches, b)
}

// expandBatches walks each queued batch's hierarchy against the frustum
// and the occlusion buffer and appends the surviving items to the
// queue's draws, where the ordinary per-draw culling then sees them.
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
		return
	}
	if occluding {
		c := n.lo.Add(n.hi).Mul(0.5)
		if g.occ.hides(viewProj, c, n.hi.Sub(c).Len()) {
			g.stats.Culled += int(n.total)
			g.stats.Occluded += int(n.total)
			return
		}
	}
	if n.right != 0 {
		g.walkBatch(q, b, frustum, viewProj, occluding, at+1)
		g.walkBatch(q, b, frustum, viewProj, occluding, n.right)
		return
	}
	for _, d := range b.items[n.start : n.start+n.total] {
		d.uniform = d.shader.uniformOffset() // the arena moves every frame
		q.draws = append(q.draws, d)
	}
}
