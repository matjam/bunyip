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
	return s
}

// radiance is the sky's light from a direction without the sun: the
// atmosphere's gradient above the horizon and the ground below. It
// matches skyRadiance in the mesh shader and the sky background shader.
func (s Sky) radiance(d lin.Vec3) (r, g, b float32) {
	up := d.Dot(s.Up)
	air := 1 - s.Vacuum
	if up >= 0 {
		c := lerpColor(s.Horizon, s.Zenith, float32(math.Pow(float64(up), 0.7)))
		return c.R * air, c.G * air, c.B * air
	}
	haze := Color{s.Horizon.R * air, s.Horizon.G * air, s.Horizon.B * air, 1}
	c := lerpColor(haze, s.Ground, float32(math.Pow(float64(-up), 0.5)))
	return c.R, c.G, c.B
}

// sh projects the sky's irradiance onto nine spherical harmonics, the
// same form an image environment stores.
func (s Sky) sh() [9]lin.Vec4 {
	return shProject(s.radiance, 64, 32)
}
