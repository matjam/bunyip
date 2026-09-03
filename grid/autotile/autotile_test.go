package autotile

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestBlobMasks(t *testing.T) {
	masks := BlobMasks()
	seen := map[uint8]bool{}
	for i, m := range masks {
		if normalizeBlob(m) != m {
			t.Errorf("mask %d (%#x) is not normalised", i, m)
		}
		if seen[m] {
			t.Errorf("mask %#x repeats", m)
		}
		seen[m] = true
		if i > 0 && masks[i-1] >= m {
			t.Errorf("masks not ascending at %d", i)
		}
		if BlobIndex(m) != i {
			t.Errorf("BlobIndex(%#x) = %d, want %d", m, BlobIndex(m), i)
		}
	}
	if BlobIndex(0) != 0 || BlobIndex(255) != 46 {
		t.Errorf("endpoints: %d %d", BlobIndex(0), BlobIndex(255))
	}
	// A diagonal without both edges folds away.
	if BlobIndex(1<<DirNE) != BlobIndex(0) {
		t.Error("lone diagonal should normalise to empty")
	}
	if BlobIndex(1<<DirNE|1<<DirN|1<<DirE) == BlobIndex(1<<DirN|1<<DirE) {
		t.Error("diagonal with both edges must stay distinct")
	}
}

// grid3 is a 3x3 map with terrain 1 in a plus shape.
func grid3(x, y int) int {
	if (x == 1 || y == 1) && x < 3 && y < 3 {
		return 1
	}
	return 0
}

func TestEdge16(t *testing.T) {
	var frames [16]int
	for i := range frames {
		frames[i] = i // frame = mask, so results are legible
	}
	m := Mapper{Rules: Edge16(1, frames), OutsideFixed: true}
	got := map[[2]int]int{}
	m.Apply(3, 3, grid3, func(x, y, f int) { got[[2]int{x, y}] = f })
	// The centre connects all four ways; the arms connect to the centre.
	want := map[[2]int]int{
		{1, 1}: 1 | 2 | 4 | 8,
		{1, 0}: 4, {1, 2}: 1, {0, 1}: 2, {2, 1}: 8,
		{0, 0}: -1, {2, 0}: -1, {0, 2}: -1, {2, 2}: -1,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("cell %v: frame %d, want %d", k, got[k], w)
		}
	}
	// Default border policy continues the terrain: a full map of 1s has
	// every cell fully connected.
	m.OutsideFixed = false
	m.Apply(2, 2, func(x, y int) int { return 1 }, func(x, y, f int) {
		if f != 15 {
			t.Errorf("cell %d,%d: frame %d, want 15", x, y, f)
		}
	})
}

func TestCorner16(t *testing.T) {
	var frames [16]int
	for i := range frames {
		frames[i] = i
	}
	m := Mapper{Rules: Corner16(1, frames), OutsideFixed: true}
	// One filled cell in a 2x2 map: the four corners around it light up
	// one quadrant each, and the far corners stay empty.
	one := func(x, y int) int {
		if x == 0 && y == 0 {
			return 1
		}
		return 0
	}
	got := map[[2]int]int{}
	m.Apply(2, 2, one, func(x, y, f int) { got[[2]int{x, y}] = f })
	if len(got) != 9 {
		t.Fatalf("corner grid has %d cells, want 9", len(got))
	}
	want := map[[2]int]int{
		{0, 0}: 8, {1, 0}: 4, {0, 1}: 2, {1, 1}: 1,
		{2, 0}: -1, {2, 2}: -1,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("corner %v: frame %d, want %d", k, got[k], w)
		}
	}
}

func TestBlob47Frames(t *testing.T) {
	var frames [47]int
	for i := range frames {
		frames[i] = i
	}
	m := Mapper{Rules: Blob47(1, frames), OutsideFixed: true}
	full := func(x, y int) int { return 1 }
	m.Apply(3, 3, full, func(x, y, f int) {
		if x == 1 && y == 1 && f != 46 {
			t.Errorf("interior frame %d, want 46", f)
		}
		if x == 0 && y == 0 {
			// Connected east, south and south-east only.
			want := BlobIndex(1<<DirE | 1<<DirS | 1<<DirSE)
			if f != want {
				t.Errorf("corner frame %d, want %d", f, want)
			}
		}
	})
}

func TestCellMatchesApply(t *testing.T) {
	var frames [47]int
	for i := range frames {
		frames[i] = i
	}
	m := Mapper{Rules: Blob47(1, frames)}
	g := map[[2]int]int{}
	terrain := func(x, y int) int { return g[[2]int{x, y}] }
	const w, h = 8, 8
	for i := 0; i < w*h; i += 3 {
		g[[2]int{i % w, i / w}] = 1
	}
	fromApply := map[[2]int]int{}
	m.Apply(w, h, terrain, func(x, y, f int) { fromApply[[2]int{x, y}] = f })
	// Change one cell, patch with Cell, and compare with a fresh Apply.
	g[[2]int{4, 4}] = 1
	patched := map[[2]int]int{}
	for k, v := range fromApply {
		patched[k] = v
	}
	m.Cell(4, 4, w, h, terrain, func(x, y, f int) { patched[[2]int{x, y}] = f })
	fresh := map[[2]int]int{}
	m.Apply(w, h, terrain, func(x, y, f int) { fresh[[2]int{x, y}] = f })
	for k, v := range fresh {
		if patched[k] != v {
			t.Errorf("cell %v: patched %d, fresh %d", k, patched[k], v)
		}
	}
}

