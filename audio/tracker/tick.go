package tracker

// processTick runs the per-tick part of every channel's effect and volume
// column, then computes the channel's output pitch, volume and pan.
func (p *Player) processTick() {
	cells := p.currentRow()
	for i := range p.chans {
		ch := &p.chans[i]
		cell := cells[i]
		if ch.noteDelay >= 0 {
			if p.tick == ch.noteDelay {
				ch.noteDelay = -1
				p.triggerCell(ch, ch.delayed)
				p.volumeColumnRow(ch, ch.delayed)
				p.rowEffect(ch, ch.delayed)
			}
			ch.outPeriod, ch.outVolume, ch.outPan = ch.period, ch.volume, ch.voice.pan
			continue
		}
		if ch.noteCut >= 0 && p.tick == ch.noteCut {
			ch.volume = 0
		}
		if ch.keyOffTick > 0 && p.tick == ch.keyOffTick {
			p.keyOff(&ch.voice)
		}
		ch.outPeriod, ch.outVolume, ch.outPan = ch.period, ch.volume, ch.voice.pan
		p.volumeColumnTick(ch, cell)
		switch cell.Effect {
		case effArpeggio:
			p.arpeggio(ch)
		case effPortaUp:
			ch.period = max(ch.period-p.slideUnit()*float64(ch.portaSpeed), 1)
			ch.outPeriod = ch.period
		case effPortaDown:
			ch.period += p.slideUnit() * float64(ch.portaSpeed)
			ch.outPeriod = ch.period
		case effTonePorta:
			p.tonePorta(ch)
		case effTonePortaVol:
			p.tonePorta(ch)
			p.volSlide(ch)
		case effVibrato:
			p.vibrato(ch, 1)
		case effFineVibrato:
			p.vibrato(ch, 4)
		case effVibratoVol:
			p.vibrato(ch, 1)
			p.volSlide(ch)
		case effTremolo:
			p.tremolo(ch)
		case effPanbrello:
			p.panbrello(ch)
		case effVolSlide:
			p.volSlide(ch)
		case effRetrig, effMultiRetrig:
			p.retrig(ch)
		case effTremor:
			p.tremor(ch)
		case effGlobalVolSlide:
			p.globalVolSlide(ch)
		case effPanSlide:
			p.panSlide(ch)
		case effChanVolSlide:
			hi, lo := ch.chanVolSlide>>4, ch.chanVolSlide&0x0F
			if hi != 0 && lo != 0x0F && lo == 0 {
				ch.chanVol = min(ch.chanVol+int(hi), 64)
			} else if lo != 0 && hi != 0x0F && hi == 0 {
				ch.chanVol = max(ch.chanVol-int(lo), 0)
			}
		case effSetTempo:
			if cell.Param < 0x20 && p.mod.Format == FormatIT {
				if cell.Param>>4 == 0 {
					p.setTempo(p.tempo - int(cell.Param&0x0F))
				} else {
					p.setTempo(p.tempo + int(cell.Param&0x0F))
				}
			}
		}
	}
}

func (p *Player) volumeColumnTick(ch *channel, cell Cell) {
	x := cell.VolParam
	switch cell.VolCmd {
	case volSlideUp:
		ch.volume = min(ch.volume+x, 64)
		ch.outVolume = ch.volume
	case volSlideDown:
		ch.volume = max(ch.volume-x, 0)
		ch.outVolume = ch.volume
	case volVibrato:
		p.vibrato(ch, 1)
	case volVibratoDepth:
		if p.mod.Format == FormatXM {
			p.vibrato(ch, 1)
		}
	case volPanSlideLeft:
		ch.voice.pan = clampPan(ch.voice.pan - float32(x)/32)
	case volPanSlideRight:
		ch.voice.pan = clampPan(ch.voice.pan + float32(x)/32)
	case volTonePorta:
		p.tonePorta(ch)
	case volPortaUp:
		ch.period = max(ch.period-p.slideUnit()*float64(x)*4, 1)
		ch.outPeriod = ch.period
	case volPortaDown:
		ch.period += p.slideUnit() * float64(x) * 4
		ch.outPeriod = ch.period
	}
}

func (p *Player) arpeggio(ch *channel) {
	if !ch.active {
		return
	}
	switch p.tick % 3 {
	case 1:
		ch.outPeriod = p.notePeriod(&ch.voice, ch.note+int(ch.arpParam>>4))
	case 2:
		ch.outPeriod = p.notePeriod(&ch.voice, ch.note+int(ch.arpParam&0x0F))
	}
}

