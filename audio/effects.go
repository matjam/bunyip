package audio

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// lowPass is a second-order Butterworth low-pass filter on stereo frames.
type lowPass struct {
	b0, b1, b2, a1, a2 float32
	z                  [2][2]float32 // per-channel state, transposed direct form II
}

func newLowPass(cutoff float32, rate int) *lowPass {
	f := &lowPass{}
	f.set(cutoff, rate)
	return f
}

func (f *lowPass) set(cutoff float32, rate int) {
	cutoff = min(cutoff, float32(rate)*0.49)
	w0 := 2 * math.Pi * float64(cutoff) / float64(rate)
	cs, sn := math.Cos(w0), math.Sin(w0)
	alpha := sn / (2 * math.Sqrt2 / 2)
	a0 := 1 + alpha
	f.b0 = float32((1 - cs) / 2 / a0)
	f.b1 = float32((1 - cs) / a0)
	f.b2 = f.b0
	f.a1 = float32(-2 * cs / a0)
	f.a2 = float32((1 - alpha) / a0)
}

func (f *lowPass) process(buf []float32) {
	for i := 0; i+1 < len(buf); i += 2 {
		for c := range 2 {
			x := buf[i+c]
			y := f.b0*x + f.z[c][0]
			f.z[c][0] = f.b1*x - f.a1*y + f.z[c][1]
			f.z[c][1] = f.b2*x - f.a2*y
			buf[i+c] = y
		}
	}
}

// ReverbSettings describe a reverb, which voices feed through their Reverb
// send. Zero RoomSize, Damping and Width take the defaults 0.5, 0.5 and 1;
// a zero Wet turns the reverb off, so the zero value is no reverb.
type ReverbSettings struct {
	RoomSize float32 // 0..1, how long the tail rings
	Damping  float32 // 0..1, how quickly highs die away
	Width    float32 // 0..1, stereo spread
	Wet      float32 // 0..1, level of the reverb in the mix
}

// withDefaults fills in the zero fields, so settings compare and blend
// on what the reverb will use.
func (s ReverbSettings) withDefaults() ReverbSettings {
	if s.RoomSize == 0 {
		s.RoomSize = 0.5
	}
	if s.Damping == 0 {
		s.Damping = 0.5
	}
	if s.Width == 0 {
		s.Width = 1
	}
	s.Wet = max(s.Wet, 0)
	return s
}

// lerp blends toward o by t.
func (s ReverbSettings) lerp(o ReverbSettings, t float32) ReverbSettings {
	return ReverbSettings{
		RoomSize: s.RoomSize + (o.RoomSize-s.RoomSize)*t,
		Damping:  s.Damping + (o.Damping-s.Damping)*t,
		Width:    s.Width + (o.Width-s.Width)*t,
		Wet:      s.Wet + (o.Wet-s.Wet)*t,
	}
}

// SetReverb configures the mixer's shared reverb: the room the listener
// is in when no ReverbZone says otherwise. Voices with a Reverb send add
// their signal to it (unless their bus has a reverb of its own); the tail
// is mixed on top of the dry output. The zero value turns it off.
func (m *Mixer) SetReverb(s ReverbSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseReverb = s
	m.updateReverb()
}

// Reverb reports the settings the shared reverb is using now: those given
// to SetReverb, or the zone the listener is in blended by how far inside
// it stands. Wet is 0 when the reverb is off.
func (m *Mixer) Reverb() ReverbSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applied
}

// ReverbZone is a region of the world with its own reverb: a cave, a
// hall, a tunnel. While the listener is inside the sphere the zone's
// Settings replace the mixer's shared reverb, blended in over Fade units
// from the edge so walking in never jumps. A zero Fade blends across the
// whole radius, so the zone is at full strength only at its centre; set
// Fade to a fraction of Radius for a room that sounds the same everywhere
// but its doorway. Where zones overlap, the one the listener is furthest
// inside wins.
type ReverbZone struct {
	Center   lin.Vec3
	Radius   float32
	Fade     float32
	Settings ReverbSettings
}

// SetReverbZones replaces the set of reverb zones. The mixer checks them
// against the listener whenever it moves, so a game sets them once per
// level and moves the listener each frame. Pass nil to clear them.
func (m *Mixer) SetReverbZones(zones []ReverbZone) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zones = append(m.zones[:0], zones...)
	m.updateReverb()
}

// effectiveReverb is the shared reverb in force for the listener's
// position: the base settings, or the best zone blended over them.
func (m *Mixer) effectiveReverb() ReverbSettings {
	base := m.baseReverb.withDefaults()
	var best *ReverbZone
	var weight float32
	for i := range m.zones {
		z := &m.zones[i]
		if z.Radius <= 0 {
			continue
		}
		dist := m.listener.Position.Sub(z.Center).Len()
		if dist >= z.Radius {
			continue
		}
		fade := z.Fade
		if fade <= 0 || fade > z.Radius {
			fade = z.Radius
		}
		w := min(1, (z.Radius-dist)/fade)
		if w > weight {
			best, weight = z, w
		}
	}
	if best == nil {
		return base
	}
	zone := best.Settings.withDefaults()
	if m.baseReverb.Wet <= 0 {
		// No room outside the zone: keep the zone's character and bring
		// only its level up, so the tail does not change colour on the way in.
		zone.Wet *= weight
		return zone
	}
	return base.lerp(zone, weight)
}

