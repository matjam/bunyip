package gfx

import (
	"fmt"
	"image"
	"math"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/go-text/typesetting/font"
	"golang.org/x/image/vector"

	"github.com/matjam/bunyip/lin"
)

// SDF fonts store each glyph as a signed distance field rasterised once
// at sdfEmPixels, so text draws sharp at any size or rotation.
const (
	sdfEmPixels   = 48 // atlas size of one em
	sdfSpread     = 8  // atlas pixels of distance encoded either side of the edge
	sdfOversample = 4  // the mask is rasterised this many times larger for sub-pixel edges
)

// NewSDFFont prepares a scalable font. Size is a nominal em size in view
// units used by DrawText; TextOptions.Size draws at any other size and
// stays sharp, where a bitmap font would blur. The printable ASCII glyphs
// every font preloads are rasterised on all cores.
func (g *Graphics) NewSDFFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	// scale is face pixels per view unit at the nominal size; the atlas is
	// sdfOversample times coarser than the face.
	scale := float32(sdfEmPixels*sdfOversample) / size
	return g.newFont(ttf, size, opts, scale, sdfEmPixels*sdfOversample, true)
}

// sdfField is one glyph's distance field at atlas resolution: w by h
// texels, placed ox, oy face pixels from the glyph origin. ok is false
// for a glyph with no outline.
type sdfField struct {
	ok     bool
	ox, oy int
	w, h   int
	texels []uint8
}

// sdfBatch holds the fields of a font's ASCII preload, rendered on every
// core by the first addSDF and taken one by one by the calls after it.
type sdfBatch struct {
	fields map[glyphKey]sdfField
}

// sdfPending is each font's batch until the font has taken every field
// in it. The preload asks for exactly the batch's glyphs, one after
// another, so an entry lives only while newFont runs.
var sdfPending struct {
	sync.Mutex
	batches map[*Font]*sdfBatch
}

// sdfParallel turns the batched preload on; tests turn it off to compare
// against one glyph at a time.
var sdfParallel = true

// addSDF rasterises a glyph mask at sdfOversample times the atlas size,
// converts it to distances with a linear-time transform and downsamples,
// so the stored edge position is accurate to a fraction of an atlas pixel.
// A font's first call renders the whole ASCII preload on every core, and
// later calls take their glyph from that batch; the glyphs are placed in
// the atlas one at a time in the order asked for, as they always were.
func (f *Font) addSDF(face uint8, gid font.GID) glyph {
	if sdfParallel && len(f.glyphs) == 0 {
		f.prefetchSDF()
	}
	field, ok := f.takeSDF(glyphKey{face, gid})
	if !ok {
		s := sdfScratchPool.Get().(*sdfScratch)
		field = f.renderSDF(face, gid, s, f.rastFor)
		sdfScratchPool.Put(s)
	}
	if !field.ok {
		return glyph{empty: true}
	}
	w, h := field.w, field.h
	x, y, placed := f.packer.place(w, h)
	if !placed {
		f.glyphErr = fmt.Errorf("gfx: glyph atlas is full (%d by %d); increase FontOptions.AtlasSize", f.packer.width, f.packer.height)
		return glyph{empty: true}
	}
	for yy := range h {
		for xx := range w {
			f.setCoverage(x+xx, y+yy, field.texels[yy*w+xx])
		}
	}
	const os = sdfOversample
	side := float32(f.packer.width)
	f.touched(x, y, w, h)
	return glyph{
		uv0:     lin.V2(float32(x)/side, float32(y)/side),
		uv1:     lin.V2(float32(x+w)/side, float32(y+h)/side),
		size:    lin.V2(float32(w*os)/f.scale, float32(h*os)/f.scale),
		bearing: lin.V2(float32(field.ox)/f.scale, float32(field.oy)/f.scale),
	}
}

// rastFor is the font's own rasteriser, for glyphs rendered one at a
// time on the font's goroutine.
func (f *Font) rastFor(w, h int) *vector.Rasterizer {
	if f.rast == nil || f.rast.Bounds().Dx() < w || f.rast.Bounds().Dy() < h {
		f.rast = vector.NewRasterizer(max(w, 64), max(h, 64))
	}
	return f.rast
}