func TestVariants(t *testing.T) {
	var frames [16]int
	for i := range frames {
		frames[i] = i
	}
	r := Edge16(1, frames).Variant(15, 100, 1)
	m := Mapper{Rules: r}
	full := func(x, y int) int { return 1 }
	counts := map[int]int{}
	m.Apply(32, 32, full, func(x, y, f int) { counts[f]++ })
	if counts[15] == 0 || counts[100] == 0 {
		t.Fatalf("variants not mixed: %v", counts)
	}
	// The choice is stable for a seed and changes with it.
	first := map[[2]int]int{}
	m.Apply(32, 32, full, func(x, y, f int) { first[[2]int{x, y}] = f })
	m.Apply(32, 32, full, func(x, y, f int) {
		if first[[2]int{x, y}] != f {
			t.Fatalf("reapply changed cell %d,%d", x, y)
		}
	})
	m.Seed = 1
	same := true
	m.Apply(32, 32, full, func(x, y, f int) {
		if first[[2]int{x, y}] != f {
			same = false
		}
	})
	if same {
		t.Error("a new seed changed nothing")
	}
}

// wangCornerSet builds the complete two-colour corner set: one tile per
// corner combination, frame = the 4-bit combination of colour 2.
func wangCornerSet() []WangTile {
	var tiles []WangTile
	for m := 0; m < 16; m++ {
		w := WangTile{Frame: m}
		for i, d := range []int{DirNW, DirNE, DirSW, DirSE} {
			w.Colors[d] = 1
			if m&(1<<i) != 0 {
				w.Colors[d] = 2
			}
		}
		tiles = append(tiles, w)
	}
	return tiles
}

