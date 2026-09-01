package tracker

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// LoadIT parses an Impulse Tracker module, including compressed samples.
func LoadIT(data []byte) (*Module, error) {
	if len(data) < 192 || string(data[:4]) != "IMPM" {
		return nil, fmt.Errorf("it: missing IMPM signature")
	}
	le := binary.LittleEndian
	ordNum := int(le.Uint16(data[32:]))
	insNum := int(le.Uint16(data[34:]))
	smpNum := int(le.Uint16(data[36:]))
	patNum := int(le.Uint16(data[38:]))
	cwtv := le.Uint16(data[40:])
	flags := le.Uint16(data[44:])
	m := &Module{
		Title:        strings.TrimRight(string(data[4:30]), "\x00 "),
		Format:       FormatIT,
		GlobalVolume: min(int(data[48]), 128),
		MixVolume:    min(int(data[49]), 128),
		Speed:        int(data[50]),
		Tempo:        int(data[51]),
		LinearSlides: flags&8 != 0,
		OldEffects:   flags&16 != 0,
		CompatGxx:    flags&32 != 0,
	}
	if m.Speed == 0 {
		m.Speed = 6
	}
	if m.Tempo < 32 {
		m.Tempo = 125
	}
	stereo := flags&1 != 0
	useInstruments := flags&4 != 0
	// Channel panning and volume for the 64 slots; a slot with bit 7 set is disabled.
	m.Channels = 64
	m.Pan = make([]float32, 64)
	m.ChannelVol = make([]int, 64)
	for c := range 64 {
		pan := data[64+c]
		switch {
		case pan >= 128:
			m.Pan[c] = 0
		case pan == 100:
			m.Pan[c] = 0 // surround
		default:
			m.Pan[c] = float32(min(int(pan), 64))/32 - 1
		}
		if !stereo {
			m.Pan[c] = 0
		}
		m.ChannelVol[c] = min(int(data[128+c]), 64)
	}
	off := 192
	for i := range ordNum {
		if off+i >= len(data) {
			break
		}
		if o := int(data[off+i]); o < 254 {
			m.Orders = append(m.Orders, o)
		}
	}
	off += ordNum
	readPtrs := func(n int) []int {
		ptrs := make([]int, 0, n)
		for i := range n {
			if off+4*i+4 > len(data) {
				break
			}
			ptrs = append(ptrs, int(le.Uint32(data[off+4*i:])))
		}
		off += 4 * n
		return ptrs
	}
	insPtrs := readPtrs(insNum)
	smpPtrs := readPtrs(smpNum)
	patPtrs := readPtrs(patNum)
	for i, p := range smpPtrs {
		s, err := loadITSample(data, p, cwtv)
		if err != nil {
			return nil, fmt.Errorf("it: sample %d: %w", i+1, err)
		}
		m.Samples = append(m.Samples, s)
	}
	if useInstruments {
		for i, p := range insPtrs {
			inst, err := loadITInstrument(data, p)
			if err != nil {
				return nil, fmt.Errorf("it: instrument %d: %w", i+1, err)
			}
			m.Instruments = append(m.Instruments, inst)
		}
	}
	maxChan := 0
	for i, p := range patPtrs {
		pat, used, err := loadITPattern(data, p)
		if err != nil {
			return nil, fmt.Errorf("it: pattern %d: %w", i, err)
		}
		m.Patterns = append(m.Patterns, pat)
		maxChan = max(maxChan, used)
	}
	// Keep only the channels the song uses (patterns are stored 64 wide).
	m.Channels = max(maxChan, 1)
	m.Pan = m.Pan[:m.Channels]
	m.ChannelVol = m.ChannelVol[:m.Channels]
	for i := range m.Patterns {
		for r := range m.Patterns[i].Rows {
			m.Patterns[i].Rows[r] = m.Patterns[i].Rows[r][:m.Channels]
		}
	}
	return m, nil
}

