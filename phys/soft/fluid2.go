package soft

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Fluid2Spec describes a body of liquid for NewFluid2. Every zero field
// takes the default named in its comment.
type Fluid2Spec struct {
	// Bounds is the tank the liquid is kept in, in view units. An empty
	// rectangle leaves it unbounded, held only by the colliders it meets.
	Bounds lin.Rect
	// Spacing is how far apart the particles sit at rest; zero means 8.
	// It sets the density the fluid settles at, so a change to it is a
	// change to the fluid.
	Spacing float32
	// Radius is how far a particle feels its neighbours; zero means two
	// and a half times the spacing, which gives each particle about
	// twenty neighbours. Larger is smoother and slower.
	Radius float32
	// Substeps is how many solves the fluid takes per update; zero means
	// 1, which is what position-based fluids are meant to run at. Raise
	// it for a fast, thin fluid that must not step past a wall, and
	// expect more jitter in return. The Substeps in the world Settings
	// belongs to cloth and soft bodies and does not reach a fluid.
	Substeps int
	// Viscosity is how fast a particle is pulled toward the velocity of
	// its neighbours, as a rate per second: 0 leaves the fluid free to
	// churn, 15 is water that settles, 60 is syrup. Zero means 15.
	Viscosity float32
	// SurfaceTension is the strength of the small push particles give
	// each other at close range, which stops them clumping into strings
	// and rounds off drops; zero means 0.1.
	SurfaceTension float32
	// Relaxation softens the density solve, so a particle with few
	// neighbours is not thrown across the tank; zero means 0.05.
	Relaxation float32
	// Damping removes a fraction of each particle's velocity per second;
	// zero means 0.1.
	Damping float32
	// Friction is the Coulomb coefficient against the tank walls and the
	// colliders; zero means 0.1, wet and slippery.
	Friction float32
	// Mask picks the collider layers the fluid collides with; zero means
	// all of them.
	Mask uint32
}

// Fluid2 is a body of liquid in the plane, simulated as particles: a
// tank of water, a wave, a splash of blood. Build one with NewFluid2,
// fill it with Fill or Add, and spawn it as a component; System steps it.
// The game draws the particles itself, from Positions, as sprites or
// circles. Positions are view units, with +Y down as the screen runs.
type Fluid2 struct {
	// Bounds is the tank, in view units. Particles are kept inside it.
	// An empty rectangle leaves the fluid unbounded.
	Bounds lin.Rect
	// Substeps is how many solves the fluid takes per update, at least 1.
	Substeps int
	// Viscosity, SurfaceTension and Relaxation tune the solve, as
	// Fluid2Spec describes them. Damping removes this fraction of each
	// particle's velocity per second, and Friction is the Coulomb
	// coefficient against walls and colliders.
	Viscosity, SurfaceTension, Relaxation float32
	Damping, Friction                     float32
	// Mask picks the collider layers the fluid collides with; zero means
	// all of them.
	Mask uint32

	radius  float32
	spacing float32
	rest    float32
	pos     []lin.Vec2
	prev    []lin.Vec2
	vel     []lin.Vec2
	delta   []lin.Vec2
	dens    []float32
	lambda  []float32
	// The spatial hash: buckets of particle indices, rebuilt each
	// substep, and the neighbour list gathered from it.
	bucket   []int32
	items    []int32
	cursor   []int32
	cells    []cell2
	nbrStart []int32
	nbr      []int32
}

// cell2 is a particle's cell in the spatial hash.
type cell2 struct{ x, y int32 }

// NewFluid2 builds an empty fluid. Fill it with Fill or Add.
func NewFluid2(spec Fluid2Spec) Fluid2 {
	spacing := spec.Spacing
	if spacing <= 0 {
		spacing = 8
	}
	radius := spec.Radius
	if radius <= 0 {
		radius = 2.5 * spacing
	}
	viscosity := spec.Viscosity
	if viscosity == 0 {
		viscosity = 15
	}
	tension := spec.SurfaceTension
	if tension == 0 {
		tension = 0.02
	}
	relaxation := spec.Relaxation
	if relaxation == 0 {
		relaxation = 0.05
	}
	damping := spec.Damping
	if damping == 0 {
		damping = 0.1
	}
	friction := spec.Friction
	if friction == 0 {
		friction = 0.1
	}
	f := Fluid2{
		Bounds: spec.Bounds, Substeps: max(spec.Substeps, 1), Viscosity: viscosity, SurfaceTension: tension,
		Relaxation: relaxation, Damping: damping, Friction: friction, Mask: spec.Mask,
		radius: radius, spacing: spacing,
	}
	f.rest = restDensity(radius, spacing)
	return f
}

