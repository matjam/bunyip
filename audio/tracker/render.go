package tracker

// render mixes every channel into out for one span of a tick.
func (p *Player) render(out []float32) {
	clear(out)
	frames := len(out) / 2
	global := float32(p.globalVol) / 64
	for i := range p.chans {
		ch := &p.chans[i]
		if !ch.playing || ch.sample == nil || ch.outPeriod <= 0 || ch.outVolume <= 0 {
			continue
		}
		s := ch.sample
		step := frequency(p.mod.Format, ch.outPeriod) / float64(p.rate)
		if step <= 0 {
			continue
		}
		vol := float32(ch.outVolume) / 64 * global * p.gain
		pan := max(-1, min(1, ch.pan))
		left, right := vol*(1-pan)/2*2, vol*(1+pan)/2*2
		if pan == 0 {
			left, right = vol, vol
		}
		data := s.Data
		n := len(data)
		end := n
		if s.loops() {
			end = s.LoopEnd
		}
		pos := ch.pos
		for f := range frames {
			if pos >= float64(end) {
				if !s.loops() {
					ch.playing = false
					break
				}
				span := float64(s.LoopEnd - s.LoopStart)
				for pos >= float64(end) {
					pos -= span
				}
			}
			idx := int(pos)
			frac := float32(pos - float64(idx))
			next := idx + 1
			if next >= end {
				if s.loops() {
					next = s.LoopStart
				} else {
					next = idx
				}
			}
			v := data[idx]*(1-frac) + data[next]*frac
			out[f*2] += v * left
			out[f*2+1] += v * right
			pos += step
		}
		ch.pos = pos
	}
}
