package tracker

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// LoadXM parses a FastTracker 2 module.
func LoadXM(data []byte) (*Module, error) {
	if len(data) < 336 || string(data[:17]) != "Extended Module: " {
		return nil, fmt.Errorf("xm: missing signature")
	}
	le := binary.LittleEndian
	headerSize := int(le.Uint32(data[60:]))
	if headerSize < 20 || 60+headerSize > len(data) {
		return nil, fmt.Errorf("xm: bad header size %d", headerSize)
	}
	songLen := int(le.Uint16(data[64:]))
	restart := int(le.Uint16(data[66:]))
	channels := int(le.Uint16(data[68:]))
	patNum := int(le.Uint16(data[70:]))
	insNum := int(le.Uint16(data[72:]))
	flags := le.Uint16(data[74:])
	m := &Module{
		Title:        strings.TrimRight(string(data[17:37]), "\x00 "),
		Channels:     channels,
		Speed:        int(le.Uint16(data[76:])),
		Tempo:        int(le.Uint16(data[78:])),
		Format:       FormatXM,
		LinearSlides: flags&1 != 0,
		GlobalVolume: 128,
		Restart:      restart,
	}
	if channels <= 0 || channels > 64 {
		return nil, fmt.Errorf("xm: %d channels", channels)
	}
	if m.Speed == 0 {
		m.Speed = 6
	}
	if m.Tempo < 32 {
		m.Tempo = 125
	}
	for i := 0; i < songLen && 80+i < 60+headerSize; i++ {
		m.Orders = append(m.Orders, int(data[80+i]))
	}
	if m.Restart >= len(m.Orders) {
		m.Restart = 0
	}
	m.Pan = make([]float32, channels)
	off := 60 + headerSize
	for i := range patNum {
		pat, next, err := loadXMPattern(data, off, channels)
		if err != nil {
			return nil, fmt.Errorf("xm: pattern %d: %w", i, err)
		}
		m.Patterns = append(m.Patterns, pat)
		off = next
	}
	for i := range insNum {
		next, err := loadXMInstrument(m, data, off)
		if err != nil {
			return nil, fmt.Errorf("xm: instrument %d: %w", i+1, err)
		}
		off = next
	}
	return m, nil
}

func loadXMPattern(data []byte, off, channels int) (Pattern, int, error) {
	le := binary.LittleEndian
	if off+9 > len(data) {
		return Pattern{}, off, fmt.Errorf("truncated header")
	}
	hdrLen := int(le.Uint32(data[off:]))
	rows := int(le.Uint16(data[off+5:]))
	packed := int(le.Uint16(data[off+7:]))
	if rows == 0 {
		rows = 64
	}
	pat := Pattern{Rows: make([][]Cell, rows)}
	for r := range pat.Rows {
		cells := make([]Cell, channels)
		for c := range cells {
			cells[c] = Cell{Note: NoteNone}
		}
		pat.Rows[r] = cells
	}
	p := off + hdrLen
	end := min(p+packed, len(data))
	for r := 0; r < rows && p < end; r++ {
		for c := 0; c < channels && p < end; c++ {
			var note, inst, vol, eff, param byte
			b := data[p]
			p++
			if b&0x80 != 0 {
				if b&1 != 0 && p < end {
					note = data[p]
					p++
				}
				if b&2 != 0 && p < end {
					inst = data[p]
					p++
				}
				if b&4 != 0 && p < end {
					vol = data[p]
					p++
				}
				if b&8 != 0 && p < end {
					eff = data[p]
					p++
				}
				if b&16 != 0 && p < end {
					param = data[p]
					p++
				}
			} else {
				if p+4 > end {
					break
				}
				note, inst, vol, eff, param = b, data[p], data[p+1], data[p+2], data[p+3]
				p += 4
			}
			cell := &pat.Rows[r][c]
			switch {
			case note == 97:
				cell.Note = NoteOff
			case note >= 1 && note <= 96:
				cell.Note = int(note) - 1
			}
			cell.Instrument = int(inst)
			cell.VolCmd, cell.VolParam = xmVolume(vol)
			cell.Effect, cell.Param = xmEffect(eff, param)
		}
	}
	return pat, off + hdrLen + packed, nil
}

// xmVolume decodes the volume column byte.
func xmVolume(v byte) (volCmd, int) {
	x := int(v & 0x0F)
	switch {
	case v >= 0x10 && v <= 0x50:
		return volSet, int(v) - 0x10
	case v >= 0x60 && v <= 0x6F:
		return volSlideDown, x
	case v >= 0x70 && v <= 0x7F:
		return volSlideUp, x
	case v >= 0x80 && v <= 0x8F:
		return volFineDown, x
	case v >= 0x90 && v <= 0x9F:
		return volFineUp, x
	case v >= 0xA0 && v <= 0xAF:
		return volVibratoSpeed, x
	case v >= 0xB0 && v <= 0xBF:
		return volVibratoDepth, x
	case v >= 0xC0 && v <= 0xCF:
		return volPan, x * 64 / 15
	case v >= 0xD0 && v <= 0xDF:
		return volPanSlideLeft, x
	case v >= 0xE0 && v <= 0xEF:
		return volPanSlideRight, x
	case v >= 0xF0:
		return volTonePorta, x * 16
	}
	return volNone, 0
}

