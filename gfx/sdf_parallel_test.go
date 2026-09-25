package gfx

import (
	"bytes"
	"image"
	"math"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
)

// distanceFieldReference is the float64 distance field SDF fonts were
// built with before, kept to check the float32 one against.
func distanceFieldReference(mask *image.Alpha, spread int) []float64 {
	w, h := mask.Rect.Dx(), mask.Rect.Dy()
	inside := make([]bool, w*h)
	for y := range h {
		for x := range w {
			inside[y*w+x] = mask.Pix[y*mask.Stride+x] >= 128
		}
	}
	dIn := edtReference(inside, w, h, false)
	dOut := edtReference(inside, w, h, true)
	out := make([]float64, w*h)
	for i := range out {
		if inside[i] {
			out[i] = 0.5 + min(dIn[i], float64(spread))/float64(spread)*0.5
		} else {
			out[i] = 0.5 - min(dOut[i], float64(spread))/float64(spread)*0.5
		}
	}
	return out
}

func edtReference(inside []bool, w, h int, invert bool) []float64 {
	type pt struct{ dx, dy int32 }
	const far = 1 << 20
	grid := make([]pt, w*h)
	for i, in := range inside {
		if in == invert {
			grid[i] = pt{0, 0}
		} else {
			grid[i] = pt{far, far}
		}
	}
	at := func(x, y int) pt {
		if x < 0 || y < 0 || x >= w || y >= h {
			return pt{far, far}
		}
		return grid[y*w+x]
	}
	dist2 := func(p pt) int64 { return int64(p.dx)*int64(p.dx) + int64(p.dy)*int64(p.dy) }
	compare := func(x, y int, cur *pt, ox, oy int) {
		o := at(x+ox, y+oy)
		if o.dx >= far {
			return
		}
		o.dx += int32(ox)
		o.dy += int32(oy)
		if dist2(o) < dist2(*cur) {
			*cur = o
		}
	}
	for y := range h {
		for x := range w {
			p := &grid[y*w+x]
			compare(x, y, p, -1, 0)
			compare(x, y, p, 0, -1)
			compare(x, y, p, -1, -1)
			compare(x, y, p, 1, -1)
		}
		for x := w - 1; x >= 0; x-- {
			compare(x, y, &grid[y*w+x], 1, 0)
		}
	}
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			p := &grid[y*w+x]
			compare(x, y, p, 1, 0)
			compare(x, y, p, 0, 1)
			compare(x, y, p, -1, 1)
			compare(x, y, p, 1, 1)
		}
		for x := range w {
			compare(x, y, &grid[y*w+x], -1, 0)
		}
	}
	out := make([]float64, w*h)
	for i, p := range grid {
		out[i] = math.Sqrt(float64(dist2(p)))
	}
	return out
}

// TestSDFTexelsMatchReference renders every glyph of two fonts' ASCII
// preload with the float32 field and with the float64 reference, and
// requires every atlas texel to agree within one unit of 255.
func TestSDFTexelsMatchReference(t *testing.T) {
	g := newHeadless(t, 16, 16)
	const os = sdfOversample
	for _, ttf := range [][]byte{gobold.TTF, goitalic.TTF} {
		f, err := g.NewSDFFont(ttf, 32, FontOptions{})
		if err != nil {
			t.Fatal(err)
		}
		s := &sdfScratch{}
		checked, worst := 0, 0
		for r := rune(32); r < 127; r++ {
			gid, ok := f.faces[0].face.NominalGlyph(r)
			if !ok {
				continue
			}
			got := f.renderSDF(0, gid, s, f.rastFor)
			mask, _, _, ok := f.rasterise(0, gid, f.pxPerEm/f.faces[0].upem, sdfSpread*os)
			if ok != got.ok {
				t.Fatalf("rune %q: rendered %v, rasterised %v", r, got.ok, ok)
			}
			if !ok {
				continue
			}
			mw, mh := mask.Rect.Dx(), mask.Rect.Dy()
			field := distanceFieldReference(mask, sdfSpread*os)
			for yy := range got.h {
				for xx := range got.w {
					var sum float64
					n := 0
					for sy := range os {
						for sx := range os {
							px, py := xx*os+sx, yy*os+sy
							if px < mw && py < mh {
								sum += field[py*mw+px]
								n++
							}
						}
					}
					want := int(uint8(math.Round(sum / float64(n) * 255)))
					d := int(got.texels[yy*got.w+xx]) - want
					worst = max(worst, d, -d)
					checked++
				}
			}
		}
		f.Destroy()
		if worst > 1 {
			t.Errorf("an SDF texel differs from the float64 reference by %d; want at most 1", worst)
		}
		if checked < 10000 {
			t.Errorf("only %d texels checked", checked)
		}
	}
}

// TestSDFParallelMatchesSerial builds the same SDF font with the ASCII
// preload rendered on every core and one glyph at a time, and requires
// the same atlas and the same glyph records.
func TestSDFParallelMatchesSerial(t *testing.T) {
	g := newHeadless(t, 16, 16)
	build := func(parallel bool) *Font {
		sdfParallel = parallel
		defer func() { sdfParallel = true }()
		f, err := g.NewSDFFont(gobold.TTF, 32, FontOptions{Preload: []rune("äßø€")})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	serial, parallel := build(false), build(true)
	defer serial.Destroy()
	defer parallel.Destroy()
	if !bytes.Equal(serial.pix.Pix, parallel.pix.Pix) {
		t.Error("the atlases differ")
	}
	if len(serial.glyphs) != len(parallel.glyphs) {
		t.Fatalf("%d glyphs serially, %d in parallel", len(serial.glyphs), len(parallel.glyphs))
	}
	for k, gl := range serial.glyphs {
		if parallel.glyphs[k] != gl {
			t.Errorf("glyph %v is %+v in parallel, want %+v", k, parallel.glyphs[k], gl)
		}
	}
	sdfPending.Lock()
	left := len(sdfPending.batches)
	sdfPending.Unlock()
	if left != 0 {
		t.Errorf("%d fonts still hold a preload batch after building", left)
	}
}