func (p *Player) tonePorta(ch *channel) {
	if ch.portaTarget <= 0 {
		return
	}
	step := p.slideUnit() * float64(ch.portaSpeed)
	switch {
	case ch.period < ch.portaTarget:
		ch.period = min(ch.period+step, ch.portaTarget)
	case ch.period > ch.portaTarget:
		ch.period = max(ch.period-step, ch.portaTarget)
	}
	ch.outPeriod = ch.period
}

// vibrato offsets the output period; divisor 4 gives the fine variant.
func (p *Player) vibrato(ch *channel, divisor int) {
	delta := waveform(ch.vibWave, ch.vibPos) * ch.vibDepth
	switch {
	case p.mod.Format == FormatMOD:
		delta /= 128
	case p.mod.LinearSlides:
		delta /= 64
	default:
		delta /= 32
	}
	ch.outPeriod = ch.period + float64(delta/divisor)
	ch.vibPos += ch.vibSpeed
}

func (p *Player) tremolo(ch *channel) {
	delta := waveform(ch.tremWave, ch.tremPos) * ch.tremDepth / 64
	ch.outVolume = max(0, min(64, ch.volume+delta))
	ch.tremPos += ch.tremSpeed
}

func (p *Player) panbrello(ch *channel) {
	delta := float32(waveform(0, ch.panbrPos)*ch.panbrDepth) / (255 * 16)
	ch.outPan = clampPan(ch.voice.pan + delta)
	ch.panbrPos += ch.panbrSpeed
}

// volSlide applies Axy / Dxy on ticks after the first. Fine slides (DxF,
// DFy) act on tick 0 only and are skipped here.
func (p *Player) volSlide(ch *channel) {
	x, y := int(ch.volSlide>>4), int(ch.volSlide&0x0F)
	shared := p.mod.Format == FormatS3M || p.mod.Format == FormatIT
	if shared && (x == 0x0F || y == 0x0F) && x != 0 && y != 0 {
		return
	}
	switch {
	case x > 0 && (y == 0 || !shared):
		ch.volume = min(ch.volume+x, 64)
	case y > 0:
		ch.volume = max(ch.volume-y, 0)
	}
	ch.outVolume = ch.volume
}

func (p *Player) globalVolSlide(ch *channel) {
	x, y := int(ch.globalSlide>>4), int(ch.globalSlide&0x0F)
	scale := 2
	if p.mod.Format == FormatIT {
		scale = 1
	}
	switch {
	case x > 0 && y == 0:
		p.globalVol = min(p.globalVol+x*scale, 128)
	case y > 0 && x == 0:
		p.globalVol = max(p.globalVol-y*scale, 0)
	}
}

func (p *Player) panSlide(ch *channel) {
	x, y := ch.panSlide>>4, ch.panSlide&0x0F
	if x == 0x0F || y == 0x0F {
		return
	}
	switch p.mod.Format {
	case FormatXM: // Pxy: x right, y left
		ch.voice.pan = clampPan(ch.voice.pan + float32(x)/128 - float32(y)/128)
	default: // IT Pxy: x left, y right
		ch.voice.pan = clampPan(ch.voice.pan - float32(x)/32 + float32(y)/32)
	}
	ch.outPan = ch.voice.pan
}

func (p *Player) retrig(ch *channel) {
	interval := int(ch.retrig & 0x0F)
	if p.mod.Format == FormatMOD {
		interval = int(ch.retrig)
	}
	if interval == 0 {
		return
	}
	ch.retrigCount++
	if ch.retrigCount < interval {
		return
	}
	ch.retrigCount = 0
	ch.pos = 0
	ch.dir = 1
	ch.active = ch.sample != nil && len(ch.sample.Data) > 0
	if p.mod.Format != FormatMOD {
		switch v := ch.retrig >> 4; v {
		case 1, 2, 3, 4, 5:
			ch.volume = max(ch.volume-(1<<(v-1)), 0)
		case 6:
			ch.volume = ch.volume * 2 / 3
		case 7:
			ch.volume /= 2
		case 9, 0xA, 0xB, 0xC, 0xD:
			ch.volume = min(ch.volume+(1<<(v-9)), 64)
		case 0xE:
			ch.volume = min(ch.volume*3/2, 64)
		case 0xF:
			ch.volume = min(ch.volume*2, 64)
		}
		ch.outVolume = ch.volume
	}
}

// tremor switches the note on for x+1 ticks and off for y+1.
func (p *Player) tremor(ch *channel) {
	on, off := int(ch.tremorParam>>4)+1, int(ch.tremorParam&0x0F)+1
	ch.tremorCount++
	if ch.tremorOff {
		if ch.tremorCount >= off {
			ch.tremorOff, ch.tremorCount = false, 0
		}
	} else if ch.tremorCount >= on {
		ch.tremorOff, ch.tremorCount = true, 0
	}
	if ch.tremorOff {
		ch.outVolume = 0
	}
}
