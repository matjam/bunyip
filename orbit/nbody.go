package orbit

import (
	"math"
	"runtime"
	"sync"
)

// Body is a mass with a position and velocity, used both as an ECS
// component and inside a Simulation. In a Simulation, Mass zero makes
// a test particle that feels gravity without exerting it. In the ECS,
// Ship or Kepler selects motion; a Body with neither stays fixed.
type Body struct {
	Pos, Vel Vec3
	Mass     float64
}

// Simulation integrates bodies under their mutual gravity with the
// leapfrog (kick-drift-kick) scheme. Use a sufficiently small fixed step
// to limit integration error; energy is not conserved exactly. The zero
// value is an empty simulation with the SI gravitational constant.
type Simulation struct {
	Bodies    []Body
	G         float64 // zero means the real constant
	Softening float64 // simulation distance: its square is added to squared separations
	Time      float64 // elapsed simulation time, advanced by Step
	acc       *accCache
}

// accCache holds the accelerations the last Step ended with, which are
// what the next Step starts with, and the bodies, G and Softening they
// were computed from, so the next Step reuses them only when nothing
// they depend on has changed. It sits behind a pointer so that copies of
// a Simulation that share it always see a consistent set.
type accCache struct {
	acc   []Vec3
	from  []Body
	g     float64
	soft  float64
	valid bool
}

// parallelBodies is the body count from which Accelerations splits the
// bodies across goroutines. Below it the goroutines cost more than they
// save.
const parallelBodies = 256

func (s *Simulation) g() float64 {
	if s.G == 0 {
		return G
	}
	return s.G
}

// Accelerations fills out with the gravitational acceleration on each body.
// out must have length at least len(Bodies). Positive Softening avoids
// the singularity when distinct bodies occupy the same position. From
// 256 bodies it shares the bodies out across goroutines, one range per
// processor; each body's sum still runs over the others in order, so the
// result is the same as on one goroutine. Do not change Bodies while it
// runs.
func (s *Simulation) Accelerations(out []Vec3) {
	n := len(s.Bodies)
	workers := min(runtime.GOMAXPROCS(0), n/(parallelBodies/4))
	if n < parallelBodies || workers < 2 {
		s.accelRange(out, 0, n)
		return
	}
	var wg sync.WaitGroup
	per := (n + workers - 1) / workers
	for lo := 0; lo < n; lo += per {
		hi := min(lo+per, n)
		wg.Go(func() { s.accelRange(out, lo, hi) })
	}
	wg.Wait()
}

// accelRange fills out[lo:hi] with the accelerations of those bodies.
func (s *Simulation) accelRange(out []Vec3, lo, hi int) {
	g := s.g()
	eps2 := s.Softening * s.Softening
	for i := lo; i < hi; i++ {
		var a Vec3
		pi := s.Bodies[i].Pos
		for j := range s.Bodies {
			if i == j || s.Bodies[j].Mass == 0 {
				continue
			}
			d := s.Bodies[j].Pos.Sub(pi)
			r2 := d.Dot(d) + eps2
			inv := g * s.Bodies[j].Mass / (r2 * sqrt(r2))
			a = a.Add(d.Mul(inv))
		}
		out[i] = a
	}
}

// FieldAt is the gravitational acceleration the bodies produce at p.
func (s *Simulation) FieldAt(p Vec3) Vec3 {
	g := s.g()
	eps2 := s.Softening * s.Softening
	var a Vec3
	for _, b := range s.Bodies {
		if b.Mass == 0 {
			continue
		}
		d := b.Pos.Sub(p)
		r2 := d.Dot(d) + eps2
		if r2 == 0 {
			continue
		}
		a = a.Add(d.Mul(g * b.Mass / (r2 * sqrt(r2))))
	}
	return a
}

