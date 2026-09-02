package tracker

// processRow handles tick 0 of a row: note and instrument triggers, the
// volume column, and the effects that act once per row.
func (p *Player) processRow() {
	cells := p.currentRow()
	p.rows = len(p.mod.Patterns[p.mod.Orders[p.order]].Rows)
	for i := range p.chans {
		ch := &p.chans[i]
		cell := cells[i]
		ch.noteCut, ch.noteDelay, ch.keyOffTick = -1, -1, -1
		ch.retrigCount = 0
		if cell.Effect == effNoteDelay && cell.Param > 0 {
			ch.noteDelay = int(cell.Param)
			ch.delayed = cell
			ch.outPeriod, ch.outVolume, ch.outPan = ch.period, ch.volume, ch.pan
			continue
		}
		p.triggerCell(ch, cell)
		p.volumeColumnRow(ch, cell)
		p.rowEffect(ch, cell)
		ch.outPeriod, ch.outVolume, ch.outPan = ch.period, ch.volume, ch.voice.pan
	}
}

// triggerCell applies the instrument and note of a cell to the channel.
func (p *Player) triggerCell(ch *channel, cell Cell) {
	m := p.mod
	porta := cell.Effect == effTonePorta || cell.Effect == effTonePortaVol || cell.VolCmd == volTonePorta
	hasNote := cell.Note >= 0 && cell.Note < NoteOff
	if cell.Instrument > 0 {
		ch.lastInst = cell.Instrument
	}
	switch cell.Note {
	case NoteOff:
		p.keyOff(&ch.voice)
		if cell.Instrument > 0 && m.Format == FormatIT {
			ch.volume = p.sampleVolume(ch.lastInst, ch.lastNote)
		}
		return
	case NoteCut:
		ch.active = false
		ch.volume = 0
		return
	case NoteFade:
		ch.fading = true
		return
	}
	note := cell.Note
	if hasNote {
		ch.lastNote = note
	} else {
		note = ch.lastNote
	}
	sample, inst := p.resolve(ch.lastInst, note)
	if cell.Instrument > 0 && sample != nil {
		// An instrument number resets volume and pan; the sample itself only
		// changes on a new note (ProTracker swaps it when the loop wraps).
		ch.volume = sample.Volume
		if inst != nil && p.mod.Format == FormatIT {
			ch.volume = sample.Volume
		}
		if sample.HasPan && (hasNote || m.Format != FormatMOD) {
			ch.voice.pan = sample.Pan
		}
		if inst != nil && inst.HasPan {
			ch.voice.pan = inst.Pan
		}
		if !hasNote && m.Format == FormatMOD && ch.sample != nil && ch.sample != sample {
			ch.pendingSample = sample
		}
		if !hasNote && m.Format != FormatMOD {
			// XM/IT: instrument without note restarts envelopes but keeps the sample playing.
			p.resetEnvelopes(&ch.voice, inst)
		}
	}
	if !hasNote || sample == nil {
		return
	}
	if inst != nil && inst.NoteMap[min(max(note, 0), 119)] >= 0 {
		note = inst.NoteMap[min(max(note, 0), 119)]
	}
	if porta && ch.active {
		// Tone portamento: aim at the new note without retriggering.
		if cell.Instrument > 0 && m.Format != FormatMOD {
			p.resetEnvelopes(&ch.voice, inst)
		}
		ch.note = note
		v := voice{sample: ch.sample}
		if ch.sample == nil {
			v.sample = sample
			ch.sample = sample
		}
		ch.portaTarget = p.notePeriod(&v, note)
		return
	}
	p.startNote(ch, sample, inst, note, cell.Instrument > 0)
}