// restDensity is the density of a square lattice of the given spacing,
// which is the density the fluid is solved toward.
func restDensity(radius, spacing float32) float32 {
	reach := int(math.Ceil(float64(radius / spacing)))
	var rho float32
	for y := -reach; y <= reach; y++ {
		for x := -reach; x <= reach; x++ {
			d := lin.V2(float32(x)*spacing, float32(y)*spacing).Len()
			rho += poly6(d, radius)
		}
	}
	return rho
}

// poly6 is the smoothing kernel, normalised over the disc of radius h.
func poly6(r, h float32) float32 {
	if r >= h {
		return 0
	}
	d := h*h - r*r
	return 4 / (float32(math.Pi) * pow8(h)) * d * d * d
}

// spikyGrad is the gradient of the spiky kernel at distance r, as a
// magnitude to scale the unit vector from the neighbour to the particle.
func spikyGrad(r, h float32) float32 {
	if r >= h || r <= 0 {
		return 0
	}
	d := h - r
	return -30 / (float32(math.Pi) * pow5(h)) * d * d
}

func pow5(h float32) float32 { return h * h * h * h * h }
func pow8(h float32) float32 { x := h * h * h * h; return x * x }

// Spacing is the distance between particles at rest.
func (f *Fluid2) Spacing() float32 { return f.spacing }

// Radius is how far a particle feels its neighbours.
func (f *Fluid2) Radius() float32 { return f.radius }

// RestDensity is the density the solver holds the fluid at.
func (f *Fluid2) RestDensity() float32 { return f.rest }

// Count is how many particles the fluid has.
func (f *Fluid2) Count() int { return len(f.pos) }

// Positions returns the particle positions in view units. The slice is
// the fluid's own: read it to draw the liquid, and add particles with
// Add rather than writing to it.
func (f *Fluid2) Positions() []lin.Vec2 { return f.pos }

// Velocities returns the particle velocities, in the same order as
// Positions. The slice is the fluid's own.
func (f *Fluid2) Velocities() []lin.Vec2 { return f.vel }

// Density is the density around particle i, as the last step measured
// it. Compare it with RestDensity to colour a splash by how packed it
// is, or to find the surface, where the density falls away.
func (f *Fluid2) Density(i int) float32 {
	if i < 0 || i >= len(f.dens) {
		return 0
	}
	return f.dens[i]
}

// Add puts one particle at p, at rest.
func (f *Fluid2) Add(p lin.Vec2) {
	f.pos = append(f.pos, p)
	f.prev = append(f.prev, p)
	f.vel = append(f.vel, lin.Vec2{})
	f.delta = append(f.delta, lin.Vec2{})
	f.dens = append(f.dens, 0)
	f.lambda = append(f.lambda, 0)
}

// Fill adds particles on a lattice of the fluid's spacing covering the
// rectangle, which is how a tank or a column of water starts. Every
// second row is offset by half a spacing, the packing a settled liquid
// finds anyway.
func (f *Fluid2) Fill(r lin.Rect) {
	if r.Empty() {
		return
	}
	rows := int(r.H / f.spacing)
	cols := int(r.W / f.spacing)
	for y := range rows {
		for x := range cols {
			p := lin.V2(r.X+(float32(x)+0.5)*f.spacing, r.Y+(float32(y)+0.5)*f.spacing)
			if y%2 == 1 {
				p.X += f.spacing / 2
				if p.X > r.X+r.W {
					continue
				}
			}
			f.Add(p)
		}
	}
}

