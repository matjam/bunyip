package tracker

import "math"

// lowpass is a two-pole resonant low-pass filter (RBJ biquad).
type lowpass struct {
	b0, b1, b2, a1, a2 float32
	x1, x2, y1, y2     float32
	set                bool
}

func (f *lowpass) setCutoff(freq, q float64, rate int) {
	freq = min(freq, float64(rate)*0.45)
	w := 2 * math.Pi * freq / float64(rate)
	alpha := math.Sin(w) / (2 * q)
	cosw := math.Cos(w)
	a0 := 1 + alpha
	f.b0 = float32((1 - cosw) / 2 / a0)
	f.b1 = float32((1 - cosw) / a0)
	f.b2 = f.b0
	f.a1 = float32(-2 * cosw / a0)
	f.a2 = float32((1 - alpha) / a0)
	f.set = true
}

func (f *lowpass) process(x float32) float32 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// itCutoffHz converts Impulse Tracker's 0..127 cutoff to a frequency.
func itCutoffHz(cutoff int) float64 {
	return 110 * math.Pow(2, 0.25+float64(cutoff)/24)
}

// render mixes every voice into out for one span of a tick.
func (p *Player) render(out []float32) {
	clear(out)
	frames := len(out) / 2
	global := float32(p.globalVol) / 128
	for i := range p.chans {
		ch := &p.chans[i]
		p.renderVoice(out, frames, &ch.voice, ch, ch.outPeriod, ch.outVolume, ch.outPan, global)
	}
	for _, v := range p.bg {
		p.renderVoice(out, frames, v, nil, v.period, v.volume, v.pan, global)
	}
	if p.amigaFilter && p.AmigaFilter {
		for f := range frames {
			out[f*2] = p.filterL.process(out[f*2])
			out[f*2+1] = p.filterR.process(out[f*2+1])
		}
	}
}

func (p *Player) renderVoice(out []float32, frames int, v *voice, ch *channel, period float64, volume int, pan float32, global float32) {
	if !v.active || v.sample == nil || period <= 0 {
		return
	}
	s := v.sample
	if ch == nil {
		period = v.period
	}
	// Pitch: envelope (semitones) and auto-vibrato on top of the channel's period.
	if v.inst != nil && v.inst.PitchEnv.Enabled && !v.inst.PitchIsFilter {
		semis := float64(v.pitchEnv.value) / 8 // IT: 32 units = 4 semitones... one unit is 1/8
		if p.mod.LinearSlides {
			period -= semis * linearPerSemi
		} else {
			period *= math.Pow(2, -semis/12)
		}
	}
	period += p.autoVibratoDelta(v)
	step := frequency(p.mod.Format, p.mod.LinearSlides, period) / float64(p.rate)
	if step <= 0 || step > 64 {
		return
	}
	vol := float32(volume) / 64 * global * p.gain
	vol *= float32(s.GlobalVolume) / 64
	if v.inst != nil {
		vol *= float32(v.inst.GlobalVolume) / 128
		if v.inst.VolEnv.Enabled {
			vol *= v.volEnv.value / 64
		}
		vol *= float32(v.fade) / 65536
		if v.inst.PanEnv.Enabled {
			pan = clampPan(pan + v.panEnv.value/32*(1-abs32(pan)))
		}
	}
	if ch != nil {
		vol *= float32(ch.chanVol) / 64
	}
	if vol <= 0 {
		return
	}
	pan = clampPan(pan)
	// Linear panning, as the trackers themselves mix: centre is unity on
	// both sides, hard left is double on one side and silent on the other.
	left, right := vol*(1-pan), vol*(1+pan)
	// Filter setup once per tick span.
	useFilter := v.filterOn && v.cutoff < 127
	if useFilter {
		cutoff := v.cutoff
		if v.inst != nil && v.inst.PitchIsFilter && v.inst.PitchEnv.Enabled {
			cutoff = int(float32(cutoff) * (v.pitchEnv.value + 32) / 64)
		}
		q := 0.707 + float64(v.resonance)/128*6
		v.filter.setCutoff(itCutoffHz(min(max(cutoff, 0), 127)), q, p.rate)
	}
	data := s.Data
	n := len(data)
	loopStart, loopEnd, loopType := s.LoopStart, s.LoopEnd, s.Loop
	if v.sustainOn && !v.keyOff && s.SusLoop != LoopNone && s.SusLoopEnd > s.SusLoopStart+1 {
		loopStart, loopEnd, loopType = s.SusLoopStart, s.SusLoopEnd, s.SusLoop
	}
	looping := loopType != LoopNone && loopEnd > loopStart+1
	end := n
	if looping {
		end = min(loopEnd, n)
	}
	pos, dir := v.pos, v.dir
	for f := range frames {
		if dir > 0 && pos >= float64(end) {
			if !looping {
				v.active = false
				break
			}
			if ch != nil && ch.pendingSample != nil {
				// ProTracker: an instrument change without a note takes
				// effect when the loop wraps.
				v.sample = ch.pendingSample
				ch.pendingSample = nil
				v.pos = float64(v.sample.LoopStart)
				v.dir = 1
				return
			}
			if loopType == LoopPingPong {
				dir = -1
				pos = float64(end) - (pos - float64(end)) - 1
			} else {
				span := float64(end - loopStart)
				for pos >= float64(end) {
					pos -= span
				}
			}
		} else if dir < 0 && pos < float64(loopStart) {
			dir = 1
			pos = float64(loopStart) + (float64(loopStart) - pos)
		}
		idx := int(pos)
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			v.active = false
			break
		}
		frac := float32(pos - float64(idx))
		next := idx + dir
		if next >= end || next < 0 {
			next = idx
		}
		sample := data[idx]*(1-frac) + data[next]*frac
		if useFilter {
			sample = v.filter.process(sample)
		}
		out[f*2] += sample * left
		out[f*2+1] += sample * right
		pos += step * float64(dir)
	}
	v.pos, v.dir = pos, dir
}

func sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
