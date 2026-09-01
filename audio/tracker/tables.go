package tracker

import "math"

// ProTracker periods for octave 1 (C-1 .. B-1) at finetune 0.
var ptPeriods = [12]float64{856, 808, 762, 720, 678, 640, 604, 570, 538, 508, 480, 453}

// ScreamTracker octave-0 periods, shifted right by the octave.
var st3Periods = [12]int{1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907}

// sineTable is the classic 32-entry half-sine used by vibrato and tremolo.
var sineTable = [32]int{
	0, 24, 49, 74, 97, 120, 141, 161, 180, 197, 212, 224, 235, 244, 250, 253,
	255, 253, 250, 244, 235, 224, 212, 197, 180, 161, 141, 120, 97, 74, 49, 24,
}

// waveform returns the oscillator value in -255..255 for position 0..63.
func waveform(kind int, pos int) int {
	pos &= 63
	switch kind & 3 {
	case 1: // ramp down
		return 255 - (pos&63)*8
	case 2: // square
		if pos < 32 {
			return 255
		}
		return -255
	}
	v := sineTable[pos&31]
	if pos >= 32 {
		return -v
	}
	return v
}

// modPeriod is the Amiga period for a note index (octave*12+semi, with
// octave 1 being ProTracker's lowest) at the given finetune.
func modPeriod(note int, finetune int) float64 {
	octave, semi := note/12, note%12
	p := ptPeriods[semi] * math.Pow(2, -float64(finetune)/96)
	switch {
	case octave >= 1:
		p /= float64(int(1) << (octave - 1))
	default:
		p *= 2
	}
	return p
}

// modNoteFromPeriod finds the note index nearest an Amiga period.
func modNoteFromPeriod(period int) int {
	best, bestDiff := -1, math.MaxFloat64
	for note := range 60 {
		if d := math.Abs(modPeriod(note, 0) - float64(period)); d < bestDiff {
			best, bestDiff = note, d
		}
	}
	return best
}

// s3mPeriod is the ScreamTracker period for a note index and C4 speed.
func s3mPeriod(note int, c4speed int) float64 {
	if c4speed <= 0 {
		c4speed = 8363
	}
	octave, semi := note/12, note%12
	return 8363 * 16 * float64(st3Periods[semi]>>octave) / float64(c4speed)
}

// frequency converts a period to a sample rate for the format.
func frequency(f Format, period float64) float64 {
	if period <= 0 {
		return 0
	}
	if f == FormatMOD {
		return 7093789.2 / (period * 2)
	}
	return 14317456 / period
}
