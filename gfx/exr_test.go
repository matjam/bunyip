package gfx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"testing"
)

// exrWriter builds a minimal single-part scanline OpenEXR file, so the
// decoder is tested against bytes this package did not also produce a
// reader for.
type exrWriter struct {
	buf  bytes.Buffer
	half bool
	comp int
}

func (w *exrWriter) str(s string) {
	w.buf.WriteString(s)
	w.buf.WriteByte(0)
}

func (w *exrWriter) i32(v int32) {
	_ = binary.Write(&w.buf, binary.LittleEndian, v)
}

func (w *exrWriter) attr(name, typ string, value []byte) {
	w.str(name)
	w.str(typ)
	w.i32(int32(len(value)))
	w.buf.Write(value)
}

// sample encodes one radiance value in the file's pixel type.
func (w *exrWriter) sample(dst []byte, v float32) {
	if w.half {
		putF16(dst, v)
		return
	}
	binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
}

func (w *exrWriter) sampleSize() int {
	if w.half {
		return 2
	}
	return 4
}

// encodeEXR writes an image with B, G and R channels, in the writer's
// pixel type and compression scheme.
func encodeEXR(width, height int, pix []float32, half bool, comp int) []byte {
	w := &exrWriter{half: half, comp: comp}
	w.i32(20000630)
	w.i32(2)
	var ch bytes.Buffer
	for _, name := range []string{"B", "G", "R"} { // channels are stored in alphabetical order
		ch.WriteString(name)
		ch.WriteByte(0)
		typ := int32(2)
		if half {
			typ = 1
		}
		_ = binary.Write(&ch, binary.LittleEndian, typ)
		ch.Write([]byte{0, 0, 0, 0})                         // pLinear and three reserved bytes
		_ = binary.Write(&ch, binary.LittleEndian, int32(1)) // xSampling
		_ = binary.Write(&ch, binary.LittleEndian, int32(1)) // ySampling
	}
	ch.WriteByte(0)
	w.attr("channels", "chlist", ch.Bytes())
	w.attr("compression", "compression", []byte{byte(comp)})
	box := new(bytes.Buffer)
	for _, v := range []int32{0, 0, int32(width - 1), int32(height - 1)} {
		_ = binary.Write(box, binary.LittleEndian, v)
	}
	w.attr("dataWindow", "box2i", box.Bytes())
	w.attr("displayWindow", "box2i", box.Bytes())
	w.attr("lineOrder", "lineOrder", []byte{0})
	w.attr("pixelAspectRatio", "float", binary.LittleEndian.AppendUint32(nil, math.Float32bits(1)))
	w.attr("screenWindowCenter", "v2f", binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 0), 0))
	w.attr("screenWindowWidth", "float", binary.LittleEndian.AppendUint32(nil, math.Float32bits(1)))
	w.buf.WriteByte(0)

	perBlock := 1
	if comp == exrZIP {
		perBlock = 16
	}
	blocks := (height + perBlock - 1) / perBlock
	// Each chunk is its first row, its size, then the payload.
	var chunks [][]byte
	for b := range blocks {
		row := b * perBlock
		rows := min(perBlock, height-row)
		raw := make([]byte, rows*width*3*w.sampleSize())
		p := 0
		for r := range rows {
			for _, c := range []int{2, 1, 0} { // B, G, R
				for x := range width {
					w.sample(raw[p:], pix[((row+r)*width+x)*3+c])
					p += w.sampleSize()
				}
			}
		}
		payload := raw
		if comp != exrNone {
			if packed := exrPack(raw, comp); len(packed) < len(raw) {
				payload = packed
			}
		}
		var chunk bytes.Buffer
		_ = binary.Write(&chunk, binary.LittleEndian, int32(row))
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

// exrPack applies the interleave and delta OpenEXR's ZIP and RLE
// compressors use, then packs the result.
func exrPack(raw []byte, comp int) []byte {
	tmp := make([]byte, len(raw))
	half := (len(raw) + 1) / 2
	t1, t2 := 0, half
	for s := 0; s < len(raw); {
		tmp[t1] = raw[s]
		t1++
		s++
		if s < len(raw) {
			tmp[t2] = raw[s]
			t2++
			s++
		}
	}
	p := int(tmp[0])
	for i := 1; i < len(tmp); i++ {
		d := int(tmp[i]) - p + (128 + 256)
		p = int(tmp[i])
		tmp[i] = byte(d)
	}
	if comp == exrRLE {
		return exrRLEPack(tmp)
	}
	var out bytes.Buffer
	zw := zlib.NewWriter(&out)
	_, _ = zw.Write(tmp)
	_ = zw.Close()
	return out.Bytes()
}

// exrRLEPack writes bytes as literal runs and repeats.
func exrRLEPack(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		run := 1
		for i+run < len(src) && src[i+run] == src[i] && run < 128 {
			run++
		}
		if run >= 3 {
			out = append(out, byte(int8(run-1)), src[i])
			i += run
			continue
		}
		start := i
		for i < len(src) && i-start < 127 {
			if i+2 < len(src) && src[i] == src[i+1] && src[i] == src[i+2] {
				break
			}
			i++
		}
		out = append(out, byte(int8(-(i - start))))
		out = append(out, src[start:i]...)
	}
	return out
}

