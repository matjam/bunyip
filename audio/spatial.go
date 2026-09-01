package audio

import "github.com/matjam/bunyip/lin"

// Listener is where positional voices are heard from. Forward and Up
// orient it; the right ear lies along Forward × Up.
type Listener struct {
	Position, Forward, Up lin.Vec3
}

// SetListener places the listener, usually at the camera each frame.
func (m *Mixer) SetListener(l Listener) {
	if l.Forward.Len() == 0 {
		l.Forward = lin.Vec3{Z: -1}
	}
	if l.Up.Len() == 0 {
		l.Up = lin.Vec3{Y: 1}
	}
	m.mu.Lock()
	m.listener = l
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