func TestWangCorners(t *testing.T) {
	m := Mapper{Rules: Wang(WangCorners, wangCornerSet()), OutsideFixed: true, Outside: 1}
	// A single colour-2 cell at (1,1) in a field of colour 1.
	terrain := func(x, y int) int {
		if x == 1 && y == 1 {
			return 2
		}
		return 1
	}
	got := map[[2]int]int{}
	m.Apply(3, 3, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	// The higher colour wins every corner it touches: the centre is all
	// colour 2, the diagonal neighbours show one colour-2 corner.
	if got[[2]int{1, 1}] != 15 {
		t.Errorf("centre frame %d, want 15", got[[2]int{1, 1}])
	}
	if got[[2]int{0, 0}] != 8 { // its SE corner touches the 2
		t.Errorf("corner frame %d, want 8", got[[2]int{0, 0}])
	}
	if got[[2]int{2, 1}] != 1|4 { // NW and SW corners touch the 2
		t.Errorf("side frame %d, want %d", got[[2]int{2, 1}], 1|4)
	}
	// All-empty cells give no tile.
	m2 := Mapper{Rules: Wang(WangCorners, wangCornerSet()), OutsideFixed: true}
	m2.Apply(1, 1, func(x, y int) int { return 0 }, func(x, y, f int) {
		if f != -1 {
			t.Errorf("empty cell frame %d, want -1", f)
		}
	})
}

func TestExpandBlob(t *testing.T) {
	const tile = 4
	// Each template tile gets a solid colour so quarters are traceable.
	tmpl := image.NewRGBA(image.Rect(0, 0, 2*tile, 3*tile))
	cols := map[[2]int]color.RGBA{
		{0, 0}: {R: 10}, {1, 0}: {R: 20},
		{0, 1}: {R: 30}, {1, 1}: {R: 40},
		{0, 2}: {R: 50}, {1, 2}: {R: 60},
	}
	for p, c := range cols {
		draw.Draw(tmpl, image.Rect(p[0]*tile, p[1]*tile, (p[0]+1)*tile, (p[1]+1)*tile),
			image.NewUniform(c), image.Point{}, draw.Src)
	}
	out, frames := ExpandBlob(tmpl, tile)
	at := func(frame, qx, qy int) uint8 {
		x := frame % 8 * tile
		y := frame / 8 * tile
		return out.RGBAAt(x+qx*(tile/2)+1, y+qy*(tile/2)+1).R
	}
	// Frame 0 is the isolated tile: all four quarters from the block
	// corners (TL 30, TR 40, BL 50, BR 60).
	iso := frames[BlobIndex(0)]
	if at(iso, 0, 0) != 30 || at(iso, 1, 0) != 40 || at(iso, 0, 1) != 50 || at(iso, 1, 1) != 60 {
		t.Errorf("isolated tile quarters: %d %d %d %d",
			at(iso, 0, 0), at(iso, 1, 0), at(iso, 0, 1), at(iso, 1, 1))
	}
	// The full interior tile draws interior quarters, which come from
	// the diagonally opposite block tiles.
	fullTile := frames[BlobIndex(255)]
	if at(fullTile, 0, 0) != 60 || at(fullTile, 1, 1) != 30 {
		t.Errorf("interior quarters: %d %d", at(fullTile, 0, 0), at(fullTile, 1, 1))
	}
	// North and west connected without the diagonal: the top-left
	// quarter is an inside corner from the inner tile.
	inner := frames[BlobIndex(1<<DirN|1<<DirW)]
	if at(inner, 0, 0) != 10 {
		t.Errorf("inside corner quarter: %d, want 10", at(inner, 0, 0))
	}
	// The preview tile is never read.
	for f := range frames {
		for qy := range 2 {
			for qx := range 2 {
				if at(frames[f], qx, qy) == 20 {
					t.Fatalf("frame %d reads the preview tile", f)
				}
			}
		}
	}
}

// TestExpandBlobRoles checks every assembled tile pixel by pixel against
// its mask, with a template whose pixels encode their role: rim 1,
// interior 2, inside-corner mark 3, transparent 0. It catches a quarter
// copied into the wrong quadrant, which a per-tile colour cannot.
func TestExpandBlobRoles(t *testing.T) {
	const tile = 16
	tmpl := image.NewRGBA(image.Rect(0, 0, 2*tile, 3*tile))
	rim := color.RGBA{R: 1, A: 255}
	in := color.RGBA{R: 2, A: 255}
	fill := func(tx, ty int, top, right, bottom, left bool) {
		for py := range tile {
			for px := range tile {
				c := in
				if top && py < 3 || bottom && py >= tile-3 || left && px < 3 || right && px >= tile-3 {
					c = rim
				}
				if top && left && px < 3 && py < 3 || top && right && px >= tile-3 && py < 3 ||
					bottom && left && px < 3 && py >= tile-3 || bottom && right && px >= tile-3 && py >= tile-3 {
					continue // outer corners are cut away
				}
				tmpl.SetRGBA(tx*tile+px, ty*tile+py, c)
			}
		}
	}
	fill(1, 0, true, true, true, true)
	fill(0, 1, true, false, false, true)
	fill(1, 1, true, true, false, false)
	fill(0, 2, false, false, true, true)
	fill(1, 2, false, true, true, false)
	fill(0, 0, false, false, false, false)
	for _, c := range [4][2]int{{0, 0}, {tile - 3, 0}, {0, tile - 3}, {tile - 3, tile - 3}} {
		for dy := range 3 {
			for dx := range 3 {
				tmpl.SetRGBA(c[0]+dx, c[1]+dy, color.RGBA{R: 3, A: 255})
			}
		}
	}
	sheet, frames := ExpandBlob(tmpl, tile)
	at := func(f, x, y int) uint8 {
		c := sheet.RGBAAt(f%8*tile+x, f/8*tile+y)
		if c.A == 0 {
			return 0
		}
		return c.R
	}
	for i, mask := range BlobMasks() {
		f := frames[i]
		bit := func(d int) bool { return mask&(1<<d) != 0 }
		n, e, s, w := bit(DirN), bit(DirE), bit(DirS), bit(DirW)
		// An edge midpoint is a rim when open and interior when connected.
		for _, c := range []struct {
			name      string
			x, y      int
			connected bool
		}{{"N", 8, 1, n}, {"E", 14, 8, e}, {"S", 8, 14, s}, {"W", 1, 8, w}} {
			want := uint8(1)
			if c.connected {
				want = 2
			}
			if got := at(f, c.x, c.y); got != want {
				t.Errorf("mask %08b %s edge: %d, want %d", mask, c.name, got, want)
			}
		}
		// A corner is cut away when both sides are open, an inside-corner
		// mark when both connect without the diagonal, interior when the
		// diagonal connects too, and otherwise a rim running past it.
		for _, c := range []struct {
			name    string
			x, y    int
			a, b, d bool
		}{{"NW", 1, 1, n, w, bit(DirNW)}, {"NE", 14, 1, n, e, bit(DirNE)},
			{"SW", 1, 14, s, w, bit(DirSW)}, {"SE", 14, 14, s, e, bit(DirSE)}} {
			var want uint8
			switch {
			case !c.a && !c.b:
				want = 0
			case c.a && c.b && c.d:
				want = 2
			case c.a && c.b:
				want = 3
			default:
				want = 1
			}
			if got := at(f, c.x, c.y); got != want {
				t.Errorf("mask %08b %s corner: %d, want %d", mask, c.name, got, want)
			}
		}
	}
}
