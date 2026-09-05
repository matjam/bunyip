package gfx

import "github.com/matjam/bunyip/lin"

// Bounds returns the mesh's axis-aligned bounds in mesh space, the box
// culling and picking test. They come from the vertices unless SetBounds
// replaced them.
func (m *Mesh) Bounds() (min, max lin.Vec3) { return m.Min, m.Max }

// SetBounds replaces the bounds culling tests the mesh against, for a
// mesh whose drawn shape leaves its vertices: a material shader that
// displaces vertices, or an animation that swings a limb outside the
// geometry as uploaded. Give the box the drawn shape stays inside, in
// mesh space. The bounds hold until the next SetBounds; Update and
// UpdateSkinned leave them alone. A mesh that has never been given
// bounds uses the box its vertices fill, and a skinned one the boxes of
// its joints under the pose.
func (m *Mesh) SetBounds(min, max lin.Vec3) {
	m.Min, m.Max, m.boundsFixed = min, max, true
}

// setJointBounds computes one box per joint over the vertices weighted
// to it, in mesh space, so a pose's bounds are the union of those boxes
// under its joint matrices. A joint that moves no vertex gets an empty
// box, which skinBounds skips.
func (m *Mesh) setJointBounds(verts []SkinVertex) {
	count := 0
	for _, v := range verts {
		for k, w := range v.Weights {
			if w > 0 && int(v.Joints[k])+1 > count {
				count = int(v.Joints[k]) + 1
			}
		}
	}
	m.jointMin = make([]lin.Vec3, count)
	m.jointMax = make([]lin.Vec3, count)
	for j := range count {
		m.jointMin[j] = lin.V3(1, 1, 1)
		m.jointMax[j] = lin.V3(-1, -1, -1) // empty until a vertex lands here
	}
	for _, v := range verts {
		for k, w := range v.Weights {
			if w <= 0 {
				continue
			}
			j := int(v.Joints[k])
			if j >= count {
				continue
			}
			if m.jointMin[j].X > m.jointMax[j].X {
				m.jointMin[j], m.jointMax[j] = v.Pos, v.Pos
				continue
			}
			m.jointMin[j] = lin.V3(min(m.jointMin[j].X, v.Pos.X), min(m.jointMin[j].Y, v.Pos.Y), min(m.jointMin[j].Z, v.Pos.Z))
			m.jointMax[j] = lin.V3(max(m.jointMax[j].X, v.Pos.X), max(m.jointMax[j].Y, v.Pos.Y), max(m.jointMax[j].Z, v.Pos.Z))
		}
	}
}

// boxUnder maps a box through a matrix and returns the transformed
// box's centre and half extents. The extents come from the matrix's
// absolute rows, so a rotated box grows to the axis-aligned box around
// it.
func boxUnder(m lin.Mat4, lo, hi lin.Vec3) (centre, extent lin.Vec3) {
	c := lo.Add(hi).Mul(0.5)
	e := hi.Sub(lo).Mul(0.5)
	centre = m.MulPoint(c)
	extent = lin.V3(
		abs32(m.At(0, 0))*e.X+abs32(m.At(0, 1))*e.Y+abs32(m.At(0, 2))*e.Z,
		abs32(m.At(1, 0))*e.X+abs32(m.At(1, 1))*e.Y+abs32(m.At(1, 2))*e.Z,
		abs32(m.At(2, 0))*e.X+abs32(m.At(2, 1))*e.Y+abs32(m.At(2, 2))*e.Z,
	)
	return centre, extent
}

// skinBounds is a skinned draw's world bounding sphere for the pose in
// joints: each joint's box under its matrix, unioned, then placed by the
// model matrix. Every posed vertex is a weighted average of its joints'
// boxes, so the union holds the whole pose. It reports false for a mesh
// with no joint boxes, or one whose bounds were set by hand, which the
// caller then bounds by the mesh's own box.
func skinBounds(m *Mesh, model lin.Mat4, joints []lin.Mat4) (centre lin.Vec3, radius float32, ok bool) {
	if m.boundsFixed {
		return lin.Vec3{}, 0, false
	}
	n := min(len(m.jointMin), len(joints))
	var lo, hi lin.Vec3
	found := false
	for j := range n {
		bmin, bmax := m.jointMin[j], m.jointMax[j]
		if bmin.X > bmax.X {
			continue // no vertex is weighted to this joint
		}
		c, e := boxUnder(joints[j], bmin, bmax)
		l, h := c.Sub(e), c.Add(e)
		if !found {
			lo, hi, found = l, h, true
			continue
		}
		lo = lin.V3(min(lo.X, l.X), min(lo.Y, l.Y), min(lo.Z, l.Z))
		hi = lin.V3(max(hi.X, h.X), max(hi.Y, h.Y), max(hi.Z, h.Z))
	}
	if !found {
		return lin.Vec3{}, 0, false
	}
	c, e := boxUnder(model, lo, hi)
	return c, e.Len(), true
}

// shadowMask marks the draws that can reach one shadow map, so a caster
// is recorded into the cascades and spot maps its bounds fall in rather
// than into all seven. A cascade ignores its near plane, since a caster
// in front of it still writes depth; a spot light uses its whole
// frustum. Draws with a shader that may move a vertex anywhere are
// always recorded. The result is the queue's own slice, valid until the
// next call.
func (q *drawQueue) shadowMask(draws drawList, index int, spots []lin.Mat4) []bool {
	n := draws.len()
	if cap(q.shadowVis) < n {
		q.shadowVis = make([]bool, n)
	}
	vis := q.shadowVis[:n]
	cascade := index < shadowCascades
	var f Frustum
	if cascade {
		f = FrustumOf(q.cascadeMats[index])
	} else {
		f = FrustumOf(spots[index-shadowCascades])
	}
	for i := range n {
		d := draws.at(i)
		vis[i] = !d.cullable || f.containsSphere(d.centre, d.radius, cascade)
	}
	return vis
}

// drawBounds is a draw's world bounding sphere for culling, and whether
// it may be culled at all. A skinned draw uses the pose's joint boxes,
// falling back to twice the bind pose's radius when the mesh has none. A
// material shader with a vertex program grows the radius by its
// VertexBounds, and a zero VertexBounds means the draw is never culled.
func (q *drawQueue) drawBounds(d *meshDraw) (centre lin.Vec3, radius float32, cullable bool) {
	centre, radius = d.mesh.boundingSphere(d.model)
	if d.skinned && d.jointCount > 0 {
		if c, r, ok := skinBounds(d.mesh, d.model, q.joints[d.jointBase:d.jointBase+d.jointCount]); ok {
			centre, radius = c, r
		} else {
			radius *= 2 // the bind pose's bounds, loosely
		}
	}
	cullable = true
	if len(d.shader.stages) > 0 { // a vertex program moves the vertices
		if d.shader.VertexBounds <= 0 {
			cullable = false
		} else {
			radius *= 1 + d.shader.VertexBounds
		}
	}
	return centre, radius, cullable
}
