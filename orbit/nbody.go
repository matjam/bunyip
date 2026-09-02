package orbit

// Body is a mass with a position and velocity, used both as an ECS
// component and inside a Simulation. Mass zero makes a test particle
// that feels gravity without exerting it.
type Body struct {
	Pos, Vel Vec3
	Mass     float64
}

// Simulation integrates bodies under their mutual gravity with the
// leapfrog (kick-drift-kick) scheme, which conserves energy over long
// runs where simpler integrators drift.
type Simulation struct {
	Bodies    []Body
	G         float64 // zero means the real constant
	Softening float64 // metres added to distances to tame close passes
	Time      float64
	acc       []Vec3
}

func (s *Simulation) g() float64 {
	if s.G == 0 {
		return G
	}
	return s.G
}

// Accelerations fills out with the gravitational acceleration on each body.
func (s *Simulation) Accelerations(out []Vec3) {
	g := s.g()
	eps2 := s.Softening * s.Softening
	for i := range s.Bodies {
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

// Step advances every body by dt seconds.
func (s *Simulation) Step(dt float64) {
	n := len(s.Bodies)
	if cap(s.acc) < n {
		s.acc = make([]Vec3, n)
	}
	acc := s.acc[:n]
	s.Accelerations(acc)
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
}

// Energy is the total kinetic plus potential energy, a check on the
// integrator: it should stay constant.
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