// Step advances every body and Time by dt simulation time units. The
// accelerations it ends with are the ones the next Step starts with, so
// it computes them once per step, not twice. Changing a body's position
// or mass, adding or removing bodies, or changing G or Softening between
// steps is noticed, and the next Step computes them afresh.
func (s *Simulation) Step(dt float64) {
	if s.acc == nil {
		s.acc = &accCache{}
	}
	c := s.acc
	n := len(s.Bodies)
	if cap(c.acc) < n {
		c.acc, c.valid = make([]Vec3, n), false
	}
	acc := c.acc[:n]
	if !s.accCurrent(c) {
		s.Accelerations(acc)
	}
	for i := range s.Bodies {
		b := &s.Bodies[i]
		b.Vel = b.Vel.Add(acc[i].Mul(dt / 2))
		b.Pos = b.Pos.Add(b.Vel.Mul(dt))
	}
	s.Accelerations(acc)
	for i := range s.Bodies {
		b := &s.Bodies[i]
		b.Vel = b.Vel.Add(acc[i].Mul(dt / 2))
	}
	s.Time += dt
	c.from = append(c.from[:0], s.Bodies...)
	c.g, c.soft, c.valid = s.G, s.Softening, true
}

// accCurrent reports whether c still holds the accelerations of the
// bodies as they are: computed from the same positions, masses, G and
// Softening, compared bit for bit.
func (s *Simulation) accCurrent(c *accCache) bool {
	if !c.valid || len(c.from) != len(s.Bodies) || !sameBits(c.g, s.G) || !sameBits(c.soft, s.Softening) {
		return false
	}
	for i := range s.Bodies {
		b, was := &s.Bodies[i], &c.from[i]
		if !sameBits(b.Pos.X, was.Pos.X) || !sameBits(b.Pos.Y, was.Pos.Y) || !sameBits(b.Pos.Z, was.Pos.Z) || !sameBits(b.Mass, was.Mass) {
			return false
		}
	}
	return true
}

func sameBits(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

// Energy returns total kinetic plus unsoftened Newtonian potential energy.
// It ignores Softening and omits potential terms for coincident bodies,
// so it is an integration diagnostic for separated, unsoftened systems.
func (s *Simulation) Energy() float64 {
	g := s.g()
	var e float64
	for i, b := range s.Bodies {
		e += 0.5 * b.Mass * b.Vel.Dot(b.Vel)
		for j := i + 1; j < len(s.Bodies); j++ {
			o := s.Bodies[j]
			if d := o.Pos.Sub(b.Pos).Len(); d > 0 {
				e -= g * b.Mass * o.Mass / d
			}
		}
	}
	return e
}

// Barycenter is the centre of mass.
func (s *Simulation) Barycenter() Vec3 {
	var sum Vec3
	var mass float64
	for _, b := range s.Bodies {
		sum = sum.Add(b.Pos.Mul(b.Mass))
		mass += b.Mass
	}
	if mass == 0 {
		return sum
	}
	return sum.Mul(1 / mass)
}

// RK4 advances a particle by dt in an acceleration field, a fourth-order
// step suited to spacecraft under thrust plus gravity.
func RK4(pos, vel Vec3, dt float64, accel func(pos, vel Vec3) Vec3) (Vec3, Vec3) {
	k1v := accel(pos, vel)
	k1p := vel
	k2v := accel(pos.Add(k1p.Mul(dt/2)), vel.Add(k1v.Mul(dt/2)))
	k2p := vel.Add(k1v.Mul(dt / 2))
	k3v := accel(pos.Add(k2p.Mul(dt/2)), vel.Add(k2v.Mul(dt/2)))
	k3p := vel.Add(k2v.Mul(dt / 2))
	k4v := accel(pos.Add(k3p.Mul(dt)), vel.Add(k3v.Mul(dt)))
	k4p := vel.Add(k3v.Mul(dt))
	pos = pos.Add(k1p.Add(k2p.Mul(2)).Add(k3p.Mul(2)).Add(k4p).Mul(dt / 6))
	vel = vel.Add(k1v.Add(k2v.Mul(2)).Add(k3v.Mul(2)).Add(k4v).Mul(dt / 6))
	return pos, vel
}

func sqrt(v float64) float64 {
	// math.Sqrt through a local name keeps the hot loop readable.
	return sqrtFloat(v)
}
