package audio

import (
	"errors"
	"math"

	"github.com/matjam/bunyip/lin"
)

// Cone directs a source's sound. Angles are full aperture angles in
// radians, 0 <= InnerAngle <= OuterAngle <= 2*pi. Gain interpolates from
// 1 inside the inner cone to OuterGain (0..1) outside the outer cone.
// The zero value is omnidirectional.
type Cone struct{ InnerAngle, OuterAngle, OuterGain float32 }

// AttenuationModel selects distance gain between a voice's distance limits.
type AttenuationModel uint8

const (
	AttenuationDefault     AttenuationModel = iota // inverse distance times linear cutoff (legacy)
	AttenuationNone                                // distance does not affect gain
	AttenuationLinear                              // linear falloff
	AttenuationInverse                             // inverse-distance falloff
	AttenuationExponential                         // power-law falloff
)

// Attenuation controls distance gain. Rolloff is nonnegative; zero means
// 1. All models except None are full volume within MinDistance and silent
// at MaxDistance. The zero value preserves the engine's original curve.
type Attenuation struct {
	Model   AttenuationModel
	Rolloff float32
}

func finite(v float32) bool     { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func finiteVec(v lin.Vec3) bool { return finite(v.X) && finite(v.Y) && finite(v.Z) }

func (c Cone) normalized() (Cone, error) {
	if !finite(c.InnerAngle) || !finite(c.OuterAngle) || !finite(c.OuterGain) || c.InnerAngle < 0 || c.OuterAngle < c.InnerAngle || c.OuterAngle > 2*math.Pi || c.OuterGain < 0 || c.OuterGain > 1 {
		return Cone{}, errors.New("audio: invalid source cone")
	}
	if c == (Cone{}) {
		return Cone{2 * math.Pi, 2 * math.Pi, 1}, nil
	}
	return c, nil
}
func (a Attenuation) normalized() (Attenuation, error) {
	if a.Model > AttenuationExponential || !finite(a.Rolloff) || a.Rolloff < 0 {
		return Attenuation{}, errors.New("audio: invalid attenuation")
	}
	if a.Rolloff == 0 {
		a.Rolloff = 1
	}
	return a, nil
}

func (a Attenuation) gain(dist, minDist, maxDist float32) float32 {
	if a.Model == AttenuationNone || dist <= minDist {
		return 1
	}
	if dist >= maxDist {
		return 0
	}
	x := (dist - minDist) / (maxDist - minDist)
	switch a.Model {
	case AttenuationLinear:
		return max(0, 1-a.Rolloff*x)
	case AttenuationInverse:
		return minDist / (minDist + a.Rolloff*(dist-minDist))
	case AttenuationExponential:
		return float32(math.Pow(float64(dist/minDist), -float64(a.Rolloff)))
	default:
		if a.Rolloff == 1 {
			return (minDist / dist) * (1 - x)
		}
		return (minDist / (minDist + a.Rolloff*(dist-minDist))) * (1 - x)
	}
}

func (c Cone) gain(direction, toward lin.Vec3) float32 {
	if c.InnerAngle >= 2*math.Pi || toward.Len() <= 1e-6 {
		return 1
	}
	cos := max(-1, min(1, direction.Norm().Dot(toward.Norm())))
	angle := float32(math.Acos(float64(cos))) * 2
	if angle <= c.InnerAngle {
		return 1
	}
	if angle >= c.OuterAngle {
		return c.OuterGain
	}
	return 1 + (c.OuterGain-1)*(angle-c.InnerAngle)/(c.OuterAngle-c.InnerAngle)
}

// SetRelativeToListener selects listener-local source coordinates: +X is
// right, +Y up, -Z forward. Position, direction and velocity use this basis;
// listener translation/velocity are added. This enables positional audio.
func (v *Voice) SetRelativeToListener(relative bool) {
	v.set(func() { v.relative = relative; v.positional = true })
}

// SetDirection enables positional audio and sets a nonzero finite direction.
// It is in world coordinates, or listener-local when relative mode is on.
func (v *Voice) SetDirection(direction lin.Vec3) error {
	if !finiteVec(direction) || direction.Len() == 0 || !finite(direction.Len()) {
		return errors.New("audio: direction must be finite and nonzero")
	}
	v.set(func() { v.direction = direction.Norm(); v.positional = true })
	return nil
}

// SetCone enables positional audio and changes directionality.
// Invalid values leave it unchanged.
func (v *Voice) SetCone(cone Cone) error {
	c, err := cone.normalized()
	if err != nil {
		return err
	}
	v.set(func() { v.cone = c; v.positional = true })
	return nil
}

// SetAttenuation enables positional audio and changes the distance model.
// Invalid values leave it unchanged.
func (v *Voice) SetAttenuation(attenuation Attenuation) error {
	a, err := attenuation.normalized()
	if err != nil {
		return err
	}
	v.set(func() { v.attenuation = a; v.positional = true })
	return nil
}

// SetDistanceRange enables positional audio with finite limits 0 < min < max.
func (v *Voice) SetDistanceRange(minDistance, maxDistance float32) error {
	if !finite(minDistance) || !finite(maxDistance) || minDistance <= 0 || maxDistance <= minDistance {
		return errors.New("audio: invalid distance range")
	}
	v.set(func() { v.minDist, v.maxDist = minDistance, maxDistance; v.positional = true })
	return nil
}

// RelativeToListener reports whether source coordinates follow the listener.
func (v *Voice) RelativeToListener() bool { v.m.mu.Lock(); defer v.m.mu.Unlock(); return v.relative }

// Direction returns the normalized cone direction in the selected coordinate space.
func (v *Voice) Direction() lin.Vec3 { v.m.mu.Lock(); defer v.m.mu.Unlock(); return v.direction }

// Cone returns the effective cone; the default has full-circle angles and unity gain.
func (v *Voice) Cone() Cone { v.m.mu.Lock(); defer v.m.mu.Unlock(); return v.cone }

// Attenuation returns the effective distance model and rolloff.
func (v *Voice) Attenuation() Attenuation { v.m.mu.Lock(); defer v.m.mu.Unlock(); return v.attenuation }

// DistanceRange returns the full-volume and silence distances.
func (v *Voice) DistanceRange() (minDistance, maxDistance float32) {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return v.minDist, v.maxDist
}

func (l Listener) localVector(p lin.Vec3) lin.Vec3 {
	f := l.Forward.Norm()
	r := f.Cross(l.Up).Norm()
	u := r.Cross(f).Norm()
	return r.Mul(p.X).Add(u.Mul(p.Y)).Sub(f.Mul(p.Z))
}

func (v *Voice) sourceSpace(l Listener) (position, velocity, direction lin.Vec3) {
	if !v.relative {
		return v.position, v.velocity, v.direction
	}
	return l.Position.Add(l.localVector(v.position)), l.Velocity.Add(l.localVector(v.velocity)), l.localVector(v.direction)
}
