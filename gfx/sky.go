package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Sky is a procedural environment described by what a game already
// knows: which way is up, how much atmosphere there is, and where the
// sun is. It needs no image and costs nothing to change, so it can
// follow a ship from orbit down to the ground, or point its ground half
// at the planet a ship is passing. Set it as Light.Sky. Rough surfaces
// take its tint from every direction, metals reflect its gradient, and
// with Light.Background the sun's disc and the stars are drawn behind
// the scene. An image Environment on the light replaces it.
type Sky struct {
	// Space is a distant image environment behind the procedural sky. Its
	// radiance is attenuated by the atmosphere and adds to diffuse lighting
	// and reflections. Light.Environment, when set, takes precedence.
	Space   *Environment
	Up      lin.Vec3 // away from the ground, or from the planet below a ship; zero means +Y
	Zenith  Color    // the sky straight up, in full atmosphere; zero means Horizon
	Horizon Color    // the sky at the horizon; zero means Zenith, or the light's Ambient
	Ground  Color    // light from below: terrain, sea, the face of a planet; zero means Ambient
	// Vacuum thins the air: 0 is a full sky, 1 is space, where the sky is
	// black and the stars come out while the ground half stays.
	Vacuum  float32
	Sun     Color   // radiance of the drawn sun disc; zero means thirty times the light's colour
	SunSize float32 // the disc's angular radius in radians; zero means 0.0047, the Sun seen from Earth
	Stars   float32 // brightness of a starfield showing through thin air; zero means none
	// Atmosphere replaces Zenith and Horizon with scattered sunlight when
	// its Height is set: blue overhead, red along the horizon at sunrise
	// and sunset, dark when the sun is down, and thinning as the camera
	// climbs. Ground still colours the half below the horizon.
	Atmosphere Atmosphere

	// sun is the direction towards the sun, filled in by resolved from the
	// light. The atmosphere's colour depends on it; the gradient does not.
	sun lin.Vec3
}

// Atmosphere is the sky computed rather than described: single
// scattering of sunlight by air (Rayleigh) and by haze (Mie) through a
// shell around a planet. To use it, set Height to how deep the air is in
// world units and leave the rest at zero for Earth's air scaled to that
// depth. The sky then reddens along the horizon as the sun sets, keeps
// its blue overhead at noon, goes dark when the sun is below the
// horizon, and thins to black as Altitude climbs out of the shell, so a
// ship can fly from the ground to space with no seam. Distant geometry
// takes the same scattered light through Light.Fog's aerial
// perspective. Sky.Vacuum still scales the result, and Sky.Ground still
// lights the half below the horizon.
//
// The model is integrated into small lookup tables on the GPU, eight
// samples along each view ray and four towards the sun at each, and the
// sky and the lit meshes read the tables, so a pixel costs a few texture
// reads. The tables are built on the first frame with an atmosphere and
// again when the air changes, when the sun moves by more than about a
// twentieth of its radius, or when Altitude moves by more than a
// thousandth of Height; a rebuild costs a fraction of a millisecond of
// GPU time, and a frame that changes nothing costs none.
type Atmosphere struct {
	// Height is how deep the air is in world units. Zero means no
	// atmosphere: the sky keeps its Zenith and Horizon gradient. Density
	// falls to 1/e at a seventh of it for air and a fiftieth for haze.
	Height float32
	// PlanetRadius is the ground's distance from the planet's centre in
	// world units. It sets how far the horizon is and how long a grazing
	// ray runs through the air; zero means a hundred times Height, which
	// is Earth's proportion.
	PlanetRadius float32
	// Altitude is how far the camera is above the ground in world units.
	// Set it from the camera each frame: the air below the camera stops
	// scattering into the view as it climbs, so the sky darkens with
	// height and the horizon drops away. Zero is the ground.
	Altitude float32
	// Rayleigh is how much the air scatters per world unit at the ground,
	// by wavelength. Its zero is Earth's air scaled to Height, which is
	// what makes the sky blue and the sunset red.
	Rayleigh Color
	// Mie is how much haze scatters per world unit at the ground, the same
	// at every wavelength. It is the white glare around the sun and the
	// milkiness of a humid day; zero means Earth's haze scaled to Height.
	Mie float32
	// Forward is how strongly haze scatters along the light rather than
	// across it, 0 for even and towards 1 for a tight glare around the
	// sun; zero means 0.76.
	Forward float32
	// Intensity is the radiance of the sunlight the scattering divides up.
	// Raise it for a brighter sky without touching Light.Color; zero
	// means 22.
	Intensity float32
}