// Clear removes every particle.
func (f *Fluid2) Clear() {
	f.pos, f.prev, f.vel = f.pos[:0], f.prev[:0], f.vel[:0]
	f.delta, f.dens, f.lambda = f.delta[:0], f.dens[:0], f.lambda[:0]
}

// step runs one substep of position-based fluids.
func (f *Fluid2) step(s *state, settings *Settings, gravity lin.Vec2, h float32, iterations int) {
	n := len(f.pos)
	if n == 0 {
		return
	}
	for i := range f.pos {
		f.vel[i] = f.vel[i].Add(gravity.Mul(h))
		if f.Damping > 0 {
			f.vel[i] = f.vel[i].Mul(max(0, 1-f.Damping*h))
		}
		f.prev[i] = f.pos[i]
		f.pos[i] = f.pos[i].Add(f.vel[i].Mul(h))
	}
	f.neighbours()
	eps := f.Relaxation / (f.radius * f.radius)
	wq := poly6(0.2*f.radius, f.radius)
	for range iterations {
		f.densities()
		for i := range f.pos {
			c := f.dens[i]/f.rest - 1
			var sumSq, sx, sy float32
			for _, j := range f.neighboursOf(i) {
				d := f.pos[i].Sub(f.pos[int(j)])
				r := d.Len()
				g := spikyGrad(r, f.radius)
				if g == 0 {
					continue
				}
				gx, gy := d.X/r*g/f.rest, d.Y/r*g/f.rest
				sx, sy = sx+gx, sy+gy
				sumSq += gx*gx + gy*gy
			}
			sumSq += sx*sx + sy*sy
			f.lambda[i] = -c / (sumSq + eps)
		}
		for i := range f.pos {
			var d lin.Vec2
			for _, jj := range f.neighboursOf(i) {
				j := int(jj)
				diff := f.pos[i].Sub(f.pos[j])
				r := diff.Len()
				g := spikyGrad(r, f.radius)
				if g == 0 {
					continue
				}
				corr := float32(0)
				if wq > 0 && f.SurfaceTension > 0 {
					q := poly6(r, f.radius) / wq
					corr = -f.SurfaceTension * q * q * q * q
				}
				d = d.Add(diff.Mul((f.lambda[i] + f.lambda[j] + corr) * g / (r * f.rest)))
			}
			f.delta[i] = d
		}
		for i := range f.pos {
			f.pos[i] = f.pos[i].Add(f.delta[i])
			f.bound(s, i, settings)
		}
	}
	for i := range f.pos {
		f.vel[i] = f.pos[i].Sub(f.prev[i]).Mul(1 / h)
	}
	f.viscosity(h)
}

// bound keeps one particle inside the tank and out of the colliders.
func (f *Fluid2) bound(s *state, i int, settings *Settings) {
	r := f.spacing / 2
	if !f.Bounds.Empty() {
		p, prev := f.pos[i], f.prev[i]
		lo, hi := f.Bounds.Min(), f.Bounds.Max()
		if p.X < lo.X+r {
			p.X = lo.X + r
			frictionSlide2(&p, prev, lin.V2(1, 0), r, f.Friction)
		} else if p.X > hi.X-r {
			p.X = hi.X - r
			frictionSlide2(&p, prev, lin.V2(-1, 0), r, f.Friction)
		}
		if p.Y < lo.Y+r {
			p.Y = lo.Y + r
			frictionSlide2(&p, prev, lin.V2(0, 1), r, f.Friction)
		} else if p.Y > hi.Y-r {
			p.Y = hi.Y - r
			frictionSlide2(&p, prev, lin.V2(0, -1), r, f.Friction)
		}
		f.pos[i] = p
	}
	s.project2(&f.pos[i], f.prev[i], r, f.Friction, f.Mask)
}

// substeps is how many solves the fluid takes per update, at least one.
func (f *Fluid2) substeps() int { return max(f.Substeps, 1) }