// prefetchSDF renders the fields of the printable ASCII glyphs every
// font preloads, on up to GOMAXPROCS goroutines, and keeps them for the
// addSDF calls the preload is about to make. Each rune maps to the first
// face that has it, as preload picks.
func (f *Font) prefetchSDF() {
	var keys []glyphKey
	seen := map[glyphKey]bool{}
	for r := rune(32); r < 127; r++ {
		for i, ff := range f.faces {
			if gid, ok := ff.face.NominalGlyph(r); ok {
				k := glyphKey{uint8(i), gid}
				if !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
				break
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	fields := make([]sdfField, len(keys))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(keys)) {
		wg.Go(func() {
			s := &sdfScratch{}
			var rast *vector.Rasterizer
			rastFor := func(w, h int) *vector.Rasterizer {
				if rast == nil || rast.Bounds().Dx() < w || rast.Bounds().Dy() < h {
					rast = vector.NewRasterizer(max(w, 64), max(h, 64))
				}
				return rast
			}
			for {
				i := int(next.Add(1) - 1)
				if i >= len(keys) {
					return
				}
				fields[i] = f.renderSDF(keys[i].face, keys[i].gid, s, rastFor)
			}
		})
	}
	wg.Wait()
	b := &sdfBatch{fields: make(map[glyphKey]sdfField, len(keys))}
	for i, k := range keys {
		b.fields[k] = fields[i]
	}
	sdfPending.Lock()
	if sdfPending.batches == nil {
		sdfPending.batches = map[*Font]*sdfBatch{}
	}
	sdfPending.batches[f] = b
	sdfPending.Unlock()
}

// takeSDF removes a glyph's field from the font's batch. The batch goes
// once it is empty, or once the font asks for a glyph it does not hold,
// which means the preload has moved past it.
func (f *Font) takeSDF(k glyphKey) (sdfField, bool) {
	sdfPending.Lock()
	defer sdfPending.Unlock()
	b := sdfPending.batches[f]
	if b == nil {
		return sdfField{}, false
	}
	field, ok := b.fields[k]
	delete(b.fields, k)
	if !ok || len(b.fields) == 0 {
		delete(sdfPending.batches, f)
	}
	return field, ok
}

// renderSDF rasterises a glyph and turns it into atlas texels, using the
// scratch and rasteriser given, so it runs on any goroutine.
func (f *Font) renderSDF(face uint8, gid font.GID, s *sdfScratch, rastFor func(w, h int) *vector.Rasterizer) sdfField {
	const os = sdfOversample
	k := f.pxPerEm / f.faces[face].upem
	pad := sdfSpread * os
	// The same steps as Font.rasterise, with a rasteriser of the caller's.
	minX, minY, maxX, maxY, has := f.outline(face, gid, k, 0, 0, nil)
	if !has || maxX <= minX || maxY <= minY {
		return sdfField{}
	}
	ox := int(math.Floor(float64(minX))) - pad
	oy := int(math.Floor(float64(minY))) - pad
	mw := int(math.Ceil(float64(maxX))) - ox + pad + 1
	mh := int(math.Ceil(float64(maxY))) - oy + pad + 1
	r := rastFor(mw, mh)
	r.Reset(mw, mh)
	f.outline(face, gid, k, float32(-ox), float32(-oy), r)
	mask := s.alpha(mw, mh)
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	// Round the mask up to whole atlas cells.
	w, h := (mw+os-1)/os, (mh+os-1)/os
	field := s.distanceField(mask, pad)
	texels := make([]uint8, w*h)
	for yy := range h {
		for xx := range w {
			var sum float32
			n := 0
			for sy := range os {
				py := yy*os + sy
				if py >= mh {
					break
				}
				row := field[py*mw:]
				for sx := range os {
					if px := xx*os + sx; px < mw {
						sum += row[px]
						n++
					}
				}
			}
			var v float32
			if n > 0 {
				v = sum / float32(n)
			}
			texels[yy*w+xx] = uint8(math.Round(float64(v * 255)))
		}
	}
	return sdfField{ok: true, ox: ox, oy: oy, w: w, h: h, texels: texels}
}

// sdfScratch is the buffers one goroutine reuses from glyph to glyph.
type sdfScratch struct {
	mask   image.Alpha
	inside []bool
	grid   []edtPoint
	dIn    []float32
	dOut   []float32
	field  []float32
}

var sdfScratchPool = sync.Pool{New: func() any { return &sdfScratch{} }}

// grow returns s resized to n, reusing its storage when it is big enough.
func grow[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}

// alpha returns a cleared w by h mask backed by the scratch.
func (s *sdfScratch) alpha(w, h int) *image.Alpha {
	s.mask.Pix = grow(s.mask.Pix, w*h)
	clear(s.mask.Pix)
	s.mask.Stride = w
	s.mask.Rect = image.Rect(0, 0, w, h)
	return &s.mask
}

// distanceField turns a coverage mask into signed distances in 0..1 with
// 0.5 on the edge, larger inside, saturating at spread pixels. It runs
// the 8SSEDT transform (Danielsson) once for the inside and once for the
// outside, which is linear in the number of pixels. The result is backed
// by the scratch and valid until its next use.
func (s *sdfScratch) distanceField(mask *image.Alpha, spread int) []float32 {
	w, h := mask.Rect.Dx(), mask.Rect.Dy()
	s.inside = grow(s.inside, w*h)
	for y := range h {
		for x := range w {
			s.inside[y*w+x] = mask.Pix[y*mask.Stride+x] >= 128
		}
	}
	s.grid = grow(s.grid, w*h)
	s.dIn = edt(s.inside, w, h, false, s.grid, grow(s.dIn, w*h))  // distance from inside pixels to the outside
	s.dOut = edt(s.inside, w, h, true, s.grid, grow(s.dOut, w*h)) // distance from outside pixels to the inside
	s.field = grow(s.field, w*h)
	sp := float32(spread)
	for i, in := range s.inside {
		if in {
			s.field[i] = 0.5 + min(s.dIn[i], sp)/sp*0.5
		} else {
			s.field[i] = 0.5 - min(s.dOut[i], sp)/sp*0.5
		}
	}
	return s.field
}

// distanceField is sdfScratch.distanceField for a caller that keeps the
// result: it returns a fresh slice of float64.
func distanceField(mask *image.Alpha, spread int) []float64 {
	s := sdfScratchPool.Get().(*sdfScratch)
	defer sdfScratchPool.Put(s)
	field := s.distanceField(mask, spread)
	out := make([]float64, len(field))
	for i, v := range field {
		out[i] = float64(v)
	}
	return out
}

// edtPoint is the offset from a pixel to the nearest pixel of the target
// class found so far.
type edtPoint struct{ dx, dy int32 }

// edtFar marks a pixel with no target pixel found yet.
const edtFar = 1 << 20

func (p edtPoint) dist2() int64 { return int64(p.dx)*int64(p.dx) + int64(p.dy)*int64(p.dy) }

// edt computes, for every pixel where inside == !invert, the Euclidean
// distance to the nearest pixel of the other class, by 8SSEDT, into out.
// grid is scratch of w*h points.
func edt(inside []bool, w, h int, invert bool, grid []edtPoint, out []float32) []float32 {
	for i, in := range inside {
		if in == invert {
			grid[i] = edtPoint{0, 0} // this is the target class
		} else {
			grid[i] = edtPoint{edtFar, edtFar}
		}
	}
	// offer carries neighbour o one step, by (ox, oy), and keeps it in cur
	// when it is nearer. A neighbour outside the grid is never offered.
	offer := func(cur *edtPoint, o edtPoint, ox, oy int32) {
		if o.dx >= edtFar {
			return
		}
		o.dx += ox
		o.dy += oy
		if o.dist2() < cur.dist2() {
			*cur = o
		}
	}
	// Pass 1: top-left to bottom-right. The neighbours are offered in a
	// fixed order, since of two at the same distance the first is kept.
	for y := range h {
		row := grid[y*w : (y+1)*w]
		var up []edtPoint
		if y > 0 {
			up = grid[(y-1)*w : y*w]
		}
		for x := range w {
			p := &row[x]
			if x > 0 {
				offer(p, row[x-1], -1, 0)
			}
			if up != nil {
				offer(p, up[x], 0, -1)
				if x > 0 {
					offer(p, up[x-1], -1, -1)
				}
				if x < w-1 {
					offer(p, up[x+1], 1, -1)
				}
			}
		}
		for x := w - 2; x >= 0; x-- {
			offer(&row[x], row[x+1], 1, 0)
		}
	}
	// Pass 2: bottom-right to top-left.
	for y := h - 1; y >= 0; y-- {
		row := grid[y*w : (y+1)*w]
		var down []edtPoint
		if y < h-1 {
			down = grid[(y+1)*w : (y+2)*w]
		}
		for x := w - 1; x >= 0; x-- {
			p := &row[x]
			if x < w-1 {
				offer(p, row[x+1], 1, 0)
			}
			if down != nil {
				offer(p, down[x], 0, 1)
				if x > 0 {
					offer(p, down[x-1], -1, 1)
				}
				if x < w-1 {
					offer(p, down[x+1], 1, 1)
				}
			}
		}
		for x := 1; x < w; x++ {
			offer(&row[x], row[x-1], -1, 0)
		}
	}
	for i, p := range grid[:w*h] {
		out[i] = float32(math.Sqrt(float64(p.dist2())))
	}
	return out
}
