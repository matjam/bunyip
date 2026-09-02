package audio

import "github.com/matjam/bunyip/lin"

// Listener is where positional voices are heard from. Forward and Up
// orient it; the right ear lies along Forward × Up. Velocity, in world
// units per second, only matters once Doppler is on (see SetDoppler).
type Listener struct {
	Position, Forward, Up lin.Vec3
	Velocity              lin.Vec3
}

// SetListener places the listener, usually at the camera each frame.
// Moving it also decides which ReverbZone, if any, the listener is in.
func (m *Mixer) SetListener(l Listener) {
	if l.Forward.Len() == 0 {
		l.Forward = lin.Vec3{Z: -1}
	}
	if l.Up.Len() == 0 {
		l.Up = lin.Vec3{Y: 1}
	}
	m.mu.Lock()
	m.listener = l
	if len(m.zones) > 0 {
		m.updateReverb()
	}
	m.mu.Unlock()
}

// SetListener2D places the listener for a 2D game whose y axis points
// down the screen: sounds to the right on screen come from the right.
func (m *Mixer) SetListener2D(x, y float32) {
	m.SetListener(Listener{Position: lin.Vec3{X: x, Y: y}, Forward: lin.Vec3{Z: 1}, Up: lin.Vec3{Y: -1}})
}

// Listener returns the current listener.
func (m *Mixer) Listener() Listener {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listener
}

// SetDoppler turns the Doppler effect on for positional sounds: a source
// closing on the listener plays sharp, one receding plays flat, by how
// fast each moves along the line between them. The factor scales the
// effect; 1 is physical, 0 (the default) is off. Speeds come from
// Listener.Velocity and Voice.SetVelocity, in world units per second,
// against the speed of sound (see SetSpeedOfSound). Streams have no
// pitch, so Doppler leaves them alone.
func (m *Mixer) SetDoppler(factor float32) {
	m.mu.Lock()
	m.doppler = max(factor, 0)
	m.mu.Unlock()
}

// SetSpeedOfSound sets the speed Doppler works against, in the game's
// world units per second. The default is 343, right for metres; a game
// in pixels or larger units raises it to keep the effect subtle.
func (m *Mixer) SetSpeedOfSound(c float32) {
	m.mu.Lock()
	if c > 0 {
		m.speedOfSound = c
	}
	m.mu.Unlock()
}

// attenuate returns the gain and pan for a source at p: unity within
// minDist, falling by inverse distance and reaching silence at maxDist;
// panned by the direction's component along the listener's right.
func (l Listener) attenuate(p lin.Vec3, minDist, maxDist float32) (gain, pan float32) {
	d := p.Sub(l.Position)
	dist := d.Len()
	if dist <= 1e-6 {
		return 1, 0
	}
	right := l.Forward.Cross(l.Up).Norm()
	pan = d.Mul(1/dist).Dot(right) * 0.8 // never fully silence an ear
	if dist < minDist {
		return 1, pan * dist / minDist
	}
	if dist >= maxDist {
		return 0, pan
	}
	gain = (minDist / dist) * (1 - (dist-minDist)/(maxDist-minDist))
	return gain, pan
}

// doppler returns the pitch ratio for a source at p moving at vel, given
// the Doppler factor and the speed of sound c. Speeds along the line
// between listener and source are clamped short of c, so a source
// outrunning its own sound is merely very flat rather than silent.
func (l Listener) doppler(p, vel lin.Vec3, factor, c float32) float32 {
	d := p.Sub(l.Position)
	dist := d.Len()
	if dist <= 1e-6 || c <= 0 || factor <= 0 {
		return 1
	}
	dir := d.Mul(1 / dist)
	const limit = 0.9
	vl := max(l.Velocity.Dot(dir)*factor, -c*limit) // listener toward the source
	vs := min(-vel.Dot(dir)*factor, c*limit)        // source toward the listener
	return (c + vl) / (c - vs)
}
