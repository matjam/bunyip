package soft

import (
	"fmt"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// ClothSpec describes a rectangular sheet for NewCloth. Every zero field
// takes the default named in its comment, so ClothSpec{} is a valid
// sheet.
type ClothSpec struct {
	// Width and Height are how many particles the sheet has across and
	// down; zero means 16. Spacing is the distance between neighbours;
	// zero means 0.1.
	Width, Height int
	Spacing       float32
	// Mass is the mass of the whole sheet, shared equally; zero means 1.
	Mass float32
	// Origin is where particle 0 sits. Right and Down are the directions
	// the grid runs in; zero means +X across and -Y down, a sheet hanging
	// in the xy plane.
	Origin      lin.Vec3
	Right, Down lin.Vec3
	// Stretch is the compliance of the edges between neighbours, in
	// metres per newton; zero means 0, a sheet that does not stretch.
	// Shear is the compliance of the diagonals; zero means 0.
	Stretch, Shear float32
	// Bend is the compliance of the constraint across each pair of
	// edges in line, which resists folding; zero means 0.02, a sheet
	// that drapes. Pass a small positive value for stiffer cloth.
	Bend float32
	// Damping removes a fraction of each particle's velocity per second;
	// zero means 0.1.
	Damping float32
	// Pinned lists particle indices held in place. Index a particle with
	// y*Width+x, or with the Index method of the cloth once built.
	Pinned []int
	// Wind is the air velocity the sheet feels, in units per second.
	Wind lin.Vec3
	// Radius is how far a particle stays clear of a collider; zero means
	// a quarter of the spacing. Friction is the Coulomb coefficient
	// against colliders; zero means 0.4.
	Radius, Friction float32
	// Mask picks the collider layers the sheet collides with; zero means
	// all of them.
	Mask uint32
}

// Cloth is a sheet of particles held together by distance constraints:
// a flag, a curtain, a cape, a sail. Build one with NewCloth and spawn it
// as a component; System steps it and UpdateMesh draws it. Positions are
// world space, so a cloth needs no transform.
type Cloth struct {
	// Wind is the air velocity the sheet feels, in units per second. The
	// force on each cell is its area times the speed of the wind through
	// it, so a sheet edge-on to the wind is barely pushed.
	Wind lin.Vec3
	// Damping removes this fraction of each particle's velocity per
	// second. Zero leaves the sheet undamped, which flutters for longer.
	Damping float32
	// Stretch, Shear and Bend are the compliances of the edge, diagonal
	// and bending constraints, in metres per newton. Zero is rigid;
	// larger is softer. Changing one takes effect on the next step.
	Stretch, Shear, Bend float32
	// Radius is how far a particle stays clear of a collider, and
	// Friction the Coulomb coefficient there, combined with the
	// collider's own as a geometric mean.
	Radius, Friction float32
	// Mask picks the collider layers the sheet collides with; zero means
	// all of them.
	Mask uint32

	cols, rows int
	spacing    float32
	mass       float32 // per particle
	pos        []lin.Vec3
	prev       []lin.Vec3
	vel        []lin.Vec3
	inv        []float32
	links      []link
	lambda     []float32
	verts      []gfx.Vertex
	indices    []uint32
}

// NewCloth builds a sheet from a spec. The particles start on a flat
// grid at rest, so a cloth pinned by two corners falls into its drape
// over the first few updates.
func NewCloth(spec ClothSpec) Cloth {
	cols, rows := spec.Width, spec.Height
	if cols < 2 {
		cols = 16
	}
	if rows < 2 {
		rows = 16
	}
	spacing := spec.Spacing
	if spacing <= 0 {
		spacing = 0.1
	}
	total := spec.Mass
	if total <= 0 {
		total = 1
	}
	right, down := spec.Right, spec.Down
	if right == (lin.Vec3{}) {
		right = lin.V3(1, 0, 0)
	}
	if down == (lin.Vec3{}) {
		down = lin.V3(0, -1, 0)
	}
	right, down = right.Norm(), down.Norm()
	bend := spec.Bend
	if bend == 0 {
		bend = 0.02
	}
	damping := spec.Damping
	if damping == 0 {
		damping = 0.1
	}
	radius := spec.Radius
	if radius <= 0 {
		radius = spacing / 4
	}
	friction := spec.Friction
	if friction <= 0 {
		friction = 0.4
	}
	n := cols * rows
	c := Cloth{
		Wind: spec.Wind, Damping: damping,
		Stretch: spec.Stretch, Shear: spec.Shear, Bend: bend,
		Radius: radius, Friction: friction, Mask: spec.Mask,
		cols: cols, rows: rows, spacing: spacing, mass: total / float32(n),
		pos:  make([]lin.Vec3, n),
		prev: make([]lin.Vec3, n),
		vel:  make([]lin.Vec3, n),
		inv:  make([]float32, n),
	}
	invMass := float32(n) / total
	for y := range rows {
		for x := range cols {
			i := y*cols + x
			c.pos[i] = spec.Origin.Add(right.Mul(float32(x) * spacing)).Add(down.Mul(float32(y) * spacing))
			c.prev[i] = c.pos[i]
			c.inv[i] = invMass
		}
	}
	for _, i := range spec.Pinned {
		if i >= 0 && i < n {
			c.inv[i] = 0
		}
	}
	diag := spacing * sqrt(2)
	add := func(a, b int, rest, compliance float32) {
		c.links = append(c.links, link{a: int32(a), b: int32(b), rest: rest, compliance: compliance})
	}
	for y := range rows {
		for x := range cols {
			i := y*cols + x
			if x+1 < cols {
				add(i, i+1, spacing, spec.Stretch)
			}
			if y+1 < rows {
				add(i, i+cols, spacing, spec.Stretch)
			}
			if x+1 < cols && y+1 < rows {
				add(i, i+cols+1, diag, spec.Shear)
				add(i+1, i+cols, diag, spec.Shear)
			}
			if x+2 < cols {
				add(i, i+2, 2*spacing, bend)
			}
			if y+2 < rows {
				add(i, i+2*cols, 2*spacing, bend)
			}
		}
	}
	c.lambda = make([]float32, len(c.links))
	c.buildMesh()
	return c
}

// Size returns the particle counts across and down.
func (c *Cloth) Size() (cols, rows int) { return c.cols, c.rows }

// Index is the particle index of column x, row y.
func (c *Cloth) Index(x, y int) int { return y*c.cols + x }

// Positions returns the particle positions in world space, row by row.
// The slice is the cloth's own: read it to draw the sheet, and move a
// particle with Move rather than writing to it.
func (c *Cloth) Positions() []lin.Vec3 { return c.pos }

// Velocities returns the particle velocities, in the same order as
// Positions. The slice is the cloth's own.
func (c *Cloth) Velocities() []lin.Vec3 { return c.vel }

// Pin holds a particle where it is, so the sheet hangs from it. A
// pinned particle ignores gravity, wind and every constraint.
func (c *Cloth) Pin(i int) {
	if i >= 0 && i < len(c.inv) {
		c.inv[i] = 0
		c.vel[i] = lin.Vec3{}
	}
}

// Free lets a pinned particle fall again.
func (c *Cloth) Free(i int) {
	if i >= 0 && i < len(c.inv) {
		c.inv[i] = 1 / c.mass
	}
}

// Pinned reports whether a particle is held in place.
func (c *Cloth) Pinned(i int) bool { return i >= 0 && i < len(c.inv) && c.inv[i] == 0 }

// Move puts a particle at p without giving it velocity. Use it to carry
// a pinned corner: a flag on a moving pole, a cape on a running
// character.
func (c *Cloth) Move(i int, p lin.Vec3) {
	if i >= 0 && i < len(c.pos) {
		c.pos[i], c.prev[i] = p, p
	}
}

// Translate moves the whole sheet, pinned particles included.
func (c *Cloth) Translate(delta lin.Vec3) {
	for i := range c.pos {
		c.pos[i] = c.pos[i].Add(delta)
		c.prev[i] = c.prev[i].Add(delta)
	}
}

// step runs one substep of the solver.
func (c *Cloth) step(s *state, settings *Settings, gravity lin.Vec3, h float32, iterations int) {
	c.wind(h)
	for i := range c.pos {
		if c.inv[i] == 0 {
			c.prev[i], c.vel[i] = c.pos[i], lin.Vec3{}
			continue
		}
		c.vel[i] = c.vel[i].Add(gravity.Mul(h))
		if c.Damping > 0 {
			c.vel[i] = c.vel[i].Mul(max(0, 1-c.Damping*h))
		}
		c.prev[i] = c.pos[i]
		c.pos[i] = c.pos[i].Add(c.vel[i].Mul(h))
	}
	c.lambda = grow(c.lambda, len(c.links))
	c.applyCompliance()
	for range iterations {
		solveLinks(c.pos, c.inv, c.links, c.lambda, h)
	}
	for i := range c.pos {
		if c.inv[i] == 0 {
			continue
		}
		s.project3(&c.pos[i], c.prev[i], c.Radius, c.Friction, c.Mask, settings)
		c.vel[i] = c.pos[i].Sub(c.prev[i]).Mul(1 / h)
	}
}

// applyCompliance copies the component's compliances onto the
// constraints, so a game can stiffen a sheet between steps. The rest
// length says which kind each link is.
func (c *Cloth) applyCompliance() {
	edge, diag := c.spacing, c.spacing*sqrt(2)
	for i := range c.links {
		switch c.links[i].rest {
		case edge:
			c.links[i].compliance = c.Stretch
		case diag:
			c.links[i].compliance = c.Shear
		default:
			c.links[i].compliance = c.Bend
		}
	}
}

// wind pushes each cell of the sheet by the wind blowing through it.
func (c *Cloth) wind(h float32) {
	if c.Wind == (lin.Vec3{}) {
		return
	}
	for y := range c.rows - 1 {
		for x := range c.cols - 1 {
			a := y*c.cols + x
			b, d, e := a+1, a+c.cols, a+c.cols+1
			d1 := c.pos[e].Sub(c.pos[a])
			d2 := c.pos[d].Sub(c.pos[b])
			cross := d1.Cross(d2)
			area := cross.Len() / 2
			if area < 1e-12 {
				continue
			}
			n := cross.Mul(1 / (2 * area))
			v := c.vel[a].Add(c.vel[b]).Add(c.vel[d]).Add(c.vel[e]).Mul(0.25)
			f := n.Mul(n.Dot(c.Wind.Sub(v)) * area * 0.25)
			for _, i := range [4]int{a, b, d, e} {
				if c.inv[i] > 0 {
					c.vel[i] = c.vel[i].Add(f.Mul(c.inv[i] * h))
				}
			}
		}
	}
}

// buildMesh lays out the render vertices and indices for the grid.
func (c *Cloth) buildMesh() {
	c.verts = make([]gfx.Vertex, c.cols*c.rows)
	c.indices = make([]uint32, 0, (c.cols-1)*(c.rows-1)*6)
	for y := range c.rows {
		for x := range c.cols {
			i := y*c.cols + x
			c.verts[i] = gfx.Vertex{Pos: c.pos[i], UV: lin.V2(float32(x)/float32(c.cols-1), float32(y)/float32(c.rows-1))}
		}
	}
	for y := range c.rows - 1 {
		for x := range c.cols - 1 {
			a := uint32(y*c.cols + x)
			b, d, e := a+1, a+uint32(c.cols), a+uint32(c.cols)+1
			c.indices = append(c.indices, a, b, e, a, e, d)
		}
	}
	gfx.ComputeNormals(c.verts, c.indices)
}

// NewMesh uploads a mesh shaped like the sheet, one vertex per particle
// with UVs running across it. Call UpdateMesh each frame to follow the
// simulation, and Destroy the mesh when the game shuts down. A cloth is
// seen from both sides, so give it a material with DoubleSided set.
func (c *Cloth) NewMesh(g *gfx.Graphics) (*gfx.Mesh, error) {
	if g == nil {
		return nil, fmt.Errorf("soft: cloth needs a graphics to build a mesh")
	}
	c.writeVertices()
	return g.NewMesh(c.verts, c.indices)
}

// UpdateMesh writes the particle positions and fresh normals into a mesh
// built by NewMesh. Call it once a frame, after the world has stepped.
// The mesh keeps the cloth's own vertex slice, so a mesh built from one
// cloth must not be updated from another.
func (c *Cloth) UpdateMesh(m *gfx.Mesh) error {
	if m == nil {
		return fmt.Errorf("soft: no mesh to update")
	}
	c.writeVertices()
	return m.Update(c.verts, c.indices)
}

func (c *Cloth) writeVertices() {
	for i := range c.verts {
		c.verts[i].Pos = c.pos[i]
	}
	gfx.ComputeNormals(c.verts, c.indices)
}
