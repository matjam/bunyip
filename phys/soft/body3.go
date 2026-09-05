package soft

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// SoftBody3Spec describes a volumetric soft body for NewSoftBody3. Every
// zero field takes the default named in its comment.
type SoftBody3Spec struct {
	// Vertices and Indices are a closed triangle mesh, as gfx.CubeMesh,
	// gfx.SphereMesh and gfx.TorusMesh return them, or as a glTF model
	// gives them. Vertices that share a position become one particle, so
	// a mesh split for flat shading or for UV seams still simulates as
	// one skin.
	Vertices []gfx.Vertex
	Indices  []uint32
	// Position moves the mesh into the world, and Scale sizes it; zero
	// scale means 1.
	Position lin.Vec3
	Scale    float32
	// Mass is the mass of the whole body, shared equally between the
	// particles; zero means 1.
	Mass float32
	// Compliance is the give in the constraints along the surface edges,
	// in metres per newton; zero means 0.0005, a firm jelly. Larger is
	// softer. To request rigid surface constraints, set the returned body's
	// Compliance to zero after construction.
	Compliance float32
	// VolumeCompliance is the give in the constraint that holds the
	// enclosed volume; zero means 0, which holds the volume as hard as
	// the solver can. Pressure is the volume the body aims for as a
	// multiple of the volume it was built with; zero means 1, and above
	// 1 inflates it.
	VolumeCompliance, Pressure float32
	// ShapeMatch pulls the body back toward its original shape, rotated
	// to where it is now: 0 leaves it a bag of constraints, 1 makes it
	// nearly rigid. Zero means 0.1.
	ShapeMatch float32
	// Damping removes a fraction of each particle's velocity per second;
	// zero means 0.5, which settles a dropped body quickly.
	Damping float32
	// Radius is how far a particle stays clear of a collider; zero means
	// a fiftieth of the mesh's size. Friction is the Coulomb coefficient
	// against colliders; zero means 0.5.
	Radius, Friction float32
	// Mask picks the collider layers the body collides with; zero means
	// all of them.
	Mask uint32
}

// SoftBody3 is a closed mesh simulated as a skin of particles that keeps
// its volume: a jelly cube, a beach ball, a lump of dough. Build one with
// NewSoftBody3 and spawn it as a component; System steps it and
// UpdateMesh updates its render mesh, which the game draws separately.
// Positions are world space, so a soft body needs no transform. Copies
// share private particle storage; construct each independent instance.
type SoftBody3 struct {
	// Compliance is the give in the surface constraints and
	// VolumeCompliance the give in the volume constraint. Surface compliance
	// is metres per newton with SI units; volume compliance has different
	// dimensions because the constrained quantity is volume. Zero is rigid;
	// larger is softer.
	Compliance, VolumeCompliance float32
	// Pressure is the volume the body aims for as a multiple of its rest
	// volume. Raise it above 1 to inflate the body, lower it to deflate.
	Pressure float32
	// ShapeMatch is how strongly the body returns to its original shape
	// each substep, from 0 (never) to 1 (at once).
	ShapeMatch float32
	// Damping removes this fraction of each particle's velocity per
	// second.
	Damping float32
	// Radius is how far a particle stays clear of a collider, and
	// Friction the Coulomb coefficient there, combined with the
	// collider's own as a geometric mean.
	Radius, Friction float32
	// Mask picks the collider layers the body collides with; zero means
	// all of them.
	Mask uint32

	pos     []lin.Vec3
	prev    []lin.Vec3
	vel     []lin.Vec3
	inv     []float32
	rest    []lin.Vec3 // rest offsets from the rest centre, for shape matching
	links   []link
	lambda  []float32
	tris    [][3]int32
	restVol float32
	mass    float32 // whole body
	verts   []gfx.Vertex
	indices []uint32
	owner   []int32 // particle behind each render vertex
}

