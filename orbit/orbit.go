// Package orbit computes celestial mechanics for space games. It
// provides orbital elements and state vectors, two-body propagation
// that handles circles, ellipses, parabolas and hyperbolas alike, an
// N-body integrator for planets and moons pulling on each other, and
// helpers for the numbers a player sees (periods, apsides, escape
// velocity, transfer burns). Everything is double precision, because a
// planetary system does not fit in float32. Nothing assumes our own
// solar system. Bodies are masses at positions, the gravitational
// constant is yours to set, and units are whatever you choose (metres
// and seconds with the real G, or a game's own units with G = 1).
//
// System connects the package to the entity component system. Body
// components hold positions and velocities at astronomical scale. The
// system moves them, analytically for entities on a Kepler orbit and
// numerically for free bodies under thrust and gravity, and writes
// scaled positions into gfx.Transform relative to a floating origin, so
// rendering keeps its precision at any camera position.
// Kepler components preserve their orbital phase through ECS saves and
// prefab JSON. Register the component and resource types with ecs.Register
// before saving; omit the orbit system's private query cache with
// ecs.SaveOptions.SkipUnregistered.
package orbit

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// G is the gravitational constant in SI units. Nothing here depends on
// SI: set Settings.G or Simulation.G to 1 and choose your own units of
// mass, distance and time, and every formula still holds. Real-world
// bodies live in the sol subpackage.
const G = 6.67430e-11 // m³ kg⁻¹ s⁻²

// Vec3 is a double-precision vector.
type Vec3 struct{ X, Y, Z float64 }

// V3 makes a Vec3.
func V3(x, y, z float64) Vec3 { return Vec3{x, y, z} }

// FromLin converts an engine vector.
func FromLin(v lin.Vec3) Vec3 { return Vec3{float64(v.X), float64(v.Y), float64(v.Z)} }

// Lin converts to an engine vector, losing precision.
func (a Vec3) Lin() lin.Vec3 { return lin.V3(float32(a.X), float32(a.Y), float32(a.Z)) }