// exrGradient is a test image whose pixels are all different, so a
// scrambled decode cannot pass.
func exrGradient(w, h int) []float32 {
	pix := make([]float32, w*h*3)
	for y := range h {
		for x := range w {
			i := (y*w + x) * 3
			pix[i] = float32(x) * 0.25
			pix[i+1] = float32(y) * 0.5
			pix[i+2] = float32(x+y) * 0.125
		}
	}
	return pix
}

func TestDecodeEXR(t *testing.T) {
	for _, tc := range []struct {
		name string
		half bool
		comp int
		w, h int
	}{
		{"uncompressed float", false, exrNone, 4, 3},
		{"uncompressed half", true, exrNone, 4, 3},
		{"zip half", true, exrZIP, 5, 20},   // more than one 16-row block
		{"zip float", false, exrZIP, 3, 17}, // a partial second block
		{"zips float", false, exrZIPS, 3, 2},
		{"rle half", true, exrRLE, 6, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := exrGradient(tc.w, tc.h)
			img, err := DecodeEXR(encodeEXR(tc.w, tc.h, want, tc.half, tc.comp))
			if err != nil {
				t.Fatal(err)
			}
			if img.Width != tc.w || img.Height != tc.h {
				t.Fatalf("decoded %dx%d, want %dx%d", img.Width, img.Height, tc.w, tc.h)
			}
			for i, v := range want {
				if math.Abs(float64(img.Pix[i]-v)) > 1e-4 {
					t.Fatalf("pixel %d is %v, want %v", i, img.Pix[i], v)
				}
			}
		})
	}
}

func TestDecodeEXRErrors(t *testing.T) {
	good := encodeEXR(2, 2, exrGradient(2, 2), true, exrNone)
	// PIZ and the other schemes this decoder does not implement must say so.
	piz := encodeEXR(2, 2, exrGradient(2, 2), true, exrNone)
	piz[bytes.Index(piz, []byte("compression\x00compression\x00"))+len("compression\x00compression\x00")+4] = exrPIZ
	if _, err := DecodeEXR(piz); err == nil || !bytes.Contains([]byte(err.Error()), []byte("PIZ")) {
		t.Errorf("PIZ file gave %v, want an error naming PIZ", err)
	}
	tiled := append([]byte{}, good...)
	binary.LittleEndian.PutUint32(tiled[4:], 2|0x200)
	if _, err := DecodeEXR(tiled); err == nil || !bytes.Contains([]byte(err.Error()), []byte("tiled")) {
		t.Errorf("tiled file gave %v, want an error naming tiled files", err)
	}
	for name, data := range map[string][]byte{
		"empty":     {},
		"not exr":   []byte("#?RADIANCE\n"),
		"truncated": good[:len(good)-8],
	} {
		if _, err := DecodeEXR(data); err == nil {
			t.Errorf("%s decoded without an error", name)
		}
	}
}

func TestHalfToFloat(t *testing.T) {
	for _, v := range []float32{0, 1, -1, 0.5, 65504, 1.0 / 1024} {
		var b [2]byte
		putF16(b[:], v)
		if got := f16ToF32(binary.LittleEndian.Uint16(b[:])); math.Abs(float64(got-v)) > math.Abs(float64(v))*1e-3+1e-6 {
			t.Errorf("half round trip of %v gave %v", v, got)
		}
	}
}

// TestEnvironmentFromEXR builds an environment from a decoded EXR, the
// path a game takes for an .exr panorama.
func TestEnvironmentFromEXR(t *testing.T) {
	g := newHeadless(t, 32, 32)
	pix := make([]float32, 16*8*3)
	for i := range 16 * 8 {
		pix[i*3], pix[i*3+1], pix[i*3+2] = 0.5, 0.25, 0.125
	}
	img, err := DecodeEXR(encodeEXR(16, 8, pix, true, exrZIP))
	if err != nil {
		t.Fatal(err)
	}
	env, err := g.NewEnvironmentHDR(img, EnvironmentOptions{Size: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Destroy()
	if env.sh[0].X <= env.sh[0].Y || env.sh[0].Y <= env.sh[0].Z {
		t.Errorf("irradiance %v does not follow the panorama's colour", env.sh[0])
	}
}