// NewSoftBody3 builds a soft body from a closed mesh. Vertices closer
// than a thousandth of the mesh's size become one particle, the edges of
// the triangles become distance constraints, and the triangles together
// give the volume the body holds on to.
func NewSoftBody3(spec SoftBody3Spec) SoftBody3 {
	scale := spec.Scale
	if scale == 0 {
		scale = 1
	}
	mass := spec.Mass
	if mass <= 0 {
		mass = 1
	}
	compliance := spec.Compliance
	if compliance == 0 {
		compliance = 0.0005
	}
	pressure := spec.Pressure
	if pressure == 0 {
		pressure = 1
	}
	shapeMatch := spec.ShapeMatch
	if shapeMatch == 0 {
		shapeMatch = 0.1
	}
	damping := spec.Damping
	if damping == 0 {
		damping = 0.5
	}
	friction := spec.Friction
	if friction <= 0 {
		friction = 0.5
	}
	b := SoftBody3{
		Compliance: compliance, VolumeCompliance: spec.VolumeCompliance, Pressure: pressure,
		ShapeMatch: shapeMatch, Damping: damping, Friction: friction, Mask: spec.Mask,
		mass:    mass,
		verts:   append([]gfx.Vertex(nil), spec.Vertices...),
		indices: append([]uint32(nil), spec.Indices...),
	}
	if len(b.verts) == 0 || len(b.indices) < 3 {
		return b
	}
	// Place the render vertices, then weld them into particles.
	lo, hi := b.verts[0].Pos, b.verts[0].Pos
	for i := range b.verts {
		p := b.verts[i].Pos.Mul(scale).Add(spec.Position)
		b.verts[i].Pos = p
		lo, hi = lo.Min(p), hi.Max(p)
	}
	size := hi.Sub(lo).Len()
	b.owner = make([]int32, len(b.verts))
	weld := max(size*1e-3, 1e-6)
	type cell struct {
		x, y, z int64
	}
	seen := make(map[cell]int32, len(b.verts))
	for i := range b.verts {
		p := b.verts[i].Pos
		key := cell{int64(math.Round(float64(p.X / weld))), int64(math.Round(float64(p.Y / weld))), int64(math.Round(float64(p.Z / weld)))}
		if j, ok := seen[key]; ok {
			b.owner[i] = j
			continue
		}
		j := int32(len(b.pos))
		seen[key] = j
		b.owner[i] = j
		b.pos = append(b.pos, p)
	}
	n := len(b.pos)
	b.prev = append([]lin.Vec3(nil), b.pos...)
	b.vel = make([]lin.Vec3, n)
	b.inv = make([]float32, n)
	for i := range b.inv {
		b.inv[i] = float32(n) / mass
	}
	radius := spec.Radius
	if radius <= 0 {
		radius = max(size/50, 1e-4)
	}
	b.Radius = radius
	// Triangles and their edges, each edge once.
	type edge struct{ a, b int32 }
	edges := make(map[edge]bool, len(b.indices))
	for i := 0; i+2 < len(b.indices); i += 3 {
		t := [3]int32{b.owner[b.indices[i]], b.owner[b.indices[i+1]], b.owner[b.indices[i+2]]}
		if t[0] == t[1] || t[1] == t[2] || t[0] == t[2] {
			continue // a degenerate triangle contributes no volume
		}
		b.tris = append(b.tris, t)
		for k := range 3 {
			x, y := t[k], t[(k+1)%3]
			if x > y {
				x, y = y, x
			}
			if edges[edge{x, y}] {
				continue
			}
			edges[edge{x, y}] = true
			b.links = append(b.links, link{a: x, b: y, rest: b.pos[x].Sub(b.pos[y]).Len(), compliance: compliance})
		}
	}
	b.lambda = make([]float32, len(b.links))
	b.restVol = b.volume()
	// Rest offsets for shape matching, about the rest centre.
	centre := b.Center()
	b.rest = make([]lin.Vec3, n)
	for i := range b.pos {
		b.rest[i] = b.pos[i].Sub(centre)
	}
	return b
}

// Particles returns the particle positions in world space. The slice is
// the body's own; move the body with Translate rather than writing to it.
func (b *SoftBody3) Particles() []lin.Vec3 { return b.pos }

// Velocities returns the particle velocities, in the same order as
// Particles. The slice is the body's own.
func (b *SoftBody3) Velocities() []lin.Vec3 { return b.vel }

// Center is the average of the particle positions, which is the body's
// centre of mass because every particle weighs the same.
func (b *SoftBody3) Center() lin.Vec3 {
	if len(b.pos) == 0 {
		return lin.Vec3{}
	}
	var sum lin.Vec3
	for _, p := range b.pos {
		sum = sum.Add(p)
	}
	return sum.Mul(1 / float32(len(b.pos)))
}

