package tracker

// effect is the format-independent effect vocabulary. Loaders map their
// own command letters onto it; the player interprets it with the module's
// Format where semantics differ.
type effect uint8

const (
	effNone effect = iota
	effArpeggio
	effPortaUp
	effPortaDown
	effTonePorta
	effVibrato
	effTonePortaVol
	effVibratoVol
	effTremolo
	effSetPan   // param 0..255
	effOffset   // param * 256
	effVolSlide // MOD Axy / S3M Dxy
	effPosJump
	effSetVolume
	effPatBreak // param already decoded from BCD
	effSetSpeed
	effSetTempo
	effFinePortaUp
	effFinePortaDown
	effExtraFinePortaUp
	effExtraFinePortaDown
	effVibWave
	effFinetune
	effPatLoop
	effTremWave
	effRetrig // MOD E9x: every x ticks; S3M Qxy: x volume change, y ticks
	effFineVolUp
	effFineVolDown
	effNoteCut
	effNoteDelay
	effPatDelay
	effTremor
	effGlobalVol
	effFineVibrato
	effSetPanCoarse // param 0..15
)

// modEffect maps a ProTracker command and parameter.
func modEffect(cmd, param byte) (effect, byte) {
	switch cmd {
	case 0x0:
		if param != 0 {
			return effArpeggio, param
		}
		return effNone, 0
	case 0x1:
		return effPortaUp, param
	case 0x2:
		return effPortaDown, param
	case 0x3:
		return effTonePorta, param
	case 0x4:
		return effVibrato, param
	case 0x5:
		return effTonePortaVol, param
	case 0x6:
		return effVibratoVol, param
	case 0x7:
		return effTremolo, param
	case 0x8:
		return effSetPan, param
	case 0x9:
		return effOffset, param
	case 0xA:
		return effVolSlide, param
	case 0xB:
		return effPosJump, param
	case 0xC:
		return effSetVolume, param
	case 0xD:
		return effPatBreak, (param>>4)*10 + param&0x0F
	case 0xE:
		sub, x := param>>4, param&0x0F
		return map[byte]effect{
			0x1: effFinePortaUp, 0x2: effFinePortaDown, 0x4: effVibWave, 0x5: effFinetune,
			0x6: effPatLoop, 0x7: effTremWave, 0x8: effSetPanCoarse, 0x9: effRetrig,
			0xA: effFineVolUp, 0xB: effFineVolDown, 0xC: effNoteCut, 0xD: effNoteDelay, 0xE: effPatDelay,
		}[sub], x
	case 0xF:
		if param < 32 {
			return effSetSpeed, param
		}
		return effSetTempo, param
	}
	return effNone, 0
}

// s3mEffect maps a ScreamTracker command (1 = A) and parameter.
func s3mEffect(cmd, param byte) (effect, byte) {
	switch cmd + '@' {
	case 'A':
		return effSetSpeed, param
	case 'B':
		return effPosJump, param
	case 'C':
		return effPatBreak, (param>>4)*10 + param&0x0F
	case 'D':
		return effVolSlide, param
	case 'E':
		switch {
		case param >= 0xF0:
			return effFinePortaDown, param & 0x0F
		case param >= 0xE0:
			return effExtraFinePortaDown, param & 0x0F
		}
		return effPortaDown, param
	case 'F':
		switch {
		case param >= 0xF0:
			return effFinePortaUp, param & 0x0F
		case param >= 0xE0:
			return effExtraFinePortaUp, param & 0x0F
		}
		return effPortaUp, param
	case 'G':
		return effTonePorta, param
	case 'H':
		return effVibrato, param
	case 'I':
		return effTremor, param
	case 'J':
		return effArpeggio, param
	case 'K':
		return effVibratoVol, param
	case 'L':
		return effTonePortaVol, param
	case 'O':
		return effOffset, param
	case 'Q':
		return effRetrig, param
	case 'R':
		return effTremolo, param
	case 'S':
		sub, x := param>>4, param&0x0F
		return map[byte]effect{
			0x2: effFinetune, 0x3: effVibWave, 0x4: effTremWave, 0x8: effSetPanCoarse,
			0xB: effPatLoop, 0xC: effNoteCut, 0xD: effNoteDelay, 0xE: effPatDelay,
		}[sub], x
	case 'T':
		return effSetTempo, param
	case 'U':
		return effFineVibrato, param
	case 'V':
		return effGlobalVol, param
	case 'X':
		if param == 0xA4 {
			return effSetPan, 128 // surround: treat as centre
		}
		return effSetPan, byte(min(int(param)*2, 255))
	}
	return effNone, 0
}
