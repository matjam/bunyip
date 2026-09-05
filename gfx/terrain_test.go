package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// testHeights is a rolling heightfield with a ridge, enough shape that
// the levels of detail differ from one another.
func testHeights(cols, rows int) []float32 {
	h := make([]float32, cols*rows)
	for z := range rows {
		for x := range cols {
			fx, fz := float64(x)*0.2, float64(z)*0.2
			h[z*cols+x] = float32(2*math.Sin(fx)*math.Cos(fz) + 0.5*math.Sin(fx*3.1))
		}
	}
	return h
}

// TestTerrainLevels draws a terrain along the camera's line of sight: the
// chunk under the camera must be at the finest level and the far one
// coarser, and every chunk's level must fall as it recedes.
func TestTerrainLevels(t *testing.T) {
	g := newHeadless(t, 64, 64)
	const cols, rows = 129, 129
	terrain, err := g.NewTerrain(TerrainOptions{
		Heights: testHeights(cols, rows), Cols: cols, Rows: rows, Cell: 1,
		ChunkSize: 32, Levels: 4, LODDistance: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	if terrain.Chunks() != 16 {
		t.Fatalf("terrain has %d chunks, want 16", terrain.Chunks())
	}
	if terrain.Levels() != 4 {
		t.Fatalf("terrain has %d levels, want 4", terrain.Levels())
	}
	eye := lin.V3(-60, 30, -60)
	frames(t, g, func() {
		g.SetCamera(Camera{Position: eye, Target: lin.Vec3{}, Far: 400})
		g.SetLight(Light{Direction: lin.V3(-0.3, -1, -0.4), Color: White})
		g.DrawTerrain(terrain)
	})
	near, far := -1, -1
	nearDist, farDist := float32(math.MaxFloat32), float32(0)
	for i := range terrain.Chunks() {
		d := terrain.ChunkCentre(i).Distance(eye)
		if d < nearDist {
			near, nearDist = i, d
		}
		if d > farDist {
			far, farDist = i, d
		}
	}
	if got := terrain.ChunkLevel(near); got != 0 {
		t.Errorf("the nearest chunk is %.0f units away at level %d, want the finest", nearDist, got)
	}
	if got := terrain.ChunkLevel(far); got <= terrain.ChunkLevel(near) {
		t.Errorf("the farthest chunk is %.0f units away at level %d, no coarser than the nearest", farDist, got)
	}
	// The level a chunk is drawn at must never rise with distance.
	for i := range terrain.Chunks() {
		for j := range terrain.Chunks() {
			di, dj := terrain.ChunkCentre(i).Distance(eye), terrain.ChunkCentre(j).Distance(eye)
			if di < dj && terrain.ChunkLevel(i) > terrain.ChunkLevel(j) {
				t.Fatalf("chunk %d at %.0f units is level %d, chunk %d at %.0f units is level %d",
					i, di, terrain.ChunkLevel(i), j, dj, terrain.ChunkLevel(j))
			}
		}
	}
}

// edgeAt is where one level's chunk edge sits at sample k along a border,
// which is the straight line between the two samples that level kept.
func edgeAt(border []float32, level, k int) float32 {
	step := 1 << level
	lo := k / step * step
	hi := min(lo+step, len(border)-1)
	if hi == lo {
		return border[lo]
	}
	f := float32(k-lo) / float32(hi-lo)
	return lerp(border[lo], border[hi], f)
}

// TestTerrainSkirts proves the property the skirts exist for. A chunk's
// edge at one level and the same edge at another are two different
// straight lines through the same samples, and the gap between them is
// the crack a coarser neighbour leaves. Every chunk's skirt must hang
// below its surface by at least the worst such gap on any of its four
// borders, whichever pair of levels meets there.
func TestTerrainSkirts(t *testing.T) {
	g := newHeadless(t, 32, 32)
	const cols, rows = 129, 129
	heights := make([]float32, cols*rows)
	for z := range rows {
		for x := range cols {
			// Rough enough that a coarse cell strays a long way from the
			// samples it skips, which is what opens a crack.
			heights[z*cols+x] = float32(3*math.Sin(float64(x)*0.7)*math.Cos(float64(z)*0.6) + 2*math.Sin(float64(x)*0.31+float64(z)*0.23))
		}
	}
	terrain, err := g.NewTerrain(TerrainOptions{
		Heights: heights, Cols: cols, Rows: rows, Cell: 1, ChunkSize: 16, Levels: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	worst := float32(0)
	for i := range terrain.Chunks() {
		c := &terrain.chunks[i]
		// The skirt is however far the finest mesh reaches below the
		// lowest sample of the chunk's surface.
		surface := float32(math.MaxFloat32)
		for j := 0; j <= 16; j++ {
			for k := 0; k <= 16; k++ {
				surface = min(surface, terrain.worldOf(c.x0+k, c.z0+j).Y)
			}
		}
		skirt := surface - c.meshes[0].Min.Y
		if skirt <= 0 {
			t.Fatalf("chunk %d has no skirt", i)
		}
		borders := [4][]float32{}
		for k := 0; k <= 16; k++ {
			borders[0] = append(borders[0], terrain.sample(c.x0+k, c.z0))
			borders[1] = append(borders[1], terrain.sample(c.x0+k, c.z0+16))
			borders[2] = append(borders[2], terrain.sample(c.x0, c.z0+k))
			borders[3] = append(borders[3], terrain.sample(c.x0+16, c.z0+k))
		}
		for _, border := range borders {
			for a := range terrain.Levels() {
				for b := range terrain.Levels() {
					for k := range len(border) {
						gap := abs32(edgeAt(border, a, k) - edgeAt(border, b, k))
						worst = max(worst, gap)
						if gap > skirt {
							t.Fatalf("chunk %d leaves a %g gap between levels %d and %d, but its skirt is only %g", i, gap, a, b, skirt)
						}
					}
				}
			}
		}
	}
	if worst < 0.5 {
		t.Fatalf("the worst gap between levels was %g, so the terrain is too smooth to test the skirts", worst)
	}
}

// TestTerrainQueries checks Height and Normal against the heights the
// terrain was built from: exactly at the samples, between them across a
// cell, and clamped outside the field.
func TestTerrainQueries(t *testing.T) {
	g := newHeadless(t, 32, 32)
	const cols, rows = 65, 65
	heights := testHeights(cols, rows)
	centre := lin.V3(10, 3, -4)
	terrain, err := g.NewTerrain(TerrainOptions{
		Heights: heights, Cols: cols, Rows: rows, Cell: 2, Centre: centre, ChunkSize: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	minX := centre.X - float32(cols-1)*2*0.5
	minZ := centre.Z - float32(rows-1)*2*0.5
	for _, s := range [][2]int{{0, 0}, {1, 7}, {32, 32}, {64, 64}, {63, 2}} {
		x, z := minX+float32(s[0])*2, minZ+float32(s[1])*2
		want := centre.Y + heights[s[1]*cols+s[0]]
		if got := terrain.Height(x, z); abs32(got-want) > 1e-4 {
			t.Errorf("Height at sample %v = %g, want %g", s, got, want)
		}
	}
	// Halfway along a cell is halfway between its two heights.
	mid := terrain.Height(minX+1, minZ)
	want := centre.Y + (heights[0]+heights[1])/2
	if abs32(mid-want) > 1e-4 {
		t.Errorf("Height between samples = %g, want %g", mid, want)
	}
	// Outside the field, the nearest edge sample.
	if got, want := terrain.Height(minX-500, minZ-500), centre.Y+heights[0]; abs32(got-want) > 1e-4 {
		t.Errorf("Height outside the terrain = %g, want the corner's %g", got, want)
	}
	// A normal is a unit vector pointing up, and level ground is straight up.
	n := terrain.Normal(minX+64, minZ+64)
	if abs32(n.Len()-1) > 1e-4 || n.Y <= 0 {
		t.Errorf("Normal = %v, want a unit vector with a positive y", n)
	}
	flat, err := g.NewTerrain(TerrainOptions{Heights: make([]float32, 33*33), Cols: 33, Rows: 33, Cell: 1, ChunkSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer flat.Destroy()
	if n := flat.Normal(0, 0); abs32(n.X) > 1e-5 || abs32(n.Z) > 1e-5 || abs32(n.Y-1) > 1e-5 {
		t.Errorf("flat ground's normal is %v, want 0,1,0", n)
	}
}

// TestTerrainRaycast fires rays at a hill and checks where they land: on
// the surface, from inside the ground, and out over the horizon.
func TestTerrainRaycast(t *testing.T) {
	g := newHeadless(t, 32, 32)
	const cols, rows = 65, 65
	heights := testHeights(cols, rows)
	terrain, err := g.NewTerrain(TerrainOptions{Heights: heights, Cols: cols, Rows: rows, Cell: 1, ChunkSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	// Straight down onto the middle of the field.
	hit, ok := terrain.Raycast(Ray{Origin: lin.V3(3, 50, -7), Dir: lin.V3(0, -1, 0)}, 0)
	if !ok {
		t.Fatal("a ray straight down missed the ground")
	}
	if abs32(hit.X-3) > 1e-3 || abs32(hit.Z+7) > 1e-3 {
		t.Errorf("the hit is at %v, want x = 3 and z = -7", hit)
	}
	if want := terrain.Height(3, -7); abs32(hit.Y-want) > 1e-2 {
		t.Errorf("the hit is at y = %g, want the ground's %g", hit.Y, want)
	}
	// A slanted ray must land on the ground it hits, not below it.
	slant, ok := terrain.Raycast(Ray{Origin: lin.V3(-30, 20, -30), Dir: lin.V3(1, -0.6, 1).Norm()}, 0)
	if !ok {
		t.Fatal("a slanted ray missed the ground")
	}
	if want := terrain.Height(slant.X, slant.Z); abs32(slant.Y-want) > 5e-2 {
		t.Errorf("the slanted hit is at y = %g, the ground there is %g", slant.Y, want)
	}
	// Under the ground, the ray starts inside it.
	if hit, ok := terrain.Raycast(Ray{Origin: lin.V3(0, -50, 0), Dir: lin.V3(0, 1, 0)}, 0); !ok || hit.Y != -50 {
		t.Errorf("a ray from under the ground gave %v, %v, want its own origin", hit, ok)
	}
	// Straight up sees nothing, and neither does a short reach.
	if _, ok := terrain.Raycast(Ray{Origin: lin.V3(0, 50, 0), Dir: lin.V3(0, 1, 0)}, 0); ok {
		t.Error("a ray into the sky hit the ground")
	}
	if _, ok := terrain.Raycast(Ray{Origin: lin.V3(0, 50, 0), Dir: lin.V3(0, -1, 0)}, 5); ok {
		t.Error("a ray that stops short of the ground hit it")
	}
}

// TestTerrainUpdate digs a hollow into the heights and checks that the
// query, the chunk bounds and the meshes follow.
func TestTerrainUpdate(t *testing.T) {
	g := newHeadless(t, 32, 32)
	const cols, rows = 65, 65
	terrain, err := g.NewTerrain(TerrainOptions{
		Heights: make([]float32, cols*rows), Cols: cols, Rows: rows, Cell: 1, ChunkSize: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	_, hi := terrain.Bounds()
	if hi.Y != 0 {
		t.Fatalf("flat terrain reaches y = %g, want 0", hi.Y)
	}
	h := terrain.Heights()
	for z := 30; z <= 34; z++ {
		for x := 30; x <= 34; x++ {
			h[z*cols+x] = 5
		}
	}
	if err := terrain.Update(30, 30, 34, 34); err != nil {
		t.Fatal(err)
	}
	if got := terrain.Height(float32(32-(cols-1)/2), float32(32-(rows-1)/2)); got != 5 {
		t.Errorf("the raised sample reads %g, want 5", got)
	}
	if _, hi := terrain.Bounds(); hi.Y != 5 {
		t.Errorf("the terrain reaches y = %g after the edit, want 5", hi.Y)
	}
	// The four chunks that meet at the edit all changed.
	raised := 0
	for i := range terrain.Chunks() {
		if _, chi := terrain.chunks[i].lo, terrain.chunks[i].hi; chi.Y == 5 {
			raised++
		}
	}
	if raised != 4 {
		t.Errorf("%d chunks rose, want the 4 that meet at the edit", raised)
	}
}

// TestTerrainOptions covers what NewTerrain refuses and what it fills in.
func TestTerrainOptions(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cases := []struct {
		name string
		opts TerrainOptions
	}{
		{"no heights", TerrainOptions{Cols: 33, Rows: 33}},
		{"too few heights", TerrainOptions{Heights: make([]float32, 10), Cols: 33, Rows: 33}},
		{"a chunk size that is not a power of two", TerrainOptions{Heights: make([]float32, 33*33), Cols: 33, Rows: 33, ChunkSize: 12}},
		{"a size that is not whole chunks", TerrainOptions{Heights: make([]float32, 40*40), Cols: 40, Rows: 40, ChunkSize: 32}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, err := g.NewTerrain(c.opts); err == nil {
				got.Destroy()
				t.Error("want an error, got none")
			}
		})
	}
	// The defaults: 32-sample chunks, four levels, cell 1.
	terrain, err := g.NewTerrain(TerrainOptions{Heights: make([]float32, 65*65), Cols: 65, Rows: 65})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	if cols, rows, cell := terrain.Size(); cols != 65 || rows != 65 || cell != 1 {
		t.Errorf("size is %d by %d at %g, want 65 by 65 at 1", cols, rows, cell)
	}
	if terrain.Chunks() != 4 || terrain.Levels() != 4 {
		t.Errorf("%d chunks at %d levels, want 4 and 4", terrain.Chunks(), terrain.Levels())
	}
	// A chunk that is only two samples across cannot have four levels.
	small, err := g.NewTerrain(TerrainOptions{Heights: make([]float32, 5*5), Cols: 5, Rows: 5, ChunkSize: 2, Levels: 6})
	if err != nil {
		t.Fatal(err)
	}
	defer small.Destroy()
	if small.Levels() != 2 {
		t.Errorf("a 2-sample chunk kept %d levels, want 2", small.Levels())
	}
}

// TestTerrainSplat draws a terrain whose splat map selects a red layer on
// one half and a blue one on the other, so the built-in shader's blend
// shows up as two colours in one frame.
func TestTerrainSplat(t *testing.T) {
	g := newHeadless(t, 64, 64)
	solid := func(c color.RGBA) *Texture {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
		}
		tex, err := g.NewTexture(img, TextureOptions{Repeat: true})
		if err != nil {
			t.Fatal(err)
		}
		return tex
	}
	red, blue := solid(color.RGBA{220, 20, 20, 255}), solid(color.RGBA{20, 20, 220, 255})
	defer red.Destroy()
	defer blue.Destroy()
	// Red weights the near half (large z, the bottom of the splat), green
	// the far half; green picks layer 1.
	splat := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			c := color.RGBA{255, 0, 0, 0}
			if y < 16 {
				c = color.RGBA{0, 255, 0, 0}
			}
			splat.SetRGBA(x, y, c)
		}
	}
	terrain, err := g.NewTerrain(TerrainOptions{
		Heights: make([]float32, 33*33), Cols: 33, Rows: 33, Cell: 1, ChunkSize: 32,
		Splat: splat, Layers: [4]*Texture{red, blue}, LayerScale: [4]float32{4, 4, 4, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terrain.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	img := frames(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 14, 0.001), Target: lin.Vec3{}, Far: 100})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{2, 2, 2, 1}})
		g.DrawTerrain(terrain)
	})
	// Looking straight down, the far half of the field is at the top of
	// the frame and the near half at the bottom.
	top := img.PixOffset(32, 16)
	bottom := img.PixOffset(32, 48)
	if img.Pix[top+2] <= img.Pix[top] {
		t.Errorf("the far half is %v, want the blue layer", img.Pix[top:top+3])
	}
	if img.Pix[bottom] <= img.Pix[bottom+2] {
		t.Errorf("the near half is %v, want the red layer", img.Pix[bottom:bottom+3])
	}
}