func loadITSample(data []byte, p int, cwtv uint16) (Sample, error) {
	le := binary.LittleEndian
	if p == 0 || p+80 > len(data) || string(data[p:p+4]) != "IMPS" {
		return Sample{Volume: 64, GlobalVolume: 64, C4Speed: 8363}, nil
	}
	h := data[p:]
	flags := h[18]
	s := Sample{
		Name:         strings.TrimRight(string(h[20:46]), "\x00 "),
		GlobalVolume: min(int(h[17]), 64),
		Volume:       min(int(h[19]), 64),
		C4Speed:      int(le.Uint32(h[60:])),
		Vibrato:      AutoVibrato{Rate: int(h[76]), Depth: int(h[77]), Sweep: int(h[78]), Type: int(h[79])},
	}
	if dfp := h[47]; dfp&0x80 != 0 {
		s.HasPan = true
		s.Pan = float32(min(int(dfp&0x7F), 64))/32 - 1
	}
	length := int(le.Uint32(h[48:]))
	loopBegin := int(le.Uint32(h[52:]))
	loopEnd := int(le.Uint32(h[56:]))
	susBegin := int(le.Uint32(h[64:]))
	susEnd := int(le.Uint32(h[68:]))
	ptr := int(le.Uint32(h[72:]))
	if flags&1 == 0 || length == 0 || ptr == 0 || ptr >= len(data) {
		return s, nil
	}
	sixteen := flags&2 != 0
	stereo := flags&4 != 0
	signed := h[46]&1 != 0
	channels := 1
	if stereo {
		channels = 2
	}
	var samples []float32
	if flags&8 != 0 {
		decoded, err := itDecompress(data[ptr:], length*channels, sixteen, cwtv >= 0x215)
		if err != nil {
			return s, err
		}
		samples = decoded
	} else {
		bytesPer := 1
		if sixteen {
			bytesPer = 2
		}
		n := min(length*channels, (len(data)-ptr)/bytesPer)
		samples = make([]float32, n)
		for i := range n {
			if sixteen {
				v := le.Uint16(data[ptr+i*2:])
				if signed {
					samples[i] = float32(int16(v)) / 32768
				} else {
					samples[i] = (float32(v) - 32768) / 32768
				}
			} else {
				v := data[ptr+i]
				if signed {
					samples[i] = float32(int8(v)) / 128
				} else {
					samples[i] = (float32(v) - 128) / 128
				}
			}
		}
	}
	if stereo { // IT stores stereo as two mono blocks; average them
		half := len(samples) / 2
		mono := make([]float32, half)
		for i := range half {
			mono[i] = (samples[i] + samples[half+i]) / 2
		}
		samples = mono
	}
	s.Data = samples
	n := len(samples)
	if flags&16 != 0 && loopEnd > loopBegin {
		s.LoopStart, s.LoopEnd, s.Loop = min(loopBegin, n), min(loopEnd, n), LoopForward
		if flags&64 != 0 {
			s.Loop = LoopPingPong
		}
	}
	if flags&32 != 0 && susEnd > susBegin {
		s.SusLoopStart, s.SusLoopEnd, s.SusLoop = min(susBegin, n), min(susEnd, n), LoopForward
		if flags&128 != 0 {
			s.SusLoop = LoopPingPong
		}
	}
	return s, nil
}

func loadITInstrument(data []byte, p int) (Instrument, error) {
	le := binary.LittleEndian
	inst := Instrument{GlobalVolume: 128, FilterCutoff: -1, FilterResonance: -1}
	for i := range inst.SampleMap {
		inst.SampleMap[i] = -1
		inst.NoteMap[i] = i
	}
	if p == 0 || p+554 > len(data) || string(data[p:p+4]) != "IMPI" {
		return inst, nil
	}
	h := data[p:]
	inst.Name = strings.TrimRight(string(h[32:58]), "\x00 ")
	inst.NNA = NNA(min(h[17], 3))
	inst.DCT = int(h[18])
	inst.DCA = int(h[19])
	inst.Fadeout = int(le.Uint16(h[20:])) * 64
	inst.GlobalVolume = min(int(h[24]), 128)
	if h[25]&0x80 == 0 {
		inst.HasPan = true
		inst.Pan = float32(min(int(h[25]&0x7F), 64))/32 - 1
	}
	if h[58]&0x80 != 0 {
		inst.FilterCutoff = int(h[58] & 0x7F)
	}
	if h[59]&0x80 != 0 {
		inst.FilterResonance = int(h[59] & 0x7F)
	}
	for n := range 120 {
		note := int(h[64+n*2])
		smp := int(h[65+n*2])
		if note < 120 {
			inst.NoteMap[n] = note
		}
		inst.SampleMap[n] = smp - 1 // 0 means no sample
	}
	readEnv := func(at int, signed bool) (Envelope, bool) {
		e := h[at:]
		flags := e[0]
		env := Envelope{Enabled: flags&1 != 0, Loop: flags&2 != 0, Sustain: flags&4 != 0,
			LoopStart: int(e[2]), LoopEnd: int(e[3]), SustainStart: int(e[4]), SustainEnd: int(e[5])}
		num := min(int(e[1]), 25)
		for i := range num {
			val := float32(e[6+i*3])
			if signed {
				val = float32(int8(e[6+i*3]))
			}
			env.Points = append(env.Points, EnvPoint{Tick: int(le.Uint16(e[7+i*3:])), Value: val})
		}
		if len(env.Points) == 0 {
			env.Enabled = false
		}
		return env, flags&0x80 != 0
	}
	inst.VolEnv, _ = readEnv(304, false)
	inst.PanEnv, _ = readEnv(386, true)
	inst.PitchEnv, inst.PitchIsFilter = readEnv(468, true)
	return inst, nil
}

