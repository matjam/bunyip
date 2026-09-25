package gfx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"testing"
)

// exrChunksReference is the decoder as it was before the chunks were
// decoded in parallel: one chunk after another, a sample at a time. The
// parallel decoder must give exactly its pixels and its errors.
func exrChunksReference(l *exrLayout, img *HDRImage) error {
	raw := make([]byte, l.perBlock*l.rowBytes)
	for _, off := range l.offsets {
		if off > uint64(len(l.data)) {
			return fmt.Errorf("gfx: exr chunk offset %d is past the end of the file", off)
		}
		c := &exrReader{buf: l.data, pos: int(off)}
		y, err := c.i32()
		if err != nil {
			return fmt.Errorf("gfx: exr chunk: %w", err)
		}
		size, err := c.i32()
		if err != nil {
			return fmt.Errorf("gfx: exr chunk: %w", err)
		}
		if size < 0 {
			return fmt.Errorf("gfx: exr chunk of %d bytes", size)
		}
		payload, err := c.bytes(int(size))
		if err != nil {
			return fmt.Errorf("gfx: exr chunk at %d: %w", off, err)
		}
		row := int(y) - l.yMin
		if row < 0 || row >= l.h {
			return fmt.Errorf("gfx: exr chunk starts at row %d, outside the data window", y)
		}
		rows := min(l.perBlock, l.h-row)
		block, err := exrBlockReference(payload, raw[:rows*l.rowBytes], l.compression)
		if err != nil {
			return fmt.Errorf("gfx: exr chunk at row %d: %w", y, err)
		}
		grey := len(l.channels) == 1 && l.channels[0].name == "Y"
		pos := 0
		for r := range rows {
			for _, ch := range l.channels {
				n := l.w * ch.size()
				plane := block[pos : pos+n]
				pos += n
				var out int
				switch {
				case grey || ch.name == "R":
					out = 0
				case ch.name == "G":
					out = 1
				case ch.name == "B":
					out = 2
				default:
					continue
				}
				base := (row + r) * l.w * 3
				for x := range l.w {
					var v float32
					if ch.pixelType == 1 {
						v = f16ToF32(binary.LittleEndian.Uint16(plane[x*2:]))
					} else {
						v = math.Float32frombits(binary.LittleEndian.Uint32(plane[x*4:]))
					}
					if grey {
						img.Pix[base+x*3], img.Pix[base+x*3+1], img.Pix[base+x*3+2] = v, v, v
						continue
					}
					img.Pix[base+x*3+out] = v
				}
			}
		}
	}
	return nil
}

func exrBlockReference(payload, dst []byte, compression int) ([]byte, error) {
	if len(payload) >= len(dst) {
		if len(payload) != len(dst) {
			return nil, fmt.Errorf("chunk is %d bytes, want %d", len(payload), len(dst))
		}
		return payload, nil
	}
	switch compression {
	case exrNone:
		return nil, fmt.Errorf("chunk is %d bytes, want %d", len(payload), len(dst))
	case exrRLE:
		if err := exrUnRLE(payload, dst); err != nil {
			return nil, err
		}
	case exrZIP, exrZIPS:
		zr, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("zlib: %w", err)
		}
		if _, err := io.ReadFull(zr, dst); err != nil {
			return nil, fmt.Errorf("zlib: %w", err)
		}
		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("zlib: %w", err)
		}
	}
	for i := 1; i < len(dst); i++ {
		dst[i] = byte(int(dst[i-1]) + int(dst[i]) - 128)
	}
	tmp := append([]byte(nil), dst...)
	half := (len(dst) + 1) / 2
	t1, t2 := 0, half
	for s := 0; s < len(dst); {
		dst[s] = tmp[t1]
		t1++
		s++
		if s < len(dst) {
			dst[s] = tmp[t2]
			t2++
			s++
		}
	}
	return dst, nil
}

// exrRawChannel is one channel of a file encodeEXRRandom writes.
type exrRawChannel struct {
	name string
	typ  int32 // 1 half, 2 float
}

// encodeEXRRandom writes a file whose samples are random bit patterns,
// so every half float, subnormals, infinities and NaNs included, goes
// through the decoder. Rows are stored with the given compression, a
// chunk left raw wherever packing it would not make it smaller.
func encodeEXRRandom(width, height int, chans []exrRawChannel, comp int, rng *rand.Rand) []byte {
	w := &exrWriter{comp: comp}
	w.i32(20000630)
	w.i32(2)
	var ch bytes.Buffer
	rowBytes := 0
	for _, c := range chans {
		ch.WriteString(c.name)
		ch.WriteByte(0)
		_ = binary.Write(&ch, binary.LittleEndian, c.typ)
		ch.Write([]byte{0, 0, 0, 0})
		_ = binary.Write(&ch, binary.LittleEndian, int32(1))
		_ = binary.Write(&ch, binary.LittleEndian, int32(1))
		rowBytes += width * 2 * int(c.typ)
	}
	ch.WriteByte(0)
	w.attr("channels", "chlist", ch.Bytes())
	w.attr("compression", "compression", []byte{byte(comp)})
	box := new(bytes.Buffer)
	for _, v := range []int32{3, 7, int32(3 + width - 1), int32(7 + height - 1)} {
		_ = binary.Write(box, binary.LittleEndian, v)
	}
	w.attr("dataWindow", "box2i", box.Bytes())
	w.buf.WriteByte(0)
	perBlock := exrLinesPerBlock[comp]
	blocks := (height + perBlock - 1) / perBlock
	var chunks [][]byte
	for b := range blocks {
		rows := min(perBlock, height-b*perBlock)
		raw := make([]byte, rows*rowBytes)
		for i := range raw {
			// Runs of repeated bytes as well as noise, so RLE and ZIP
			// both have something to pack.
			if i%7 < 3 && i > 0 {
				raw[i] = raw[i-1]
			} else {
				raw[i] = byte(rng.Uint32())
			}
		}
		payload := raw
		if comp != exrNone {
			if packed := exrPack(raw, comp); len(packed) < len(raw) {
				payload = packed
			}
		}
		var chunk bytes.Buffer
		_ = binary.Write(&chunk, binary.LittleEndian, int32(7+b*perBlock))
		_ = binary.Write(&chunk, binary.LittleEndian, int32(len(payload)))
		chunk.Write(payload)
		chunks = append(chunks, chunk.Bytes())
	}
	off := uint64(w.buf.Len() + blocks*8)
	for _, c := range chunks {
		_ = binary.Write(&w.buf, binary.LittleEndian, off)
		off += uint64(len(c))
	}
	for _, c := range chunks {
		w.buf.Write(c)
	}
	return w.buf.Bytes()
}

