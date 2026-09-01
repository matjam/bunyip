package tracker

// processTick runs the per-tick part of every channel's effect.
func (p *Player) processTick() {
	cells := p.currentRow()
	for i := range p.chans {
		ch := &p.chans[i]
		cell := cells[i]
		if ch.noteDelay >= 0 {
			if p.tick == ch.noteDelay {
				ch.noteDelay = -1
				p.triggerCell(ch, ch.delayed)
			}
			continue
		}
		if ch.noteCut >= 0 && p.tick == ch.noteCut {
			ch.volume, ch.outVolume = 0, 0
		}
		ch.outPeriod = ch.period
		ch.outVolume = ch.volume
		switch cell.Effect {
		case effArpeggio:
			p.arpeggio(ch)
		case effPortaUp:
			ch.period = max(ch.period-p.portaUnit()*float64(ch.portaSpeed), 1)
			ch.outPeriod = ch.period
		case effPortaDown:
			ch.period += p.portaUnit() * float64(ch.portaSpeed)
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
		case effVolSlide:
			p.volSlide(ch)
		case effRetrig:
			p.retrig(ch)
		case effTremor:
			p.tremor(ch)
		}
	}
}

func (p *Player) arpeggio(ch *channel) {
	if ch.note < 0 {
		return
	}
	switch p.tick % 3 {
	case 1:
		ch.outPeriod = p.notePeriod(ch, ch.note+int(ch.arpParam>>4))
	case 2:
		ch.outPeriod = p.notePeriod(ch, ch.note+int(ch.arpParam&0x0F))
	}
}

func (p *Player) tonePorta(ch *channel) {
	if ch.portaTarget <= 0 {
		return
	}
	step := p.portaUnit() * float64(ch.portaSpeed)
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
	delta := waveform(ch.vibWave, ch.vibPos) * int(ch.vibDepth)
	if p.mod.Format == FormatS3M {
		delta /= 32
	} else {
		delta /= 128
	}
	ch.outPeriod = ch.period + float64(delta/divisor)
	ch.vibPos += int(ch.vibSpeed)
}

func (p *Player) tremolo(ch *channel) {
	delta := waveform(ch.tremWave, ch.tremPos) * int(ch.tremDepth) / 64
	ch.outVolume = max(0, min(64, ch.volume+delta))
	ch.tremPos += int(ch.tremSpeed)
}

// volSlide applies Axy / Dxy on ticks after the first. ScreamTracker's
// fine slides (DxF, DFy) only act on tick 0 and are skipped here.
func (p *Player) volSlide(ch *channel) {
	x, y := int(ch.volSlide>>4), int(ch.volSlide&0x0F)
	if p.mod.Format == FormatS3M && (x == 0x0F || y == 0x0F) && x != 0 && y != 0 {
		return
	}
	switch {
	case x > 0:
		ch.volume = min(ch.volume+x, 64)
	case y > 0:
		ch.volume = max(ch.volume-y, 0)
	}
	ch.outVolume = ch.volume
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
	ch.playing = ch.sample != nil && len(ch.sample.Data) > 0
	if p.mod.Format == FormatS3M {
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