func loadXMInstrument(m *Module, data []byte, off int) (int, error) {
	le := binary.LittleEndian
	if off+29 > len(data) {
		return off, fmt.Errorf("truncated")
	}
	size := int(le.Uint32(data[off:]))
	inst := Instrument{Name: strings.TrimRight(string(data[off+4:off+26]), "\x00 "), GlobalVolume: 128, FilterCutoff: -1, FilterResonance: -1}
	for i := range inst.SampleMap {
		inst.SampleMap[i] = -1
		inst.NoteMap[i] = i
	}
	numSamples := int(le.Uint16(data[off+27:]))
	if numSamples == 0 || size < 243 {
		m.Instruments = append(m.Instruments, inst)
		return off + size, nil
	}
	h := data[off:]
	sampleHeaderSize := int(le.Uint32(h[29:]))
	base := len(m.Samples)
	for n := range 96 {
		if si := int(h[33+n]); si < numSamples {
			inst.SampleMap[n] = base + si
		}
	}
	readEnv := func(at int, count int, sustain, loopStart, loopEnd int, typ byte, pan bool) Envelope {
		e := Envelope{Enabled: typ&1 != 0, Sustain: typ&2 != 0, Loop: typ&4 != 0,
			SustainStart: sustain, SustainEnd: sustain, LoopStart: loopStart, LoopEnd: loopEnd}
		for i := range min(count, 12) {
			tick := int(le.Uint16(h[at+i*4:]))
			val := float32(le.Uint16(h[at+i*4+2:]))
			if pan {
				val -= 32
			}
			e.Points = append(e.Points, EnvPoint{Tick: tick, Value: val})
		}
		if len(e.Points) == 0 {
			e.Enabled = false
		}
		return e
	}
	inst.VolEnv = readEnv(129, int(h[225]), int(h[227]), int(h[228]), int(h[229]), h[233], false)
	inst.PanEnv = readEnv(177, int(h[226]), int(h[230]), int(h[231]), int(h[232]), h[234], true)
	vib := AutoVibrato{Type: int(h[235]), Sweep: int(h[236]), Depth: int(h[237]), Rate: int(h[238])}
	inst.Fadeout = int(le.Uint16(h[239:])) * 2
	m.Instruments = append(m.Instruments, inst)

	off += size
	type sh struct {
		length, loopStart, loopLen int
		sixteen, pingpong, looped  bool
	}
	headers := make([]sh, numSamples)
	for i := range numSamples {
		if off+40 > len(data) {
			return off, fmt.Errorf("sample header %d truncated", i)
		}
		s := data[off:]
		typ := s[14]
		ft := int(int8(s[13]))
		headers[i] = sh{
			length: int(le.Uint32(s[0:])), loopStart: int(le.Uint32(s[4:])), loopLen: int(le.Uint32(s[8:])),
			sixteen: typ&16 != 0, pingpong: typ&3 == 2, looped: typ&3 != 0,
		}
		m.Samples = append(m.Samples, Sample{
			Name: strings.TrimRight(string(s[18:40]), "\x00 "), Volume: min(int(s[12]), 64), GlobalVolume: 64,
			Finetune: float32(ft) / 128, RelativeNote: int(int8(s[16])), C4Speed: 8363,
			Pan: float32(s[15])/127.5 - 1, HasPan: true, Vibrato: vib,
		})
		off += sampleHeaderSize
	}
	for i, h := range headers {
		s := &m.Samples[base+i]
		bytesPer := 1
		if h.sixteen {
			bytesPer = 2
		}
		count := h.length / bytesPer
		end := min(off+h.length, len(data))
		count = min(count, (end-off)/bytesPer)
		s.Data = make([]float32, count)
		var acc int32
		for j := range count {
			if h.sixteen {
				acc += int32(int16(le.Uint16(data[off+j*2:])))
				s.Data[j] = float32(int16(acc)) / 32768
			} else {
				acc += int32(int8(data[off+j]))
				s.Data[j] = float32(int8(acc)) / 128
			}
		}
		if h.looped && h.loopLen/bytesPer > 1 {
			s.LoopStart = min(h.loopStart/bytesPer, count)
			s.LoopEnd = min((h.loopStart+h.loopLen)/bytesPer, count)
			s.Loop = LoopForward
			if h.pingpong {
				s.Loop = LoopPingPong
			}
		}
		off = end
	}
	return off, nil
}