// TestDecodeEXRMatchesReference decodes files of every supported scheme
// and sample type with the parallel decoder and the reference one and
// requires the same bits in every pixel, and the same error for files
// that are broken.
func TestDecodeEXRMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	layouts := map[string][]exrRawChannel{
		"half rgb":   {{"B", 1}, {"G", 1}, {"R", 1}},
		"float rgb":  {{"B", 2}, {"G", 2}, {"R", 2}},
		"half rgba":  {{"A", 1}, {"B", 1}, {"G", 1}, {"R", 1}},
		"mixed":      {{"B", 2}, {"G", 1}, {"R", 2}, {"Z", 1}},
		"half grey":  {{"Y", 1}},
		"float grey": {{"Y", 2}},
	}
	var files []struct {
		name string
		data []byte
	}
	for name, chans := range layouts {
		for _, comp := range []int{exrNone, exrRLE, exrZIPS, exrZIP} {
			for _, size := range [][2]int{{1, 1}, {7, 5}, {33, 40}, {130, 67}} {
				files = append(files, struct {
					name string
					data []byte
				}{fmt.Sprintf("%s %s %dx%d", name, exrName(comp), size[0], size[1]),
					encodeEXRRandom(size[0], size[1], chans, comp, rng)})
			}
		}
	}
	// Broken files: a chunk's zlib stream cut short, and a later chunk
	// whose offset points past the end; the earlier fault is the one to
	// report.
	broken := encodeEXRRandom(40, 80, layouts["half rgb"], exrZIP, rng)
	good := append([]byte(nil), broken...)
	// The offset table follows the dataWindow attribute's size and value
	// and the header's closing zero byte.
	table := bytes.Index(broken, []byte("dataWindow\x00box2i\x00")) + len("dataWindow\x00box2i\x00") + 4 + 16 + 1
	c1 := binary.LittleEndian.Uint64(broken[table+8:])
	// Chunk 1 claims one byte less than it holds, so it is read as a
	// zlib stream, and its first two bytes are no zlib header.
	binary.LittleEndian.PutUint32(broken[c1+4:], binary.LittleEndian.Uint32(broken[c1+4:])-1)
	broken[c1+8], broken[c1+9] = 0, 0
	binary.LittleEndian.PutUint64(broken[table+3*8:], uint64(len(broken)+10))
	files = append(files, struct {
		name string
		data []byte
	}{"corrupt chunk before a bad offset", broken})
	overlap := append([]byte(nil), good...)
	binary.LittleEndian.PutUint64(overlap[table+2*8:], binary.LittleEndian.Uint64(overlap[table+8:]))
	files = append(files, struct {
		name string
		data []byte
	}{"two offsets at one chunk", overlap})

	for _, f := range files {
		got, gotErr := DecodeEXR(f.data)
		want, wantErr := decodeEXR(f.data, exrChunksReference)
		if (gotErr == nil) != (wantErr == nil) || gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Errorf("%s: error %v, want %v", f.name, gotErr, wantErr)
			continue
		}
		if f.name == "corrupt chunk before a bad offset" && (gotErr == nil || !bytes.Contains([]byte(gotErr.Error()), []byte("row 23: zlib"))) {
			t.Errorf("%s: error %v, want chunk 1's zlib error", f.name, gotErr)
		}
		if gotErr != nil {
			continue
		}
		for i := range want.Pix {
			if math.Float32bits(got.Pix[i]) != math.Float32bits(want.Pix[i]) {
				t.Errorf("%s: sample %d is %08x, want %08x", f.name, i, math.Float32bits(got.Pix[i]), math.Float32bits(want.Pix[i]))
				break
			}
		}
	}
}

// TestHalfTable checks the half-float table against the conversion it
// is built from, for every one of the 65536 values.
func TestHalfTable(t *testing.T) {
	table := halfTable()
	for i := range 1 << 16 {
		if math.Float32bits(table[i]) != math.Float32bits(f16ToF32(uint16(i))) {
			t.Fatalf("half %04x converts to %v, want %v", i, table[i], f16ToF32(uint16(i)))
		}
	}
}