// Add returns a + b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a - b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Mul scales the vector.
func (a Vec3) Mul(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Dot is the dot product.
func (a Vec3) Dot(b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross is the cross product.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// Len is the length.
func (a Vec3) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Norm returns the unit vector; zero stays zero.
func (a Vec3) Norm() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Mul(1 / l)
}

// State is a position and velocity relative to the body being orbited.
type State struct{ Pos, Vel Vec3 }

// Elements are classical orbital elements; angles are radians.
type Elements struct {
	SemiMajorAxis float64 // negative for hyperbolic orbits
	Eccentricity  float64
	Inclination   float64
	Node          float64 // longitude of the ascending node
	ArgPeriapsis  float64 // argument of periapsis
	TrueAnomaly   float64
}

// Circular makes a circular orbit of the given radius in the reference
// plane.
func Circular(radius float64) Elements { return Elements{SemiMajorAxis: radius} }

// Periapsis is the closest distance to the primary.
func (e Elements) Periapsis() float64 { return e.SemiMajorAxis * (1 - e.Eccentricity) }

// Apoapsis is the farthest distance; infinite for open orbits.
func (e Elements) Apoapsis() float64 {
	if e.Eccentricity >= 1 {
		return math.Inf(1)
	}
	return e.SemiMajorAxis * (1 + e.Eccentricity)
}

// Period is the orbital period for mu; infinite for open orbits.
func (e Elements) Period(mu float64) float64 {
	if e.SemiMajorAxis <= 0 || e.Eccentricity >= 1 {
		return math.Inf(1)
	}
	return 2 * math.Pi * math.Sqrt(e.SemiMajorAxis*e.SemiMajorAxis*e.SemiMajorAxis/mu)
}

// MeanMotion is the average angular rate, radians per second.
func (e Elements) MeanMotion(mu float64) float64 {
	return math.Sqrt(mu / math.Abs(e.SemiMajorAxis*e.SemiMajorAxis*e.SemiMajorAxis))
}

// State converts elements to a position and velocity for mu.
func (e Elements) State(mu float64) State {
	p := e.SemiMajorAxis * (1 - e.Eccentricity*e.Eccentricity)
	if e.Eccentricity == 1 {
		p = 2 * e.SemiMajorAxis // treat SemiMajorAxis as periapsis for parabolas
	}
	cv, sv := math.Cos(e.TrueAnomaly), math.Sin(e.TrueAnomaly)
	r := p / (1 + e.Eccentricity*cv)
	rp := Vec3{r * cv, r * sv, 0}
	k := math.Sqrt(mu / p)
	vp := Vec3{-k * sv, k * (e.Eccentricity + cv), 0}
	rot := func(v Vec3) Vec3 {
		// R3(Ω) R1(i) R3(ω) applied to a perifocal vector.
		cw, sw := math.Cos(e.ArgPeriapsis), math.Sin(e.ArgPeriapsis)
		ci, si := math.Cos(e.Inclination), math.Sin(e.Inclination)
		cn, sn := math.Cos(e.Node), math.Sin(e.Node)
		x := v.X*cw - v.Y*sw
		y := v.X*sw + v.Y*cw
		z := v.Z
		y, z = y*ci-z*si, y*si+z*ci
		return Vec3{x*cn - y*sn, x*sn + y*cn, z}
	}
	return State{Pos: rot(rp), Vel: rot(vp)}
}

// ElementsOf converts a state to classical elements for mu.
func ElementsOf(s State, mu float64) Elements {
	r, v := s.Pos, s.Vel
	rl := r.Len()
	h := r.Cross(v)
	n := Vec3{0, 0, 1}.Cross(h)
	ev := r.Mul(v.Dot(v) - mu/rl).Sub(v.Mul(r.Dot(v))).Mul(1 / mu)
	ecc := ev.Len()
	xi := v.Dot(v)/2 - mu/rl
	var a float64
	if math.Abs(ecc-1) > 1e-9 {
		a = -mu / (2 * xi)
	} else {
		a = h.Dot(h) / mu / 2 // periapsis distance for a parabola
	}
	out := Elements{SemiMajorAxis: a, Eccentricity: ecc}
	hl := h.Len()
	if hl > 0 {
		out.Inclination = math.Acos(clamp(h.Z/hl, -1, 1))
	}
	nl := n.Len()
	equatorial := nl < 1e-12
	circular := ecc < 1e-12
	if !equatorial {
		out.Node = math.Acos(clamp(n.X/nl, -1, 1))
		if n.Y < 0 {
			out.Node = 2*math.Pi - out.Node
		}
	}
	switch {
	case !equatorial && !circular:
		out.ArgPeriapsis = math.Acos(clamp(n.Dot(ev)/(nl*ecc), -1, 1))
		if ev.Z < 0 {
			out.ArgPeriapsis = 2*math.Pi - out.ArgPeriapsis
		}
	case equatorial && !circular:
		out.ArgPeriapsis = math.Atan2(ev.Y, ev.X)
		if h.Z < 0 {
			out.ArgPeriapsis = 2*math.Pi - out.ArgPeriapsis
		}
	}
	switch {
	case !circular:
		out.TrueAnomaly = math.Acos(clamp(ev.Dot(r)/(ecc*rl), -1, 1))
		if r.Dot(v) < 0 {
			out.TrueAnomaly = 2*math.Pi - out.TrueAnomaly
		}
	case !equatorial:
		out.TrueAnomaly = math.Acos(clamp(n.Dot(r)/(nl*rl), -1, 1))
		if r.Z < 0 {
			out.TrueAnomaly = 2*math.Pi - out.TrueAnomaly
		}
	default:
		out.TrueAnomaly = math.Atan2(r.Y, r.X)
		if h.Z < 0 {
			out.TrueAnomaly = 2*math.Pi - out.TrueAnomaly
		}
	}
	out.Node = wrap(out.Node)
	out.ArgPeriapsis = wrap(out.ArgPeriapsis)
	out.TrueAnomaly = wrap(out.TrueAnomaly)
	return out
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

func wrap(a float64) float64 {
	a = math.Mod(a, 2*math.Pi)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

// SolveKepler returns the eccentric anomaly for a mean anomaly and
// eccentricity below one.
func SolveKepler(meanAnomaly, e float64) float64 {
	m := wrap(meanAnomaly)
	E := m
	if e > 0.8 {
		E = math.Pi
	}
	for range 50 {
		f := E - e*math.Sin(E) - m
		d := 1 - e*math.Cos(E)
		step := f / d
		E -= step
		if math.Abs(step) < 1e-14 {
			break
		}
	}
	return E
}

// TrueAnomalyFromMean converts a mean anomaly on a closed orbit.
func TrueAnomalyFromMean(meanAnomaly, e float64) float64 {
	E := SolveKepler(meanAnomaly, e)
	return wrap(2 * math.Atan2(math.Sqrt(1+e)*math.Sin(E/2), math.Sqrt(1-e)*math.Cos(E/2)))
}

// MeanAnomalyFromTrue converts a true anomaly on a closed orbit.
func MeanAnomalyFromTrue(trueAnomaly, e float64) float64 {
	E := 2 * math.Atan2(math.Sqrt(1-e)*math.Sin(trueAnomaly/2), math.Sqrt(1+e)*math.Cos(trueAnomaly/2))
	return wrap(E - e*math.Sin(E))
}

// AtTime returns the elements after dt seconds on a closed orbit.
func (e Elements) AtTime(mu, dt float64) Elements {
	if e.Eccentricity >= 1 {
		s := Propagate(e.State(mu), mu, dt)
		return ElementsOf(s, mu)
	}
	m := MeanAnomalyFromTrue(e.TrueAnomaly, e.Eccentricity) + e.MeanMotion(mu)*dt
	out := e
	out.TrueAnomaly = TrueAnomalyFromMean(m, e.Eccentricity)
	return out
}

// Propagate advances a state by dt seconds under two-body gravity using
// the universal variable formulation, which is exact for every conic.
func Propagate(s State, mu, dt float64) State {
	if dt == 0 {
		return s
	}
	r0 := s.Pos.Len()
	v0 := s.Vel.Len()
	rdotv := s.Pos.Dot(s.Vel)
	sqmu := math.Sqrt(mu)
	alpha := -v0*v0/mu + 2/r0 // 1 / semi-major axis
	var chi float64
	switch {
	case alpha > 1e-12: // ellipse
		chi = sqmu * dt * alpha
	case math.Abs(alpha) <= 1e-12: // parabola
		h := s.Pos.Cross(s.Vel)
		p := h.Dot(h) / mu
		sv := 0.5 * math.Atan(1/(3*math.Sqrt(mu/(p*p*p))*dt))
		wv := math.Atan(math.Cbrt(math.Tan(sv)))
		chi = math.Sqrt(p) * 2 / math.Tan(2*wv)
	default: // hyperbola
		a := 1 / alpha
		sgn := 1.0
		if dt < 0 {
			sgn = -1
		}
		num := -2 * mu * alpha * dt
		den := rdotv + sgn*math.Sqrt(-mu*a)*(1-r0*alpha)
		chi = sgn * math.Sqrt(-a) * math.Log(num/den)
	}
	var r, c2, c3, psi float64
	for range 100 {
		psi = chi * chi * alpha
		c2, c3 = stumpff(psi)
		r = chi*chi*c2 + rdotv/sqmu*chi*(1-psi*c3) + r0*(1-psi*c2)
		next := chi + (sqmu*dt-chi*chi*chi*c3-rdotv/sqmu*chi*chi*c2-r0*chi*(1-psi*c3))/r
		if math.Abs(next-chi) < 1e-10 {
			chi = next
			break
		}
		chi = next
	}
	psi = chi * chi * alpha
	c2, c3 = stumpff(psi)
	r = chi*chi*c2 + rdotv/sqmu*chi*(1-psi*c3) + r0*(1-psi*c2)
	f := 1 - chi*chi/r0*c2
	g := dt - chi*chi*chi/sqmu*c3
	gdot := 1 - chi*chi/r*c2
	fdot := sqmu / (r * r0) * chi * (psi*c3 - 1)
	return State{Pos: s.Pos.Mul(f).Add(s.Vel.Mul(g)), Vel: s.Pos.Mul(fdot).Add(s.Vel.Mul(gdot))}
}

// stumpff returns the c2 and c3 functions of psi.
func stumpff(psi float64) (c2, c3 float64) {
	switch {
	case psi > 1e-6:
		sq := math.Sqrt(psi)
		return (1 - math.Cos(sq)) / psi, (sq - math.Sin(sq)) / (sq * sq * sq)
	case psi < -1e-6:
		sq := math.Sqrt(-psi)
		return (1 - math.Cosh(sq)) / psi, (math.Sinh(sq) - sq) / (sq * sq * sq)
	}
	return 0.5, 1.0 / 6
}

// Energy is the specific orbital energy; negative for bound orbits.
func Energy(s State, mu float64) float64 { return s.Vel.Dot(s.Vel)/2 - mu/s.Pos.Len() }

// CircularVelocity is the speed of a circular orbit at radius r.
func CircularVelocity(mu, r float64) float64 { return math.Sqrt(mu / r) }

// EscapeVelocity is the speed needed to leave from radius r.
func EscapeVelocity(mu, r float64) float64 { return math.Sqrt(2 * mu / r) }

// VisViva is the speed at radius r on an orbit of semi-major axis a.
func VisViva(mu, r, a float64) float64 { return math.Sqrt(mu * (2/r - 1/a)) }

// Hohmann plans the two-burn transfer between circular orbits of radii
// r1 and r2: the delta-v of each burn and the coast time between them.
func Hohmann(mu, r1, r2 float64) (dv1, dv2, coast float64) {
	at := (r1 + r2) / 2
	dv1 = math.Abs(VisViva(mu, r1, at) - CircularVelocity(mu, r1))
	dv2 = math.Abs(CircularVelocity(mu, r2) - VisViva(mu, r2, at))
	coast = math.Pi * math.Sqrt(at*at*at/mu)
	return
}

// SphereOfInfluence is the radius within which a body of mass m orbiting
// a primary of mass M at distance a dominates gravity.
func SphereOfInfluence(a, m, M float64) float64 { return a * math.Pow(m/M, 0.4) }