// Translate moves the whole body without changing its shape or velocity.
func (b *SoftBody3) Translate(delta lin.Vec3) {
	for i := range b.pos {
		b.pos[i] = b.pos[i].Add(delta)
		b.prev[i] = b.prev[i].Add(delta)
	}
}

// AddImpulse changes the whole body's velocity by impulse divided by its
// mass, as a hit or a kick does. To push one part of it, add to the
// velocities of the particles there instead.
func (b *SoftBody3) AddImpulse(impulse lin.Vec3) {
	if b.mass <= 0 {
		return
	}
	dv := impulse.Mul(1 / b.mass)
	for i := range b.vel {
		b.vel[i] = b.vel[i].Add(dv)
	}
}

// Mass is the mass of the whole body.
func (b *SoftBody3) Mass() float32 { return b.mass }

// RestVolume is the volume the mesh enclosed when the body was built.
func (b *SoftBody3) RestVolume() float32 { return b.restVol }

// Volume is the volume the mesh encloses now. A body resting under
// gravity keeps it within a few percent of RestVolume times Pressure.
func (b *SoftBody3) Volume() float32 { return b.volume() }

// volume is the signed volume of the closed mesh, by the divergence
// theorem over its triangles.
func (b *SoftBody3) volume() float32 {
	var v float32
	for _, t := range b.tris {
		p0, p1, p2 := b.pos[t[0]], b.pos[t[1]], b.pos[t[2]]
		v += p0.Cross(p1).Dot(p2)
	}
	return v / 6
}

// step runs one substep of the solver.
func (b *SoftBody3) step(s *state, settings *Settings, gravity lin.Vec3, h float32, iterations int) {
	if len(b.pos) == 0 {
		return
	}
	for i := range b.pos {
		if b.inv[i] == 0 {
			b.prev[i], b.vel[i] = b.pos[i], lin.Vec3{}
			continue
		}
		b.vel[i] = b.vel[i].Add(gravity.Mul(h))
		if b.Damping > 0 {
			b.vel[i] = b.vel[i].Mul(max(0, 1-b.Damping*h))
		}
		b.prev[i] = b.pos[i]
		b.pos[i] = b.pos[i].Add(b.vel[i].Mul(h))
	}
	b.lambda = grow(b.lambda, len(b.links)+1)
	for i := range b.links {
		b.links[i].compliance = b.Compliance
	}
	for range iterations {
		solveLinks(b.pos, b.inv, b.links, b.lambda, h)
		b.solveVolume(s, h, len(b.links))
	}
	b.matchShape()
	for i := range b.pos {
		if b.inv[i] == 0 {
			continue
		}
		s.project3(&b.pos[i], b.prev[i], b.Radius, b.Friction, b.Mask, settings)
		b.vel[i] = b.pos[i].Sub(b.prev[i]).Mul(1 / h)
	}
}

// solveVolume projects the one constraint that holds the enclosed volume
// at Pressure times the rest volume.
func (b *SoftBody3) solveVolume(s *state, h float32, slot int) {
	if len(b.tris) == 0 || b.restVol == 0 {
		return
	}
	if cap(s.grads) < len(b.pos) {
		s.grads = make([]lin.Vec3, len(b.pos))
	}
	grads := s.grads[:len(b.pos)]
	clear(grads)
	for _, t := range b.tris {
		p0, p1, p2 := b.pos[t[0]], b.pos[t[1]], b.pos[t[2]]
		grads[t[0]] = grads[t[0]].Add(p1.Cross(p2))
		grads[t[1]] = grads[t[1]].Add(p2.Cross(p0))
		grads[t[2]] = grads[t[2]].Add(p0.Cross(p1))
	}
	var w float32
	for i := range grads {
		grads[i] = grads[i].Mul(1.0 / 6)
		w += b.inv[i] * grads[i].Dot(grads[i])
	}
	if w == 0 {
		return
	}
	c := b.volume() - b.restVol*b.Pressure
	alpha := b.VolumeCompliance / (h * h)
	dl := (-c - alpha*b.lambda[slot]) / (w + alpha)
	b.lambda[slot] += dl
	for i := range b.pos {
		if b.inv[i] > 0 {
			b.pos[i] = b.pos[i].Add(grads[i].Mul(dl * b.inv[i]))
		}
	}
}

