package phys

import "github.com/matjam/bunyip/lin"

// aabbTree is a dynamic bounding volume hierarchy over boxes, which the
// world queries search instead of visiting every collider. Each leaf
// holds one item's box grown by a margin, so an item that moves a little
// keeps its leaf and one that moves further is taken out and put back.
// Insertion descends to the sibling that grows the tree's surface least,
// and rotations on the way back up keep the tree balanced, so a query
// reaches each item it finds through about log n nodes.
//
// The 2D queries use the same tree with every box flat at z zero.
//
// A query returns items in the order the tree holds them, which depends
// on the history of insertions. Callers that need a stable order sort
// what the tree returns.
type aabbTree struct {
	nodes []treeNode
	root  int32
	free  int32 // first free node, linked through parent
	stack []int32
}

// treeNode is one box of the tree. A leaf has no children and carries
// the item it stands for; a free node has height -1.
type treeNode struct {
	lo, hi      lin.Vec3
	parent      int32
	left, right int32
	height      int32
	item        int32
}

const nullNode = -1

func (t *aabbTree) leaf(n int32) bool { return t.nodes[n].left == nullNode }

// alloc takes a node from the free list or grows the node slice.
func (t *aabbTree) alloc() int32 {
	if len(t.nodes) == 0 {
		t.root, t.free = nullNode, nullNode
	}
	if t.free != nullNode {
		n := t.free
		t.free = t.nodes[n].parent
		t.nodes[n] = treeNode{parent: nullNode, left: nullNode, right: nullNode}
		return n
	}
	t.nodes = append(t.nodes, treeNode{parent: nullNode, left: nullNode, right: nullNode})
	return int32(len(t.nodes) - 1)
}

func (t *aabbTree) release(n int32) {
	t.nodes[n] = treeNode{parent: t.free, left: nullNode, right: nullNode, height: -1}
	t.free = n
}

// fatten grows a box by a twentieth of its longest side, the slack that
// lets an item move a little without leaving its leaf.
func fatten(lo, hi lin.Vec3) (lin.Vec3, lin.Vec3) {
	e := hi.Sub(lo)
	m := 0.05 * max(e.X, e.Y, e.Z)
	if !(m > 0) {
		return lo, hi
	}
	d := lin.V3(m, m, m)
	return lo.Sub(d), hi.Add(d)
}

// contains reports whether the box lo, hi lies within the node's box.
func (n *treeNode) contains(lo, hi lin.Vec3) bool {
	return n.lo.X <= lo.X && n.lo.Y <= lo.Y && n.lo.Z <= lo.Z && hi.X <= n.hi.X && hi.Y <= n.hi.Y && hi.Z <= n.hi.Z
}

// area is half the surface of a box, the cost insertion minimises.
func area(lo, hi lin.Vec3) float32 {
	e := hi.Sub(lo)
	return e.X*e.Y + e.Y*e.Z + e.Z*e.X
}

// insert adds an item with the given box and returns its leaf.
func (t *aabbTree) insert(item int32, lo, hi lin.Vec3) int32 {
	n := t.alloc()
	node := &t.nodes[n]
	node.lo, node.hi = fatten(lo, hi)
	node.item = item
	t.insertLeaf(n)
	return n
}

// move updates an item's box. The leaf is only taken out and put back
// when the box has left the grown box the leaf holds; the leaf keeps its
// number either way.
func (t *aabbTree) move(n int32, lo, hi lin.Vec3) {
	if t.nodes[n].contains(lo, hi) {
		return
	}
	t.removeLeaf(n)
	t.nodes[n].lo, t.nodes[n].hi = fatten(lo, hi)
	t.insertLeaf(n)
}

// remove takes an item's leaf out of the tree and frees it.
func (t *aabbTree) remove(n int32) {
	t.removeLeaf(n)
	t.release(n)
}

func (t *aabbTree) insertLeaf(n int32) {
	if t.root == nullNode {
		t.root = n
		t.nodes[n].parent = nullNode
		return
	}
	lo, hi := t.nodes[n].lo, t.nodes[n].hi
	// Descend to the sibling that costs least: a new parent over a node
	// costs the combined box's area, and every ancestor it passes grows
	// by what the leaf adds to it.
	i := t.root
	for !t.leaf(i) {
		node := &t.nodes[i]
		a := area(node.lo, node.hi)
		combined := area(node.lo.Min(lo), node.hi.Max(hi))
		cost := 2 * combined
		inherit := 2 * (combined - a)
		childCost := func(c int32) float32 {
			cn := &t.nodes[c]
			grown := area(cn.lo.Min(lo), cn.hi.Max(hi))
			if t.leaf(c) {
				return grown + inherit
			}
			return grown - area(cn.lo, cn.hi) + inherit
		}
		c1, c2 := childCost(node.left), childCost(node.right)
		if cost < c1 && cost < c2 {
			break
		}
		if c1 < c2 {
			i = node.left
		} else {
			i = node.right
		}
	}
	sibling := i
	oldParent := t.nodes[sibling].parent
	p := t.alloc()
	sib := &t.nodes[sibling]
	t.nodes[p].parent = oldParent
	t.nodes[p].lo, t.nodes[p].hi = sib.lo.Min(lo), sib.hi.Max(hi)
	t.nodes[p].height = sib.height + 1
	t.nodes[p].left, t.nodes[p].right = sibling, n
	if oldParent != nullNode {
		if t.nodes[oldParent].left == sibling {
			t.nodes[oldParent].left = p
		} else {
			t.nodes[oldParent].right = p
		}
	} else {
		t.root = p
	}
	t.nodes[sibling].parent = p
	t.nodes[n].parent = p
	t.refit(p)
}