// updateReverb applies the effective settings, allocating or dropping the
// reverb as needed. Callers hold the lock.
func (m *Mixer) updateReverb() {
	s := m.effectiveReverb()
	if s.Wet <= 0 && len(m.zones) == 0 {
		m.reverb = nil
		m.applied = ReverbSettings{}
		return
	}
	if m.reverb == nil {
		m.reverb = newReverb(m.rate)
	}
	if s != m.applied {
		m.reverb.set(s)
		m.applied = s
	}
}

// SetReverb gives the bus a reverb of its own. Voices on the bus send to
// it instead of the mixer's shared reverb, so a cave's effects can ring
// while the music stays dry, or the reverse. The zero value removes it.
func (b *Bus) SetReverb(s ReverbSettings) {
	b.m.mu.Lock()
	defer b.m.mu.Unlock()
	if s.Wet <= 0 {
		b.reverb = nil
		return
	}
	if b.reverb == nil {
		b.reverb = newReverb(b.m.rate)
	}
	b.reverb.set(s.withDefaults())
}

// occlusionGain is the level an occluded voice keeps: 0 leaves it alone,
// 1 drops it 20 dB, in between on a decibel scale.
func occlusionGain(o float32) float32 {
	return float32(math.Pow(10, -float64(o)))
}

// occlusionCutoff is the low-pass cutoff for an occlusion amount: open
// (20 kHz) at 0, a wall's 400 Hz at 1, geometric in between.
func occlusionCutoff(o float32) float32 {
	return float32(20000 * math.Pow(0.02, float64(o)))
}

// reverb is Freeverb (Jezar at Dreampoint, public domain): eight parallel
// feedback combs with damping into four series allpasses, per channel,
// the right channel's delays offset for stereo width.
type reverb struct {
	combL, combR [8]comb
	apL, apR     [4]allpass
	wet1, wet2   float32
}

type comb struct {
	buf            []float32
	idx            int
	filt           float32
	feedback, damp float32
}

func (c *comb) process(in float32) float32 {
	out := c.buf[c.idx]
	c.filt = out*(1-c.damp) + c.filt*c.damp
	c.buf[c.idx] = in + c.filt*c.feedback
	c.idx++
	if c.idx == len(c.buf) {
		c.idx = 0
	}
	return out
}

type allpass struct {
	buf []float32
	idx int
}

func (a *allpass) process(in float32) float32 {
	b := a.buf[a.idx]
	a.buf[a.idx] = in + b*0.5
	a.idx++
	if a.idx == len(a.buf) {
		a.idx = 0
	}
	return b - in
}

func newReverb(rate int) *reverb {
	combs := [8]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	allpasses := [4]int{556, 441, 341, 225}
	const spread = 23
	scale := float64(rate) / 44100
	r := &reverb{}
	for i, n := range combs {
		r.combL[i].buf = make([]float32, int(float64(n)*scale))
		r.combR[i].buf = make([]float32, int(float64(n+spread)*scale))
	}
	for i, n := range allpasses {
		r.apL[i].buf = make([]float32, int(float64(n)*scale))
		r.apR[i].buf = make([]float32, int(float64(n+spread)*scale))
	}
	return r
}

func (r *reverb) set(s ReverbSettings) {
	s = s.withDefaults()
	feedback := s.RoomSize*0.28 + 0.7
	damp := s.Damping * 0.4
	for i := range r.combL {
		r.combL[i].feedback, r.combL[i].damp = feedback, damp
		r.combR[i].feedback, r.combR[i].damp = feedback, damp
	}
	const scaleWet = 3
	r.wet1 = s.Wet * scaleWet * (s.Width/2 + 0.5)
	r.wet2 = s.Wet * scaleWet * ((1 - s.Width) / 2)
}

// process runs the send buffer through the reverb and adds the tail to out.
func (r *reverb) process(send, out []float32) {
	const fixedGain = 0.015
	for i := 0; i+1 < len(send); i += 2 {
		in := (send[i] + send[i+1]) * fixedGain
		var l, rr float32
		for c := range r.combL {
			l += r.combL[c].process(in)
			rr += r.combR[c].process(in)
		}
		for a := range r.apL {
			l = r.apL[a].process(l)
			rr = r.apR[a].process(rr)
		}
		out[i] += l*r.wet1 + rr*r.wet2
		out[i+1] += rr*r.wet1 + l*r.wet2
	}
}