// The air's density falls to 1/e at Height divided by these, and the
// integral takes this many samples. atmoslut.frag.wgsl builds the sky's
// lookup tables with the same numbers, so the drawn sky and the ambient
// harmonics projected from Sky.radiance agree.
const (
	rayleighFalloff     = 7.5
	mieFalloff          = 50
	atmosphereViewSteps = 8
	atmosphereSunSteps  = 4
)

// resolved fills in an atmosphere's defaults. A zero Height leaves it
// off, and the zero of every other field is Earth's air scaled to the
// height given, so setting Height alone is a whole sky.
func (a Atmosphere) resolved() Atmosphere {
	if a.Height <= 0 {
		return Atmosphere{}
	}
	// Earth's coefficients are per metre through a shell 60 km deep, so
	// scaling them by that shell's size over this one keeps the optical
	// depth, and with it the colour, whatever units the game works in.
	k := 60000 / a.Height
	if a.PlanetRadius <= 0 {
		a.PlanetRadius = 100 * a.Height
	}
	if a.Rayleigh == (Color{}) {
		a.Rayleigh = Color{5.8e-6 * k, 13.5e-6 * k, 33.1e-6 * k, 1}
	}
	if a.Mie == 0 {
		a.Mie = 21e-6 * k
	}
	if a.Forward == 0 {
		a.Forward = 0.76
	}
	if a.Intensity == 0 {
		a.Intensity = 22
	}
	return a
}

// skyKey is the part of a Sky its irradiance harmonics depend on: the
// sun disc and the stars are drawn, not projected.
type skyKey struct {
	space                   *Environment
	up                      lin.Vec3
	zenith, horizon, ground Color
	vacuum                  float32
	atmos                   Atmosphere
	sun                     lin.Vec3
}

func (s Sky) key() skyKey {
	k := skyKey{up: s.Up, zenith: s.Zenith, horizon: s.Horizon, ground: s.Ground, vacuum: s.Vacuum}
	if s.Space != nil && s.Space.cube != nil {
		k.space = s.Space
	}
	if s.Atmosphere.Height > 0 {
		// Projecting an atmosphere runs the scattering integral for every
		// direction, so the key steps the two things that otherwise change
		// every frame: the altitude in 256ths of the air's depth and the
		// sun in about a degree. The drawn sky uses the exact values; only
		// the ambient light lags, by less than either step is worth.
		a := s.Atmosphere
		a.Altitude = quantise(a.Altitude, a.Height/256)
		k.atmos = a
		k.sun = lin.V3(quantise(s.sun.X, 1.0/64), quantise(s.sun.Y, 1.0/64), quantise(s.sun.Z, 1.0/64))
	}
	return k
}

// quantise rounds v down to a multiple of step.
func quantise(v, step float32) float32 {
	if step <= 0 {
		return v
	}
	return float32(math.Floor(float64(v/step))) * step
}

// resolved fills in the defaults from the light.
func (s Sky) resolved(l Light) Sky {
	if s.Up == (lin.Vec3{}) {
		s.Up = lin.V3(0, 1, 0)
	} else {
		s.Up = s.Up.Norm()
	}
	if s.Zenith == (Color{}) {
		s.Zenith = s.Horizon
	}
	if s.Horizon == (Color{}) {
		s.Horizon = s.Zenith
	}
	if s.Zenith == (Color{}) {
		s.Zenith, s.Horizon = l.Ambient, l.Ambient
	}
	if s.Ground == (Color{}) {
		s.Ground = l.Ambient
	}
	s.Vacuum = lin.Clamp(s.Vacuum, 0, 1)
	if s.Sun == (Color{}) {
		s.Sun = Color{l.Color.R * 30, l.Color.G * 30, l.Color.B * 30, 1}
	}
	if s.SunSize == 0 {
		s.SunSize = 0.0047
	}
	s.Atmosphere = s.Atmosphere.resolved()
	s.sun = l.Direction.Norm().Mul(-1)
	return s
}