// startNote begins a fresh voice on the channel, applying the instrument's
// new-note action to whatever was playing.
func (p *Player) startNote(ch *channel, sample *Sample, inst *Instrument, note int, withInst bool) {
	if ch.active && inst != nil && p.mod.Format == FormatIT {
		p.applyNNA(ch, inst, sample, note)
	}
	old := ch.voice
	ch.voice = voice{
		sample: sample, inst: inst, active: len(sample.Data) > 0, dir: 1, index: ch.index,
		note: note, volume: ch.volume, pan: ch.voice.pan, fade: 65536,
		cutoff: ch.cutoff, resonance: ch.resonance, filterOn: ch.filterOn, sustainOn: true,
	}
	if !withInst {
		ch.volume = old.volume
		ch.voice.volume = old.volume
	}
	if inst != nil {
		if inst.FilterCutoff >= 0 {
			ch.voice.cutoff, ch.voice.filterOn = inst.FilterCutoff, true
		}
		if inst.FilterResonance >= 0 {
			ch.voice.resonance, ch.voice.filterOn = inst.FilterResonance, true
		}
	}
	ch.period = p.notePeriod(&ch.voice, note)
	ch.portaTarget = ch.period
	ch.pendingSample = nil
	if ch.vibWave&4 == 0 {
		ch.vibPos = 0
	}
	if ch.tremWave&4 == 0 {
		ch.tremPos = 0
	}
	ch.tremorCount, ch.tremorOff = 0, false
	p.resetEnvelopes(&ch.voice, inst)
}

func (p *Player) resetEnvelopes(v *voice, inst *Instrument) {
	v.volEnv, v.panEnv, v.pitchEnv = envState{}, envState{}, envState{}
	v.keyOff, v.fading, v.fade = false, false, 65536
	v.autoVib = autoVibState{}
	if inst != nil {
		v.inst = inst
	}
}

// keyOff releases the voice: envelopes leave sustain and fading may start.
func (p *Player) keyOff(v *voice) {
	v.keyOff = true
	v.sustainOn = false
	if v.inst == nil {
		v.active = false // sample-only formats have nothing to release
	}
}

// resolve finds the sample and instrument for an instrument number and note.
func (p *Player) resolve(instNum, note int) (*Sample, *Instrument) {
	m := p.mod
	if instNum <= 0 {
		return nil, nil
	}
	if len(m.Instruments) == 0 {
		if instNum > len(m.Samples) {
			return nil, nil
		}
		return &m.Samples[instNum-1], nil
	}
	if instNum > len(m.Instruments) {
		return nil, nil
	}
	inst := &m.Instruments[instNum-1]
	n := min(max(note, 0), 119)
	si := inst.SampleMap[n]
	if si < 0 || si >= len(m.Samples) {
		return nil, inst
	}
	return &m.Samples[si], inst
}

func (p *Player) sampleVolume(instNum, note int) int {
	s, _ := p.resolve(instNum, note)
	if s == nil {
		return 0
	}
	return s.Volume
}

// applyNNA moves the channel's current voice to the background according to
// the instrument's new-note action, after duplicate checks.
func (p *Player) applyNNA(ch *channel, inst *Instrument, sample *Sample, note int) {
	action := inst.NNA
	if old := ch.inst; old != nil && old.DCT != 0 && old == inst {
		dup := false
		switch old.DCT {
		case 1:
			dup = ch.note == note
		case 2:
			dup = ch.sample == sample
		case 3:
			dup = true
		}
		if dup {
			switch old.DCA {
			case 0:
				action = NNACut
			case 1:
				action = NNAOff
			case 2:
				action = NNAFade
			}
		}
	}
	if action == NNACut {
		return
	}
	if len(p.bg) >= 64 {
		return // the background is full; the oldest notes simply cut
	}
	bg := p.takeVoice()
	*bg = ch.voice
	switch action {
	case NNAOff:
		p.keyOff(bg)
	case NNAFade:
		bg.fading = true
	}
	p.bg = append(p.bg, bg)
}

// volumeColumnRow applies the tick-0 part of an XM/IT volume column.
func (p *Player) volumeColumnRow(ch *channel, cell Cell) {
	x := cell.VolParam
	switch cell.VolCmd {
	case volSet:
		ch.volume = min(x, 64)
	case volFineUp:
		ch.volume = min(ch.volume+x, 64)
	case volFineDown:
		ch.volume = max(ch.volume-x, 0)
	case volVibratoSpeed:
		ch.vibSpeed = x
	case volVibratoDepth, volVibrato:
		if x != 0 {
			ch.vibDepth = x
		}
	case volPan:
		ch.voice.pan = float32(x)/32 - 1
	case volTonePorta:
		if x != 0 {
			ch.portaSpeed = x
		}
	}
}