// matchShape pulls the particles toward the rest shape, rotated to the
// pose that fits them best. It is what stops a soft body collapsing into
// a puddle when the constraints alone cannot hold it up.
func (b *SoftBody3) matchShape() {
	if b.ShapeMatch <= 0 || len(b.rest) != len(b.pos) {
		return
	}
	centre := b.Center()
	var apq m3
	for i := range b.pos {
		p := b.pos[i].Sub(centre)
		q := b.rest[i]
		apq[0] += p.X * q.X
		apq[1] += p.X * q.Y
		apq[2] += p.X * q.Z
		apq[3] += p.Y * q.X
		apq[4] += p.Y * q.Y
		apq[5] += p.Y * q.Z
		apq[6] += p.Z * q.X
		apq[7] += p.Z * q.Y
		apq[8] += p.Z * q.Z
	}
	r, ok := apq.polar()
	if !ok {
		return
	}
	t := min(b.ShapeMatch, 1)
	for i := range b.pos {
		if b.inv[i] == 0 {
			continue
		}
		goal := r.mulVec(b.rest[i]).Add(centre)
		b.pos[i] = b.pos[i].Lerp(goal, t)
	}
}

// NewMesh uploads the body's mesh. Call UpdateMesh each frame to follow
// the simulation, and Destroy the mesh when the game shuts down.
func (b *SoftBody3) NewMesh(g *gfx.Graphics) (*gfx.Mesh, error) {
	if g == nil {
		return nil, fmt.Errorf("soft: a soft body needs a graphics to build a mesh")
	}
	b.writeVertices()
	return g.NewMesh(b.verts, b.indices)
}

// UpdateMesh writes the particle positions and fresh normals into a mesh
// built by NewMesh. Call it once a frame, after the world has stepped.
// The mesh keeps the body's own vertex slice, so a mesh built from one
// body must not be updated from another.
func (b *SoftBody3) UpdateMesh(m *gfx.Mesh) error {
	if m == nil {
		return fmt.Errorf("soft: no mesh to update")
	}
	b.writeVertices()
	return m.Update(b.verts, b.indices)
}

func (b *SoftBody3) writeVertices() {
	for i := range b.verts {
		b.verts[i].Pos = b.pos[b.owner[i]]
	}
	gfx.ComputeNormals(b.verts, b.indices)
}

// m3 is a row-major 3x3 matrix, for shape matching.
type m3 [9]float32

func (m m3) mulVec(v lin.Vec3) lin.Vec3 {
	return lin.V3(m[0]*v.X+m[1]*v.Y+m[2]*v.Z, m[3]*v.X+m[4]*v.Y+m[5]*v.Z, m[6]*v.X+m[7]*v.Y+m[8]*v.Z)
}

func (m m3) transpose() m3 {
	return m3{m[0], m[3], m[6], m[1], m[4], m[7], m[2], m[5], m[8]}
}

func (m m3) det() float32 {
	return m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
}

func (m m3) inverse() (m3, bool) {
	d := m.det()
	if float32(math.Abs(float64(d))) < 1e-12 {
		return m3{}, false
	}
	s := 1 / d
	return m3{
		(m[4]*m[8] - m[5]*m[7]) * s, (m[2]*m[7] - m[1]*m[8]) * s, (m[1]*m[5] - m[2]*m[4]) * s,
		(m[5]*m[6] - m[3]*m[8]) * s, (m[0]*m[8] - m[2]*m[6]) * s, (m[2]*m[3] - m[0]*m[5]) * s,
		(m[3]*m[7] - m[4]*m[6]) * s, (m[1]*m[6] - m[0]*m[7]) * s, (m[0]*m[4] - m[1]*m[3]) * s,
	}, true
}

// polar returns the rotation part of the matrix, by averaging it with
// its own inverse transpose until it stops changing. A matrix that has
// been flattened or turned inside out has no rotation to find.
func (m m3) polar() (m3, bool) {
	if m.det() <= 0 {
		return m3{}, false
	}
	r := m
	for range 16 {
		inv, ok := r.inverse()
		if !ok {
			return m3{}, false
		}
		it := inv.transpose()
		var next m3
		var diff float32
		for i := range r {
			next[i] = 0.5 * (r[i] + it[i])
			diff += float32(math.Abs(float64(next[i] - r[i])))
		}
		r = next
		if diff < 1e-6 {
			break
		}
	}
	return r, true
}
