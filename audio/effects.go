package audio

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// biquad holds a second-order Butterworth low-pass filter's coefficients.
// It is separate from the filter's state because the two are reached from
// different threads: the game loop sets coefficients under the mixer's
// lock, and the mixer runs the state over a block without it.
type biquad struct {
	b0, b1, b2, a1, a2 float32
}

// set computes the coefficients for a cutoff in Hz at the given rate.
func (c *biquad) set(cutoff float32, rate int) {
	cutoff = min(cutoff, float32(rate)*0.49)
	w0 := 2 * math.Pi * float64(cutoff) / float64(rate)
	cs, sn := math.Cos(w0), math.Sin(w0)
	alpha := sn / (2 * math.Sqrt2 / 2)
	a0 := 1 + alpha
	c.b0 = float32((1 - cs) / 2 / a0)
	c.b1 = float32((1 - cs) / a0)
	c.b2 = c.b0
	c.a1 = float32(-2 * cs / a0)
	c.a2 = float32((1 - alpha) / a0)
}

// newBiquad returns the coefficients for a cutoff in Hz at the given rate.
func newBiquad(cutoff float32, rate int) biquad {
	var c biquad
	c.set(cutoff, rate)
	return c
}

// lowPass is the running state of a biquad over stereo frames, in
// transposed direct form II: two delays per channel. It belongs to the
// mixer's thread and outlives coefficient changes, so retuning a filter
// while it runs never resets it and never clicks.
type lowPass struct {
	l0, l1 float32 // left channel delays
	r0, r1 float32 // right channel delays
}

// process filters a block of interleaved stereo in place. The two
// channels are unrolled and every coefficient and delay is held in a
// local, so the inner loop touches no memory but the buffer.
func (f *lowPass) process(c biquad, buf []float32) {
	b0, b1, b2, a1, a2 := c.b0, c.b1, c.b2, c.a1, c.a2
	l0, l1, r0, r1 := f.l0, f.l1, f.r0, f.r1
	for i := 0; i+1 < len(buf); i += 2 {
		x := buf[i]
		y := b0*x + l0
		l0 = b1*x - a1*y + l1
		l1 = b2*x - a2*y
		buf[i] = y

		x = buf[i+1]
		y = b0*x + r0
		r0 = b1*x - a1*y + r1
		r1 = b2*x - a2*y
		buf[i+1] = y
	}
	f.l0, f.l1, f.r0, f.r1 = l0, l1, r0, r1
}

// ReverbSettings describe a reverb, which voices feed through their Reverb
// send. Zero RoomSize, Damping and Width take the defaults 0.5, 0.5 and 1;
// a zero Wet turns the reverb off, so the zero value is no reverb. Every
// field runs from 0 to 1 and values outside that are clamped when the
// reverb uses them, because a room larger than 1 has feedback that never
// decays.
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
	Center   lin.Vec3       // sphere centre in listener world units
	Radius   float32        // sphere radius; nonpositive zones are ignored
	Fade     float32        // inward blend distance; nonpositive means Radius
	Settings ReverbSettings // reverb at full zone strength
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

// updateReverb settles the effective settings, allocating or dropping the
// reverb as needed. The settings reach the reverb itself at the start of
// the next mixed block, because the reverb runs on the mixer's thread
// without the lock. Callers hold the lock.
func (m *Mixer) updateReverb() {
	s := m.effectiveReverb()
	if s.Wet <= 0 && len(m.zones) == 0 {
		m.reverb = nil
		m.applied = ReverbSettings{}
		m.pending = false
		return
	}
	if m.reverb == nil {
		m.reverb = newReverb(m.rate)
		m.pending = true
	}
	if s != m.applied {
		m.applied = s
		m.pending = true
	}
}

// SetReverb gives the bus a reverb of its own. Voices on the bus send to
// it instead of the mixer's shared reverb, so a cave's effects can ring
// while the music stays dry, or the reverse. The zero value removes it.
// The settings reach the reverb at the start of the next mixed block.
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
	b.applied, b.pending = s.withDefaults(), true
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
	// The comb feedback reaches 0.98 at a room size of 1. Sizes are
	// clamped here rather than in the setter, because a feedback of one
	// or more never decays: the tail grows until the output is a solid
	// clamped block and then reaches NaN.
	feedback := clamp01(s.RoomSize)*0.28 + 0.7
	damp := clamp01(s.Damping) * 0.4
	width := clamp01(s.Width)
	for i := range r.combL {
		r.combL[i].feedback, r.combL[i].damp = feedback, damp
		r.combR[i].feedback, r.combR[i].damp = feedback, damp
	}
	const scaleWet = 3
	wet := max(s.Wet, 0)
	r.wet1 = wet * scaleWet * (width/2 + 0.5)
	r.wet2 = wet * scaleWet * ((1 - width) / 2)
}

// clamp01 holds a setting to the 0..1 the effects are designed for.
func clamp01(v float32) float32 { return max(0, min(1, v)) }

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
