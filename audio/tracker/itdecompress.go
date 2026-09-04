package tracker

import "fmt"

// itDecompress expands Impulse Tracker 2.14 sample compression: blocks of
// variable-width delta-coded values with in-band width changes. Version
// 2.15 files integrate the deltas twice.
func itDecompress(src []byte, count int, sixteen bool, it215 bool) ([]float32, error) {
	if count < 0 {
		return nil, fmt.Errorf("compressed sample has a negative length")
	}
	// The data cannot hold more samples than it has bits, so a header
	// claiming billions of samples does not reserve memory for them.
	out := make([]float32, 0, min(count, len(src)*8))
	blockSamples := 0x8000
	widthMax, borderMax := 9, 0xFF
	if sixteen {
		blockSamples = 0x4000
		widthMax, borderMax = 17, 0xFFFF
	}
	off := 0
	for len(out) < count {
		if off+2 > len(src) {
			return nil, fmt.Errorf("compressed sample truncated")
		}
		blockLen := int(src[off]) | int(src[off+1])<<8
		off += 2
		if off+blockLen > len(src) {
			blockLen = len(src) - off
		}
		r := bitReader{data: src[off : off+blockLen]}
		off += blockLen
		width := widthMax
		var mem1, mem2 int32
		want := min(blockSamples, count-len(out))
		for n := 0; n < want; {
			v, ok := r.read(width)
			if !ok {
				break
			}
			// Width change escapes, per the format's three ranges.
			switch {
			case width <= 6:
				if v == 1<<(width-1) {
					nw, ok := r.read(3)
					if !ok {
						break
					}
					width = adjustWidth(int(nw)+1, width)
					continue
				}
			case width < widthMax:
				border := (borderMax >> (widthMax - width)) - (widthMax / 2)
				if int(v) > border && int(v) <= border+widthMax-1 {
					width = adjustWidth(int(v)-border, width)
					continue
				}
			default:
				if v&(1<<(widthMax-1)) != 0 {
					width = int(v+1) & 0xFF
					if width < 1 || width > widthMax {
						return nil, fmt.Errorf("compressed sample sets a width of %d bits", width)
					}
					continue
				}
			}
			// Sign-extend from width bits.
			sv := int32(v)
			if width < 32 {
				sv = int32(v<<(32-width)) >> (32 - width)
			}
			mem1 += sv
			mem2 += mem1
			val := mem1
			if it215 {
				val = mem2
			}
			if sixteen {
				out = append(out, float32(int16(val))/32768)
			} else {
				out = append(out, float32(int8(val))/128)
			}
			n++
		}
		if r.exhausted() && len(out) < count && blockLen == 0 {
			return nil, fmt.Errorf("compressed sample ended early")
		}
	}
	return out, nil
}

// adjustWidth applies the format's rule that a new width equal to or above
// the current one is stored one less than meant.
func adjustWidth(nw, width int) int {
	if nw >= width {
		nw++
	}
	return nw
}

type bitReader struct {
	data []byte
	pos  int // in bits
}

func (r *bitReader) read(bits int) (uint32, bool) {
	if r.pos+bits > len(r.data)*8 {
		return 0, false
	}
	var v uint32
	for i := range bits {
		bit := (r.data[r.pos>>3] >> (r.pos & 7)) & 1
		v |= uint32(bit) << i
		r.pos++
	}
	return v, true
}

func (r *bitReader) exhausted() bool { return r.pos >= len(r.data)*8 }
