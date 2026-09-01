package tracker

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// LoadMOD parses a ProTracker-family module.
func LoadMOD(data []byte) (*Module, error) {
	if len(data) < 1084 {
		return nil, fmt.Errorf("mod: file too short (%d bytes)", len(data))
	}
	tag := string(data[1080:1084])
	channels := 0
	switch {
	case tag == "M.K." || tag == "M!K!" || tag == "FLT4" || tag == "4CHN":
		channels = 4
	case tag == "6CHN":
		channels = 6
	case tag == "8CHN" || tag == "FLT8" || tag == "OCTA" || tag == "CD81":
		channels = 8
	case len(tag) == 4 && strings.HasSuffix(tag, "CHN") && tag[0] >= '1' && tag[0] <= '9':
		channels = int(tag[0] - '0')
	case len(tag) == 4 && strings.HasSuffix(tag, "CH") && tag[0] >= '1' && tag[0] <= '9' && tag[1] >= '0' && tag[1] <= '9':
		channels = int(tag[0]-'0')*10 + int(tag[1]-'0')
	default:
		return nil, fmt.Errorf("mod: unknown format tag %q", tag)
	}
	m := &Module{
		Title:        strings.TrimRight(string(data[:20]), "\x00 "),
		Channels:     channels,
		Speed:        6,
		Tempo:        125,
		Format:       FormatMOD,
		GlobalVolume: 64,
	}
	type header struct{ length, loopStart, loopLen int }
	headers := make([]header, 31)
	off := 20
	for i := range 31 {
		h := data[off : off+30]
		ft := int(h[24] & 0x0F)
		if ft > 7 {
			ft -= 16
		}
		headers[i] = header{
			length:    int(binary.BigEndian.Uint16(h[22:])) * 2,
			loopStart: int(binary.BigEndian.Uint16(h[26:])) * 2,
			loopLen:   int(binary.BigEndian.Uint16(h[28:])) * 2,
		}
		m.Samples = append(m.Samples, Sample{
			Name:     strings.TrimRight(string(h[:22]), "\x00 "),
			Volume:   min(int(h[25]), 64),
			Finetune: ft,
			C4Speed:  8363,
		})
		off += 30
	}
	songLen := int(data[950])
	m.Restart = int(data[951])
	if m.Restart >= songLen {
		m.Restart = 0
	}
	maxPattern := 0
	for i := range 128 {
		p := int(data[952+i])
		if i < songLen {
			m.Orders = append(m.Orders, p)
		}
		maxPattern = max(maxPattern, p)
	}
	off = 1084
	patternSize := 64 * channels * 4
	for p := 0; p <= maxPattern; p++ {
		if off+patternSize > len(data) {
			return nil, fmt.Errorf("mod: pattern %d truncated", p)
		}
		m.Patterns = append(m.Patterns, decodeMODPattern(data[off:off+patternSize], channels))
		off += patternSize
	}
	for i, h := range headers {
		s := &m.Samples[i]
		if h.length == 0 {
			continue
		}
		end := min(off+h.length, len(data))
		s.Data = make([]float32, end-off)
		for j, b := range data[off:end] {
			s.Data[j] = float32(int8(b)) / 128
		}
		if h.loopLen > 2 {
			s.LoopStart = min(h.loopStart, len(s.Data))
			s.LoopEnd = min(h.loopStart+h.loopLen, len(s.Data))
		}
		off = end
	}
	m.Pan = make([]float32, channels)
	for c := range channels {
		// Amiga hardware pans channels L R R L; soften the hard split.
		if c%4 == 1 || c%4 == 2 {
			m.Pan[c] = 0.5
		} else {
			m.Pan[c] = -0.5
		}
	}
	return m, nil
}

func decodeMODPattern(data []byte, channels int) Pattern {
	pat := Pattern{Rows: make([][]Cell, 64)}
	for row := range 64 {
		cells := make([]Cell, channels)
		for c := range channels {
			b := data[(row*channels+c)*4:]
			period := int(b[0]&0x0F)<<8 | int(b[1])
			cell := Cell{Note: -1, Volume: -1, Instrument: int(b[0]&0xF0) | int(b[2]>>4)}
			if period > 0 {
				cell.Note = modNoteFromPeriod(period)
			}
			cell.Effect, cell.Param = modEffect(b[2]&0x0F, b[3])
			cells[c] = cell
		}
		pat.Rows[row] = cells
	}
	return pat
}
