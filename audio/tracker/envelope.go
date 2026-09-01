package tracker

// value evaluates the envelope at tick, interpolating between points.
func (e *Envelope) value(tick int) float32 {
	pts := e.Points
	if len(pts) == 0 {
		return 0
	}
	if tick <= pts[0].Tick {
		return pts[0].Value
	}
	for i := 1; i < len(pts); i++ {
		if tick <= pts[i].Tick {
			a, b := pts[i-1], pts[i]
			if b.Tick == a.Tick {
				return b.Value
			}
			t := float32(tick-a.Tick) / float32(b.Tick-a.Tick)
			return a.Value + (b.Value-a.Value)*t
		}
	}
	return pts[len(pts)-1].Value
}

// advance moves an envelope one tick, honouring sustain (while the key is
// held) and loops. It reports whether the envelope has run past its end.
func (e *Envelope) advance(st *envState, keyOff bool) {
	if !e.Enabled || len(e.Points) == 0 {
		st.value = 0
		return
	}
	st.value = e.value(st.tick)
	pts := e.Points
	if e.Sustain && !keyOff && e.SustainEnd < len(pts) && st.tick >= pts[e.SustainEnd].Tick {
		st.tick = pts[e.SustainStart].Tick
		if e.SustainStart == e.SustainEnd {
			return // hold on the sustain point
		}
	}
	if e.Loop && e.LoopEnd < len(pts) && st.tick >= pts[e.LoopEnd].Tick {
		st.tick = pts[e.LoopStart].Tick
		if e.LoopStart == e.LoopEnd {
			return
		}
	}
	if st.tick >= pts[len(pts)-1].Tick {
		st.done = true
		return
	}
	st.tick++
}

// updateVoices runs envelopes, fades and auto-vibrato for every voice after
// the row or tick effects have set the channel's output values.
func (p *Player) updateVoices() {
	for i := range p.chans {
		ch := &p.chans[i]
		p.updateVoice(&ch.voice)
	}
	live := p.bg[:0]
	for _, v := range p.bg {
		p.updateVoice(v)
		if v.active {
			live = append(live, v)
		}
	}
	p.bg = live
}

func (p *Player) updateVoice(v *voice) {
	if !v.active {
		return
	}
	if v.inst != nil {
		in := v.inst
		in.VolEnv.advance(&v.volEnv, v.keyOff)
		in.PanEnv.advance(&v.panEnv, v.keyOff)
		in.PitchEnv.advance(&v.pitchEnv, v.keyOff)
		// Fading starts at key off (IT), or once the volume envelope has
		// finished after key off (XM keeps the note until then).
		if v.keyOff && (p.mod.Format == FormatIT || !in.VolEnv.Enabled || v.volEnv.done) {
			v.fading = true
		}
		if v.fading {
			v.fade -= in.Fadeout
			if v.fade <= 0 {
				v.fade = 0
				v.active = false
			}
		}
		if v.keyOff && !in.VolEnv.Enabled && p.mod.Format == FormatXM {
			v.active = false // FastTracker cuts a key-off note without an envelope
		}
		if in.VolEnv.Enabled && v.volEnv.done && v.volEnv.value <= 0 {
			v.active = false
		}
	}
	if s := v.sample; s != nil && s.Vibrato.Depth > 0 {
		av := &v.autoVib
		if av.sweep < s.Vibrato.Sweep {
			av.sweep++
		}
		av.pos += s.Vibrato.Rate
	}
}

// autoVibratoDelta is the instrument vibrato's period offset this tick.
func (p *Player) autoVibratoDelta(v *voice) float64 {
	s := v.sample
	if s == nil || s.Vibrato.Depth == 0 {
		return 0
	}
	depth := float64(s.Vibrato.Depth)
	if s.Vibrato.Sweep > 0 {
		depth *= float64(v.autoVib.sweep) / float64(s.Vibrato.Sweep)
	}
	w := float64(waveform(s.Vibrato.Type, v.autoVib.pos/4))
	if p.mod.LinearSlides {
		return w * depth / 256 // XM: depth in 1/... of a semitone
	}
	return w * depth / 64
}
