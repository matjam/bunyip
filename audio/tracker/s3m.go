package tracker

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// LoadS3M parses a ScreamTracker 3 module. Adlib instruments are ignored.
func LoadS3M(data []byte) (*Module, error) {
	if len(data) < 96 || string(data[44:48]) != "SCRM" {
		return nil, fmt.Errorf("s3m: missing SCRM signature")
	}
	le := binary.LittleEndian
	ordNum := int(le.Uint16(data[32:]))
	insNum := int(le.Uint16(data[34:]))
	patNum := int(le.Uint16(data[36:]))
	ffi := le.Uint16(data[42:]) // 1 signed samples, 2 unsigned
	m := &Module{
		Title:        strings.TrimRight(string(data[:28]), "\x00 "),
		GlobalVolume: min(int(data[48]), 64) * 2,
		Speed:        int(data[49]),
		Tempo:        int(data[50]),
		Format:       FormatS3M,
	}
	if m.Speed == 0 {
		m.Speed = 6
	}
	if m.Tempo < 33 {
		m.Tempo = 125
	}
	stereo := data[51]&0x80 != 0
	m.MixVolume = int(data[51] & 0x7F)
	defaultPan := data[53] == 252
	off := 96 + ordNum + insNum*2 + patNum*2
	if off > len(data) {
		return nil, fmt.Errorf("s3m: header truncated")
	}
	// Channel settings decide which of the 32 slots exist and their side.
	var chanSide []int
	for c := range 32 {
		set := data[64+c]
		if set&0x80 != 0 || set == 255 {
			continue
		}
		if set&0x7F >= 16 {
			continue // Adlib channels are not played
		}
		chanSide = append(chanSide, c)
	}
	slotToChannel := map[int]int{}
	for i, slot := range chanSide {
		slotToChannel[slot] = i
	}
	m.Channels = len(chanSide)
	m.Pan = make([]float32, m.Channels)
	for i, slot := range chanSide {
		if !stereo {
			continue
		}
		if data[64+slot]&0x7F < 8 {
			m.Pan[i] = -0.5
		} else {
			m.Pan[i] = 0.5
		}
	}
	for i := range ordNum {
		o := int(data[96+i])
		if o < 254 {
			m.Orders = append(m.Orders, o)
		}
	}
	insPtr := 96 + ordNum
	patPtr := insPtr + insNum*2
	if defaultPan && off+32 <= len(data) {
		for i, slot := range chanSide {
			if p := data[off+slot]; p&0x20 != 0 && stereo {
				m.Pan[i] = float32(p&0x0F)/7.5 - 1
			}
		}
	}
	for i := range insNum {
		p := int(le.Uint16(data[insPtr+i*2:])) * 16
		s, err := loadS3MInstrument(data, p, ffi)
		if err != nil {
			return nil, fmt.Errorf("s3m: instrument %d: %w", i+1, err)
		}
		m.Samples = append(m.Samples, s)
	}
	for i := range patNum {
		p := int(le.Uint16(data[patPtr+i*2:])) * 16
		pat, err := loadS3MPattern(data, p, m.Channels, slotToChannel)
		if err != nil {
			return nil, fmt.Errorf("s3m: pattern %d: %w", i, err)
		}
		m.Patterns = append(m.Patterns, pat)
	}
	return m, nil
}

func loadS3MInstrument(data []byte, p int, ffi uint16) (Sample, error) {
	if p == 0 || p+80 > len(data) {
		return Sample{Volume: 64, GlobalVolume: 64, C4Speed: 8363}, nil // empty slot
	}
	h := data[p : p+80]
	s := Sample{Name: strings.TrimRight(string(h[48:76]), "\x00 "), Volume: min(int(h[28]), 64), GlobalVolume: 64}
	le := binary.LittleEndian
	s.C4Speed = int(le.Uint32(h[32:]))
	if h[0] != 1 { // not a sample instrument
		return s, nil
	}
	memseg := int(h[13])<<16 | int(le.Uint16(h[14:]))
	start := memseg * 16
	length := int(le.Uint32(h[16:]))
	loopBegin := int(le.Uint32(h[20:]))
	loopEnd := int(le.Uint32(h[24:]))
	flags := h[31]
	sixteen := flags&4 != 0
	bytesPer := 1
	if sixteen {
		bytesPer = 2
	}
	if start+length*bytesPer > len(data) {
		length = max(0, (len(data)-start)/bytesPer)
	}
	s.Data = make([]float32, length)
	for i := range length {
		if sixteen {
			v := le.Uint16(data[start+i*2:])
			if ffi == 2 {
				s.Data[i] = (float32(v) - 32768) / 32768
			} else {
				s.Data[i] = float32(int16(v)) / 32768
			}
		} else {
			v := data[start+i]
			if ffi == 2 {
				s.Data[i] = (float32(v) - 128) / 128
			} else {
				s.Data[i] = float32(int8(v)) / 128
			}
		}
	}
	if flags&1 != 0 && loopEnd > loopBegin {
		s.LoopStart = min(loopBegin, length)
		s.LoopEnd = min(loopEnd, length)
		s.Loop = LoopForward
	}
	return s, nil
}

func loadS3MPattern(data []byte, p int, channels int, slotToChannel map[int]int) (Pattern, error) {
	pat := Pattern{Rows: make([][]Cell, 64)}
	for r := range pat.Rows {
		cells := make([]Cell, channels)
		for c := range cells {
			cells[c] = Cell{Note: NoteNone}
		}
		pat.Rows[r] = cells
	}
	if p == 0 {
		return pat, nil
	}
	if p+2 > len(data) {
		return pat, fmt.Errorf("pattern pointer past end of file")
	}
	end := min(p+2+int(binary.LittleEndian.Uint16(data[p:])), len(data))
	off := p + 2
	row := 0
	for off < end && row < 64 {
		b := data[off]
		off++
		if b == 0 {
			row++
			continue
		}
		cell := Cell{Note: NoteNone}
		if b&0x20 != 0 {
			if off+2 > end {
				break
			}
			n := data[off]
			cell.Instrument = int(data[off+1])
			off += 2
			switch {
			case n == 255:
			case n == 254:
				cell.Note = NoteOff
			default:
				cell.Note = int(n>>4)*12 + int(n&0x0F)
			}
		}
		if b&0x40 != 0 {
			if off+1 > end {
				break
			}
			cell.VolCmd, cell.VolParam = volSet, min(int(data[off]), 64)
			off++
		}
		if b&0x80 != 0 {
			if off+2 > end {
				break
			}
			cell.Effect, cell.Param = s3mEffect(data[off], data[off+1])
			off += 2
		}
		if ch, ok := slotToChannel[int(b&0x1F)]; ok {
			pat.Rows[row][ch] = cell
		}
	}
	return pat, nil
}