// loadITPattern decodes one packed pattern and reports the highest channel used.
func loadITPattern(data []byte, p int) (Pattern, int, error) {
	le := binary.LittleEndian
	if p == 0 {
		pat := Pattern{Rows: make([][]Cell, 64)}
		for r := range pat.Rows {
			pat.Rows[r] = emptyRow(64)
		}
		return pat, 0, nil
	}
	if p+8 > len(data) {
		return Pattern{}, 0, fmt.Errorf("pattern pointer past end of file")
	}
	length := int(le.Uint16(data[p:]))
	rows := int(le.Uint16(data[p+2:]))
	pat := Pattern{Rows: make([][]Cell, rows)}
	for r := range pat.Rows {
		pat.Rows[r] = emptyRow(64)
	}
	var lastMask [64]byte
	var lastNote, lastIns, lastVol, lastCmd, lastParam [64]byte
	used := 0
	off := p + 8
	end := min(p+8+length, len(data))
	for r := 0; r < rows && off < end; {
		b := data[off]
		off++
		if b == 0 {
			r++
			continue
		}
		c := int(b-1) & 63
		mask := lastMask[c]
		if b&128 != 0 {
			if off >= end {
				break
			}
			mask = data[off]
			off++
			lastMask[c] = mask
		}
		cell := &pat.Rows[r][c]
		used = max(used, c+1)
		if mask&1 != 0 {
			if off >= end {
				break
			}
			lastNote[c] = data[off]
			off++
		}
		if mask&2 != 0 {
			if off >= end {
				break
			}
			lastIns[c] = data[off]
			off++
		}
		if mask&4 != 0 {
			if off >= end {
				break
			}
			lastVol[c] = data[off]
			off++
		}
		if mask&8 != 0 {
			if off+1 >= end {
				break
			}
			lastCmd[c], lastParam[c] = data[off], data[off+1]
			off += 2
		}
		if mask&(1|16) != 0 {
			cell.Note = itNote(lastNote[c])
		}
		if mask&(2|32) != 0 {
			cell.Instrument = int(lastIns[c])
		}
		if mask&(4|64) != 0 {
			cell.VolCmd, cell.VolParam = itVolume(lastVol[c])
		}
		if mask&(8|128) != 0 {
			cell.Effect, cell.Param = s3mEffect(lastCmd[c], lastParam[c])
		}
	}
	return pat, used, nil
}

func emptyRow(n int) []Cell {
	cells := make([]Cell, n)
	for i := range cells {
		cells[i] = Cell{Note: NoteNone}
	}
	return cells
}

func itNote(n byte) int {
	switch {
	case n < 120:
		return int(n)
	case n == 255:
		return NoteOff
	case n == 254:
		return NoteCut
	}
	return NoteFade
}

var itPortaTable = [10]int{0, 1, 4, 8, 16, 32, 64, 96, 128, 255}

// itVolume decodes the volume/panning column byte.
func itVolume(v byte) (volCmd, int) {
	x := int(v)
	switch {
	case x <= 64:
		return volSet, x
	case x <= 74:
		return volFineUp, x - 65
	case x <= 84:
		return volFineDown, x - 75
	case x <= 94:
		return volSlideUp, x - 85
	case x <= 104:
		return volSlideDown, x - 95
	case x <= 114:
		return volPortaDown, x - 105
	case x <= 124:
		return volPortaUp, x - 115
	case x >= 128 && x <= 192:
		return volPan, x - 128
	case x >= 193 && x <= 202:
		return volTonePorta, itPortaTable[x-193]
	case x >= 203 && x <= 212:
		return volVibrato, x - 203
	}
	return volNone, 0
}
