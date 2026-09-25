package gfx

import "github.com/matjam/bunyip/lin"

func (s Sky) radiance(d lin.Vec3) (float32, float32, float32) {
	r, g, b := s.proceduralRadiance(d)
	if env := s.Space; env != nil && env.cube != nil && env.ambient != nil {
		x, y, z := env.ambient.sample(d)
		tr := s.spaceTransmittance(d).Mul(env.scale)
		r += x * tr.X
		g += y * tr.Y
		b += z * tr.Z
	}
	return r, g, b
}

// spaceTransmittance is the optical depth between the camera and infinity.
// Unlike atmospheric scattering it requires no samples toward the sun.
// The shaders read the same eight-step integral from the atmosphere's
// transmittance table (spaceTransmittance in the ATMOSPHERE LOOKUP
// block of prelude_mesh.wgsl and skyparam.frag.wgsl).
func (s Sky) spaceTransmittance(d lin.Vec3) lin.Vec3 {
	if s.Atmosphere.Height <= 0 {
		visible := float32(1)
		if d.Dot(s.Up) < 0 {
			visible = s.Vacuum
		}
		return lin.V3(visible, visible, visible)
	}
	a := s.Atmosphere
	origin := s.Up.Mul(a.PlanetRadius + a.Altitude)
	if near, far := raySphere(origin, d, a.PlanetRadius); far > 0 && near >= 0 {
		return lin.Vec3{}
	}
	near, far := raySphere(origin, d, a.PlanetRadius+a.Height)
	start := max(0, near)
	if far <= start {
		return lin.V3(1, 1, 1)
	}
	step := (far - start) / atmosphereViewSteps
	var odR, odM float32
	for i := range atmosphereViewSteps {
		p := origin.Add(d.Mul(start + (float32(i)+.5)*step))
		h := max(p.Len()-a.PlanetRadius, 0)
		odR += exp32(-h/(a.Height/rayleighFalloff)) * step
		odM += exp32(-h/(a.Height/mieFalloff)) * step
	}
	air := 1 - s.Vacuum
	return lin.V3(1-air+air*exp32(-(a.Rayleigh.R*odR+a.Mie*1.1*odM)),
		1-air+air*exp32(-(a.Rayleigh.G*odR+a.Mie*1.1*odM)),
		1-air+air*exp32(-(a.Rayleigh.B*odR+a.Mie*1.1*odM)))
}