// viscosity pulls each particle toward the average velocity of its
// neighbours, which is what makes a thick liquid move as one. The pull
// is a rate per second, so the substep count does not change it.
func (f *Fluid2) viscosity(h float32) {
	if f.Viscosity <= 0 {
		return
	}
	rate := min(f.Viscosity*h, 1)
	for i := range f.pos {
		var sum lin.Vec2
		for _, jj := range f.neighboursOf(i) {
			j := int(jj)
			w := poly6(f.pos[i].Sub(f.pos[j]).Len(), f.radius)
			sum = sum.Add(f.vel[j].Sub(f.vel[i]).Mul(w))
		}
		f.delta[i] = sum.Mul(rate / f.rest)
	}
	for i := range f.pos {
		f.vel[i] = f.vel[i].Add(f.delta[i])
	}
}

// densities measures the density around every particle.
func (f *Fluid2) densities() {
	self := poly6(0, f.radius)
	for i := range f.pos {
		rho := self
		for _, j := range f.neighboursOf(i) {
			rho += poly6(f.pos[i].Sub(f.pos[int(j)]).Len(), f.radius)
		}
		f.dens[i] = rho
	}
}

// neighboursOf returns the neighbours gathered for particle i.
func (f *Fluid2) neighboursOf(i int) []int32 {
	return f.nbr[f.nbrStart[i]:f.nbrStart[i+1]]
}

// hashCell mixes a cell coordinate into a bucket index.
func hashCell(x, y int32, mask uint32) uint32 {
	h := uint32(x)*0x9e3779b1 ^ uint32(y)*0x85ebca6b
	h ^= h >> 15
	h *= 0x2545f491
	return (h ^ h>>13) & mask
}

// neighbours rebuilds the spatial hash and the neighbour lists. Cells
// are the size of the smoothing radius, so the nine cells around a
// particle hold everything within reach.
func (f *Fluid2) neighbours() {
	n := len(f.pos)
	buckets := 64
	for buckets < 2*n {
		buckets *= 2
	}
	mask := uint32(buckets - 1)
	if cap(f.bucket) < buckets+1 {
		f.bucket = make([]int32, buckets+1)
	}
	f.bucket = f.bucket[:buckets+1]
	clear(f.bucket)
	if cap(f.cells) < n {
		f.cells = make([]cell2, n)
	}
	f.cells = f.cells[:n]
	inv := 1 / f.radius
	for i := range f.pos {
		f.cells[i] = cell2{int32(math.Floor(float64(f.pos[i].X * inv))), int32(math.Floor(float64(f.pos[i].Y * inv)))}
		f.bucket[hashCell(f.cells[i].x, f.cells[i].y, mask)+1]++
	}
	for i := 1; i <= buckets; i++ {
		f.bucket[i] += f.bucket[i-1]
	}
	if cap(f.items) < n {
		f.items = make([]int32, n)
	}
	f.items = f.items[:n]
	if cap(f.cursor) < buckets {
		f.cursor = make([]int32, buckets)
	}
	f.cursor = f.cursor[:buckets]
	copy(f.cursor, f.bucket[:buckets])
	for i := range f.pos {
		b := hashCell(f.cells[i].x, f.cells[i].y, mask)
		f.items[f.cursor[b]] = int32(i)
		f.cursor[b]++
	}
	if cap(f.nbr) == 0 {
		f.nbr = make([]int32, 0, n*16)
	}
	f.nbr = f.nbr[:0]
	if cap(f.nbrStart) < n+1 {
		f.nbrStart = make([]int32, n+1)
	}
	f.nbrStart = f.nbrStart[:n+1]
	h2 := f.radius * f.radius
	for i := range f.pos {
		f.nbrStart[i] = int32(len(f.nbr))
		c := f.cells[i]
		for dy := int32(-1); dy <= 1; dy++ {
			for dx := int32(-1); dx <= 1; dx++ {
				want := cell2{c.x + dx, c.y + dy}
				b := hashCell(want.x, want.y, mask)
				for k := f.bucket[b]; k < f.bucket[b+1]; k++ {
					j := f.items[k]
					// Two cells can share a bucket, so check the cell as
					// well; without it a neighbour could be counted twice.
					if int(j) == i || f.cells[j] != want {
						continue
					}
					d := f.pos[i].Sub(f.pos[j])
					if d.Dot(d) < h2 {
						f.nbr = append(f.nbr, j)
					}
				}
			}
		}
	}
	f.nbrStart[n] = int32(len(f.nbr))
}
