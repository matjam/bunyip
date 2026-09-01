package tracker

// rowEffect runs the tick-0 part of a cell's effect.
func (p *Player) rowEffect(ch *channel, cell Cell) {
	x := cell.Param
	m := p.mod
	unit := p.slideUnit()
	shared := m.Format == FormatS3M || m.Format == FormatIT
	if shared && x != 0 {
		ch.mem = x
	}
	recall := func(mem *byte) byte {
		if x != 0 {
			*mem = x
			return x
		}
		if !p.hasMemory() {
			return 0
		}
		if shared {
			return ch.mem
		}
		return *mem
	}
	switch cell.Effect {
	case effArpeggio:
		ch.arpParam = recall(&ch.arpParam)
	case effPortaUp, effPortaDown:
		ch.portaSpeed = int(recall(&ch.portaMem))
	case effTonePorta:
		if v := recall(&ch.portaMem); v != 0 || !shared {
			if m.CompatGxx || !shared {
				ch.portaSpeed = int(v)
			} else if v != 0 {
				ch.portaSpeed = int(v)
			}
		}
	case effTonePortaVol:
		ch.volSlide = recall(&ch.volSlide)
		if shared {
			p.s3mFineVolSlide(ch)
		}
	case effVibrato, effFineVibrato:
		if x>>4 != 0 {
			ch.vibSpeed = int(x >> 4)
		}
		if x&0x0F != 0 {
			ch.vibDepth = int(x & 0x0F)
		}
		if shared && x == 0 {
			ch.vibSpeed, ch.vibDepth = int(ch.vibMem>>4), int(ch.vibMem&0x0F)
		} else if shared {
			ch.vibMem = x
		}
	case effVibratoVol:
		ch.volSlide = recall(&ch.volSlide)
		if shared {
			p.s3mFineVolSlide(ch)
		}
	case effTremolo:
		if x>>4 != 0 {
			ch.tremSpeed = int(x >> 4)
		}
		if x&0x0F != 0 {
			ch.tremDepth = int(x & 0x0F)
		}
	case effPanbrello:
		if x>>4 != 0 {
			ch.panbrSpeed = int(x >> 4)
		}
		if x&0x0F != 0 {
			ch.panbrDepth = int(x & 0x0F)
		}
	case effSetPan:
		ch.voice.pan = float32(x)/127.5 - 1
	case effSetPanCoarse:
		ch.voice.pan = float32(x)/7.5 - 1
	case effSurround:
		ch.voice.pan = 0
	case effSetSampleOffsetHigh:
		ch.offsetHigh = int(x)
	case effOffset:
		off := int(recall(&ch.offset)) * 256
		if m.Format == FormatIT {
			off += ch.offsetHigh << 16
		}
		if ch.sample != nil && cell.Note >= 0 && cell.Note < NoteOff {
			ch.pos = float64(off)
			if ch.pos >= float64(len(ch.sample.Data)) {
				if ch.sample.loops() {
					ch.pos = float64(ch.sample.LoopStart)
				} else if m.Format == FormatIT || m.Format == FormatXM {
					ch.active = false
				} else {
					ch.pos = float64(len(ch.sample.Data))
				}
			}
		}
	case effVolSlide:
		ch.volSlide = recall(&ch.volSlide)
		if shared {
			p.s3mFineVolSlide(ch)
		}
	case effPosJump:
		p.jumpOrder = int(x)
	case effSetVolume:
		ch.volume = min(int(x), 64)
	case effPatBreak:
		p.breakRow = int(x)
	case effSetSpeed:
		if x > 0 {
			p.speed = int(x)
		}
	case effSetTempo:
		if x >= 0x20 {
			p.setTempo(int(x))
		} else if m.Format == FormatIT {
			ch.mem = x // T0x/T1x slide on later ticks
		}
	case effFinePortaUp:
		v := recall(&ch.portaMem) & 0x0F
		ch.period = max(ch.period-unit*float64(v), 1)
	case effFinePortaDown:
		v := recall(&ch.portaMem) & 0x0F
		ch.period += unit * float64(v)
	case effExtraFinePortaUp:
		v := recall(&ch.portaMem) & 0x0F
		ch.period = max(ch.period-float64(v), 1)
	case effExtraFinePortaDown:
		v := recall(&ch.portaMem) & 0x0F
		ch.period += float64(v)
	case effVibWave:
		ch.vibWave = int(x)
	case effTremWave:
		ch.tremWave = int(x)
	case effGlissando:
		ch.glissando = x != 0
	case effFinetune:
		ft := int(x)
		if ft > 7 {
			ft -= 16
		}
		if ch.sample != nil {
			ch.finetune = float32(ft) / 8
			s := *ch.sample
			s.Finetune = ch.finetune
			v := voice{sample: &s}
			ch.period = p.notePeriod(&v, ch.note)
		}
	case effPatLoop:
		if x == 0 {
			p.loopRow = p.row
		} else if p.loopCount == 0 {
			p.loopCount = int(x)
			p.loopJump = true
		} else {
			p.loopCount--
			if p.loopCount > 0 {
				p.loopJump = true
			}
		}
	case effRetrig, effMultiRetrig:
		if m.Format == FormatMOD {
			ch.retrig = x
		} else {
			ch.retrig = recall(&ch.retrig)
		}
		if m.Format == FormatXM && cell.Effect == effMultiRetrig && cell.Note < 0 {
			ch.retrigCount = 0
		}
	case effFineVolUp:
		ch.volume = min(ch.volume+int(x), 64)
	case effFineVolDown:
		ch.volume = max(ch.volume-int(x), 0)
	case effNoteCut:
		if x == 0 {
			ch.volume = 0
		} else {
			ch.noteCut = int(x)
		}
	case effPatDelay:
		if p.patDelay == 0 {
			p.patDelay = int(x)
		}
	case effTremor:
		ch.tremorParam = recall(&ch.tremorParam)
	case effGlobalVol:
		if m.Format == FormatXM || m.Format == FormatS3M {
			p.globalVol = min(int(x), 64) * 2
		} else {
			p.globalVol = min(int(x), 128)
		}
	case effGlobalVolSlide:
		ch.globalSlide = recall(&ch.globalSlide)
	case effKeyOff:
		ch.keyOffTick = int(x)
		if x == 0 {
			p.keyOff(&ch.voice)
		}
	case effEnvPos:
		ch.volEnv.tick = int(x)
		ch.panEnv.tick = int(x)
	case effPanSlide:
		ch.panSlide = recall(&ch.panSlide)
		if shared {
			hi, lo := ch.panSlide>>4, ch.panSlide&0x0F
			if lo == 0xF && hi != 0 {
				ch.voice.pan = clampPan(ch.voice.pan + float32(hi)/32)
			} else if hi == 0xF && lo != 0 {
				ch.voice.pan = clampPan(ch.voice.pan - float32(lo)/32)
			}
		}
	case effChanVol:
		ch.chanVol = min(int(x), 64)
	case effChanVolSlide:
		ch.chanVolSlide = recall(&ch.chanVolSlide)
		hi, lo := ch.chanVolSlide>>4, ch.chanVolSlide&0x0F
		if lo == 0xF && hi != 0 {
			ch.chanVol = min(ch.chanVol+int(hi), 64)
		} else if hi == 0xF && lo != 0 {
			ch.chanVol = max(ch.chanVol-int(lo), 0)
		}
	case effFilter:
		switch {
		case x < 0x80:
			ch.cutoff, ch.filterOn = int(x), true
			ch.voice.cutoff, ch.voice.filterOn = int(x), true
		case x < 0x90:
			ch.resonance, ch.filterOn = int(x&0x0F)*8, true
			ch.voice.resonance, ch.voice.filterOn = int(x&0x0F)*8, true
		}
	case effAmigaFilter:
		p.amigaFilter = x == 0
	case effPastNote:
		switch x {
		case 0:
			for _, v := range p.bg {
				v.active = false
			}
		case 1:
			for _, v := range p.bg {
				p.keyOff(v)
			}
		case 2:
			for _, v := range p.bg {
				v.fading = true
			}
		case 3, 4, 5, 6:
			if ch.inst != nil {
				ch.inst.NNA = NNA(x - 3) // S73..S76 override the new-note action
			}
		}
	}
}

// s3mFineVolSlide applies the tick-0 half of a ScreamTracker/Impulse Dxy.
func (p *Player) s3mFineVolSlide(ch *channel) {
	x, y := ch.volSlide>>4, ch.volSlide&0x0F
	switch {
	case y == 0x0F && x != 0:
		ch.volume = min(ch.volume+int(x), 64)
	case x == 0x0F && y != 0:
		ch.volume = max(ch.volume-int(y), 0)
	}
}

func clampPan(v float32) float32 { return max(-1, min(1, v)) }
