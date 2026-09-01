package audio

import "math"

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

// ReverbSettings describe the mixer's shared reverb, which voices feed
// through their Reverb send. Zero RoomSize, Damping and Width take the
// defaults 0.5, 0.5 and 1; a zero Wet turns the reverb off.
type ReverbSettings struct {
	RoomSize float32 // 0..1, how long the tail rings
	Damping  float32 // 0..1, how quickly highs die away
	Width    float32 // 0..1, stereo spread
	Wet      float32 // 0..1, level of the reverb in the mix
}

// SetReverb configures the shared reverb. Voices with a Reverb send add
// their signal to it; the tail is mixed on top of the dry output.
func (m *Mixer) SetReverb(s ReverbSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Wet <= 0 {
		m.reverb = nil
		return
	}
	if m.reverb == nil {
		m.reverb = newReverb(m.rate)
	}
	m.reverb.set(s)
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
	if s.RoomSize == 0 {
		s.RoomSize = 0.5
	}
	if s.Damping == 0 {
		s.Damping = 0.5
	}
	if s.Width == 0 {
		s.Width = 1
	}
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