// radiance is the sky's light from a direction without the sun: the
// atmosphere's scattering or gradient above the horizon and the ground
// below. It matches skyColor in the mesh shader and the sky background
// shader.
func (s Sky) proceduralRadiance(d lin.Vec3) (float32, float32, float32) {
	up := d.Dot(s.Up)
	air := 1 - s.Vacuum
	if s.Atmosphere.Height > 0 {
		// Below the horizon a camera inside the air looks the colour up
		// along the horizon instead, because its own ray meets the ground
		// at once and would leave a dark band; from above the air the ray
		// itself is integrated, so a planet seen from orbit keeps the glow
		// around its limb.
		dir := d
		if up < 0 && s.Atmosphere.Altitude < s.Atmosphere.Height {
			flat := d.Sub(s.Up.Mul(up))
			if l := flat.Len(); l > 1e-4 {
				dir = flat.Mul(1 / l)
			}
		}
		r, g, b := s.scatter(dir, 1e9)
		c := Color{r * air, g * air, b * air, 1}
		if up < 0 {
			c = lerpColor(c, s.Ground, float32(math.Pow(float64(-up), 0.5)))
		}
		return c.R, c.G, c.B
	}
	if up >= 0 {
		c := lerpColor(s.Horizon, s.Zenith, float32(math.Pow(float64(up), 0.7)))
		return c.R * air, c.G * air, c.B * air
	}
	haze := Color{s.Horizon.R * air, s.Horizon.G * air, s.Horizon.B * air, 1}
	c := lerpColor(haze, s.Ground, float32(math.Pow(float64(-up), 0.5)))
	return c.R, c.G, c.B
}

// raySphere returns where a ray from o along d crosses a sphere of
// radius r about the origin, as two distances along the ray. The first
// is greater than the second when the ray misses.
func raySphere(o, d lin.Vec3, r float32) (float32, float32) {
	b := o.Dot(d)
	c := o.Dot(o) - r*r
	h := b*b - c
	if h < 0 {
		return 1, -1
	}
	h = float32(math.Sqrt(float64(h)))
	return -b - h, -b + h
}

// scatter integrates single scattering along a ray leaving the camera in
// direction d, for at most dist world units: air and haze thinning with
// height, each sample lit by what is left of the sunlight that reached
// it and dimmed by the air back to the camera. Samples the planet
// shadows are dark, which is what makes dusk fall. The shaders draw the
// sky from lookup tables atmoslut.frag.wgsl builds with the same
// integral (scatterTerms), and the ambient harmonics are projected from
// this, so the two must stay in step.
func (s Sky) scatter(d lin.Vec3, dist float32) (float32, float32, float32) {
	a := s.Atmosphere
	ray, mie, _, _ := s.scatterTerms(d, dist, atmosphereViewSteps, atmosphereSunSteps)
	mu := d.Dot(s.sun)
	pr := phaseRayleigh(mu)
	pm := phaseMie(mu, a.Forward)
	i := a.Intensity
	return i * (ray.X*pr + mie.X*pm),
		i * (ray.Y*pr + mie.Y*pm),
		i * (ray.Z*pr + mie.Z*pm)
}

