package tracker

// processRow handles tick 0 of a row: note and instrument triggers, volume
// column, and the effects that act once per row.
func (p *Player) processRow() {
	cells := p.currentRow()
	for i := range p.chans {
		ch := &p.chans[i]
		cell := cells[i]
		ch.noteCut, ch.noteDelay = -1, -1
		ch.retrigCount = 0
		ch.outPeriod = ch.period
		ch.outVolume = ch.volume
		if cell.Effect == effNoteDelay && cell.Param > 0 {
			ch.noteDelay = int(cell.Param)
			ch.delayed = cell
			continue
		}
		p.triggerCell(ch, cell)
		p.rowEffect(ch, cell)
	}
}

// triggerCell applies instrument, note and volume column of a cell.
func (p *Player) triggerCell(ch *channel, cell Cell) {
	if cell.Instrument > 0 && cell.Instrument <= len(p.mod.Samples) {
		s := &p.mod.Samples[cell.Instrument-1]
		ch.sample = s
		ch.volume = s.Volume
		ch.c4speed = s.C4Speed
		ch.finetune = s.Finetune
		ch.outVolume = ch.volume
	}
	porta := cell.Effect == effTonePorta || cell.Effect == effTonePortaVol
	switch {
	case cell.Note == NoteOff:
		ch.playing = false
	case cell.Note >= 0 && ch.sample != nil:
		target := p.notePeriod(ch, cell.Note)
		if porta {
			ch.portaTarget = target
			ch.note = cell.Note
		} else {
			ch.note = cell.Note
			ch.period = target
			ch.outPeriod = target
			ch.pos = 0
			ch.playing = len(ch.sample.Data) > 0
			if ch.vibWave&4 == 0 {
				ch.vibPos = 0
			}
			if ch.tremWave&4 == 0 {
				ch.tremPos = 0
			}
			ch.tremorCount, ch.tremorOff = 0, false
		}
	}
	if cell.Volume >= 0 {
		ch.volume = cell.Volume
		ch.outVolume = ch.volume
	}
}

// notePeriod converts a note index to the format's period for the channel's sample.
func (p *Player) notePeriod(ch *channel, note int) float64 {
	if p.mod.Format == FormatMOD {
		return modPeriod(note, ch.finetune)
	}
	return s3mPeriod(note, ch.c4speed)
}

// rowEffect runs the tick-0 part of a cell's effect.
func (p *Player) rowEffect(ch *channel, cell Cell) {
	x := cell.Param
	s3m := p.mod.Format == FormatS3M
	if s3m && x != 0 {
		ch.s3mMem = x
	}
	switch cell.Effect {
	case effArpeggio:
		ch.arpParam = p.param(ch, x, &ch.arpParam)
	case effPortaUp, effPortaDown:
		p.param(ch, x, &ch.portaSpeed)
		if s3m && ch.portaSpeed == 0 {
			ch.portaSpeed = ch.s3mMem
		}
		if !s3m {
			ch.portaSpeed = x
		}
	case effTonePorta:
		if x != 0 || s3m {
			ch.portaSpeed = p.param(ch, x, &ch.portaSpeed)
		}
	case effTonePortaVol:
		ch.volSlide = p.param(ch, x, &ch.volSlide)
	case effVibrato, effFineVibrato:
		if x>>4 != 0 {
			ch.vibSpeed = x >> 4
		}
		if x&0x0F != 0 {
			ch.vibDepth = x & 0x0F
		}
	case effVibratoVol:
		ch.volSlide = p.param(ch, x, &ch.volSlide)
	case effTremolo:
		if x>>4 != 0 {
			ch.tremSpeed = x >> 4
		}
		if x&0x0F != 0 {
			ch.tremDepth = x & 0x0F
		}
	case effSetPan:
		ch.pan = float32(x)/127.5 - 1
	case effSetPanCoarse:
		ch.pan = float32(x)/7.5 - 1
	case effOffset:
		off := p.param(ch, x, &ch.offset)
		if ch.sample != nil && cell.Note >= 0 && cell.Note != NoteOff {
			ch.pos = float64(int(off) * 256)
			if ch.pos >= float64(len(ch.sample.Data)) {
				if ch.sample.loops() {
					ch.pos = float64(ch.sample.LoopStart)
				} else {
					ch.playing = false
				}
			}
		}
	case effVolSlide:
		ch.volSlide = p.param(ch, x, &ch.volSlide)
		if s3m {
			p.s3mFineVolSlide(ch)
		}
	case effPosJump:
		p.jumpOrder = int(x)
	case effSetVolume:
		ch.volume = min(int(x), 64)
		ch.outVolume = ch.volume
	case effPatBreak:
		p.breakRow = int(x)
	case effSetSpeed:
		if x > 0 {
			p.speed = int(x)
		}
	case effSetTempo:
		if x > 0 {
			p.setTempo(int(x))
		}
	case effFinePortaUp:
		ch.period = max(ch.period-p.portaUnit()*float64(x), 1)
		ch.outPeriod = ch.period
	case effFinePortaDown:
		ch.period += p.portaUnit() * float64(x)
		ch.outPeriod = ch.period
	case effExtraFinePortaUp:
		ch.period = max(ch.period-float64(x), 1)
		ch.outPeriod = ch.period
	case effExtraFinePortaDown:
		ch.period += float64(x)
		ch.outPeriod = ch.period
	case effVibWave:
		ch.vibWave = int(x)
	case effTremWave:
		ch.tremWave = int(x)
	case effFinetune:
		ft := int(x)
		if ft > 7 {
			ft -= 16
		}
		ch.finetune = ft
		if ch.note >= 0 && p.mod.Format == FormatMOD {
			ch.period = modPeriod(ch.note, ft)
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
	case effRetrig:
		if s3m {
			ch.retrig = p.param(ch, x, &ch.retrig)
		} else {
			ch.retrig = x
		}
	case effFineVolUp:
		ch.volume = min(ch.volume+int(x), 64)
		ch.outVolume = ch.volume
	case effFineVolDown:
		ch.volume = max(ch.volume-int(x), 0)
		ch.outVolume = ch.volume
	case effNoteCut:
		if x == 0 {
			ch.volume, ch.outVolume = 0, 0
		} else {
			ch.noteCut = int(x)
		}
	case effPatDelay:
		if p.patDelay == 0 {
			p.patDelay = int(x)
		}
	case effTremor:
		ch.tremorParam = p.param(ch, x, &ch.tremorParam)
	case effGlobalVol:
		p.globalVol = min(int(x), 64)
	}
}

// param returns the effective parameter, remembering non-zero values.
func (p *Player) param(ch *channel, x byte, mem *byte) byte {
	if x != 0 {
		*mem = x
	}
	return *mem
}

// portaUnit is one unit of portamento in the format's period space.
func (p *Player) portaUnit() float64 {
	if p.mod.Format == FormatS3M {
		return 4
	}
	return 1
}

// s3mFineVolSlide applies the tick-0 half of a ScreamTracker Dxy.
func (p *Player) s3mFineVolSlide(ch *channel) {
	x, y := ch.volSlide>>4, ch.volSlide&0x0F
	switch {
	case y == 0x0F && x != 0:
		ch.volume = min(ch.volume+int(x), 64)
	case x == 0x0F && y != 0:
		ch.volume = max(ch.volume-int(y), 0)
	}
	ch.outVolume = ch.volume
}