// refit walks from a node to the root, balancing each ancestor and
// recomputing its box and height.
func (t *aabbTree) refit(i int32) {
	for i != nullNode {
		i = t.balance(i)
		node := &t.nodes[i]
		l, r := &t.nodes[node.left], &t.nodes[node.right]
		node.height = 1 + max(l.height, r.height)
		node.lo, node.hi = l.lo.Min(r.lo), l.hi.Max(r.hi)
		i = node.parent
	}
}

func (t *aabbTree) removeLeaf(n int32) {
	if n == t.root {
		t.root = nullNode
		return
	}
	parent := t.nodes[n].parent
	grand := t.nodes[parent].parent
	sibling := t.nodes[parent].left
	if sibling == n {
		sibling = t.nodes[parent].right
	}
	if grand != nullNode {
		if t.nodes[grand].left == parent {
			t.nodes[grand].left = sibling
		} else {
			t.nodes[grand].right = sibling
		}
		t.nodes[sibling].parent = grand
		t.release(parent)
		t.refit(grand)
	} else {
		t.root = sibling
		t.nodes[sibling].parent = nullNode
		t.release(parent)
	}
	t.nodes[n].parent = nullNode
}

// balance rotates the subtree at a when one child is two or more levels
// taller than the other, and returns the subtree's new root.
func (t *aabbTree) balance(a int32) int32 {
	A := &t.nodes[a]
	if t.leaf(a) || A.height < 2 {
		return a
	}
	b, c := A.left, A.right
	B, C := &t.nodes[b], &t.nodes[c]
	switch bal := C.height - B.height; {
	case bal > 1:
		return t.rotate(a, c, b, true)
	case bal < -1:
		return t.rotate(a, b, c, false)
	}
	return a
}

// rotate lifts the taller child up over a. up is that child, other the
// shorter one, and upIsRight says which side of a the tall child was on.
func (t *aabbTree) rotate(a, up, other int32, upIsRight bool) int32 {
	A, U := &t.nodes[a], &t.nodes[up]
	f, g := U.left, U.right
	F, G := &t.nodes[f], &t.nodes[g]
	// The tall child takes a's place under a's parent.
	U.left = a
	U.parent = A.parent
	A.parent = up
	if U.parent != nullNode {
		P := &t.nodes[U.parent]
		if P.left == a {
			P.left = up
		} else {
			P.right = up
		}
	} else {
		t.root = up
	}
	O := &t.nodes[other]
	// The taller grandchild stays under up; the shorter one moves under a.
	keep, give := f, g
	K, Gv := F, G
	if F.height < G.height {
		keep, give = g, f
		K, Gv = G, F
	}
	U.right = keep
	if upIsRight {
		A.right = give
	} else {
		A.left = give
	}
	Gv.parent = a
	A.lo, A.hi = O.lo.Min(Gv.lo), O.hi.Max(Gv.hi)
	U.lo, U.hi = A.lo.Min(K.lo), A.hi.Max(K.hi)
	A.height = 1 + max(O.height, Gv.height)
	U.height = 1 + max(A.height, K.height)
	return up
}

// overlap appends every item whose leaf box overlaps lo, hi to dst.
func (t *aabbTree) overlap(dst []int32, lo, hi lin.Vec3) []int32 {
	if len(t.nodes) == 0 || t.root == nullNode {
		return dst
	}
	stack := append(t.stack[:0], t.root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := &t.nodes[n]
		if node.lo.X > hi.X || lo.X > node.hi.X || node.lo.Y > hi.Y || lo.Y > node.hi.Y || node.lo.Z > hi.Z || lo.Z > node.hi.Z {
			continue
		}
		if node.left == nullNode {
			dst = append(dst, node.item)
			continue
		}
		stack = append(stack, node.left, node.right)
	}
	t.stack = stack
	return dst
}

// ray appends every item whose leaf box the segment from origin to
// origin+dir passes through to dst.
func (t *aabbTree) ray(dst []int32, origin, dir lin.Vec3) []int32 {
	if len(t.nodes) == 0 || t.root == nullNode {
		return dst
	}
	stack := append(t.stack[:0], t.root)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := &t.nodes[n]
		tmin, tmax := float32(0), float32(1)
		if !slabAxis(origin.X, dir.X, node.lo.X, node.hi.X, &tmin, &tmax) ||
			!slabAxis(origin.Y, dir.Y, node.lo.Y, node.hi.Y, &tmin, &tmax) ||
			!slabAxis(origin.Z, dir.Z, node.lo.Z, node.hi.Z, &tmin, &tmax) {
			continue
		}
		if node.left == nullNode {
			dst = append(dst, node.item)
			continue
		}
		stack = append(stack, node.left, node.right)
	}
	t.stack = stack
	return dst
}