// scatterTerms is scatter without the phase functions and the sun's
// intensity, in steps samples along the ray and sunSteps towards the sun
// from each: the light air and haze scatter, each times its scattering
// coefficient, and the ray's optical depth of air and haze in world
// units. It is scatterTerms in atmoslut.frag.wgsl, which fills the
// atmosphere's sky view with eight and four steps, reading the four
// towards the sun from the transmittance table, and its aerial
// perspective with four and two.
func (s Sky) scatterTerms(d lin.Vec3, dist float32, steps, sunSteps int) (ray, mie lin.Vec3, odR, odM float32) {
	a := s.Atmosphere
	radius, height := a.PlanetRadius, a.Height
	hR, hM := height/rayleighFalloff, height/mieFalloff
	betaR := lin.V3(a.Rayleigh.R, a.Rayleigh.G, a.Rayleigh.B)
	origin := s.Up.Mul(radius + a.Altitude)
	near, far := raySphere(origin, d, radius+height)
	t0, t1 := max(near, 0), far
	if t1 <= t0 {
		return // outside the air, looking away from the planet
	}
	if g0, g1 := raySphere(origin, d, radius); g1 > 0 && g0 > 0 {
		t1 = min(t1, g0) // the ray meets the ground first
	}
	t1 = min(t1, t0+dist)
	if t1 <= t0 {
		return
	}
	step := (t1 - t0) / float32(steps)
	var sumR, sumM lin.Vec3
	for i := range steps {
		p := origin.Add(d.Mul(t0 + (float32(i)+0.5)*step))
		h := max(p.Len()-radius, 0)
		dR := exp32(-h/hR) * step
		dM := exp32(-h/hM) * step
		odR += dR
		odM += dM
		if g0, g1 := raySphere(p, s.sun, radius); g1 > 0 && g0 > 0 {
			continue // the planet stands between this sample and the sun
		}
		lodR, lodM := s.opticalDepth(p, s.sun, sunSteps)
		att := lin.V3(
			exp32(-(betaR.X*(odR+lodR) + a.Mie*1.1*(odM+lodM))),
			exp32(-(betaR.Y*(odR+lodR) + a.Mie*1.1*(odM+lodM))),
			exp32(-(betaR.Z*(odR+lodR) + a.Mie*1.1*(odM+lodM))),
		)
		sumR = sumR.Add(att.Mul(dR))
		sumM = sumM.Add(att.Mul(dM))
	}
	ray = lin.V3(sumR.X*betaR.X, sumR.Y*betaR.Y, sumR.Z*betaR.Z)
	mie = sumM.Mul(a.Mie)
	return ray, mie, odR, odM
}

// sunTint is how much of the sun's light reaches the camera through the
// air, which reddens and dims the drawn disc and its glow: the
// transmittance of four samples along the ray towards the sun. It is
// found once a frame and folded into the disc's colour in the frame
// block.
func (s Sky) sunTint() lin.Vec3 {
	a := s.Atmosphere
	_, _, odR, odM := s.scatterTerms(s.sun, 1e9, 4, 1)
	return lin.V3(
		exp32(-(a.Rayleigh.R*odR + a.Mie*1.1*odM)),
		exp32(-(a.Rayleigh.G*odR + a.Mie*1.1*odM)),
		exp32(-(a.Rayleigh.B*odR + a.Mie*1.1*odM)))
}

// opticalDepth integrates the air and the haze from p along dir to the
// top of the air in steps samples, in world units: the light a sample
// of a view ray receives from the sun. It is opticalDepth in
// atmoslut.frag.wgsl, whose transmittance table holds it for four steps
// and for eight.
func (s Sky) opticalDepth(p, dir lin.Vec3, steps int) (odR, odM float32) {
	a := s.Atmosphere
	hR, hM := a.Height/rayleighFalloff, a.Height/mieFalloff
	_, lit := raySphere(p, dir, a.PlanetRadius+a.Height)
	lightStep := max(lit, 0) / float32(steps)
	for j := range steps {
		q := p.Add(dir.Mul((float32(j) + 0.5) * lightStep))
		hj := max(q.Len()-a.PlanetRadius, 0)
		odR += exp32(-hj/hR) * lightStep
		odM += exp32(-hj/hM) * lightStep
	}
	return odR, odM
}

// phaseRayleigh is how much air scatters towards an angle whose cosine
// is mu: nearly even, a little more forwards and backwards.
func phaseRayleigh(mu float32) float32 { return 3.0 / (16 * math.Pi) * (1 + mu*mu) }

// phaseMie is the Henyey-Greenstein lobe haze scatters into, forwards by
// g, which is the glare around the sun.
func phaseMie(mu, g float32) float32 {
	g2 := g * g
	d := (2 + g2) * float32(math.Pow(float64(1+g2-2*g*mu), 1.5))
	return 3.0 / (8 * math.Pi) * ((1 - g2) * (1 + mu*mu)) / d
}

func exp32(v float32) float32 { return float32(math.Exp(float64(v))) }

// sh projects the sky's irradiance onto nine spherical harmonics, the
// same form an image environment stores. An atmosphere is sampled on a
// coarser grid than the gradient: each direction is a scattering
// integral, and the irradiance it projects to is smooth.
func (s Sky) sh() [9]lin.Vec4 {
	if s.Atmosphere.Height > 0 {
		return shProject(s.radiance, 32, 16)
	}
	return shProject(s.radiance, 64, 32)
}
