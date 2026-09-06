package gfx

import (
	"fmt"
	"image"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/lin"
)

// Terrain is a heightfield split into square chunks, each with a mesh at
// several resolutions, drawn at the resolution its distance from the
// camera deserves. Chunks are ordinary draws, so the frustum and the
// frame's occluders cull them, and each carries a skirt around its edge
// deep enough to hide the crack where it meets a coarser neighbour.
//
// The ground is shaded by the built-in terrain shader: a splat map whose
// four channels weight four tiling layer textures. Height and Normal
// answer where the ground is, for placing trees, dropping items and
// walking on it, and Heights with Update let a game dig into it.
//
// Build one with NewTerrain, draw it with DrawTerrain and free it with
// Destroy. One Terrain owns one shader and its pipelines, so a game with
// several of them pays for each; a game usually has one.
type Terrain struct {
	heights []float32
	cols    int
	rows    int
	cell    float32
	centre  lin.Vec3
	minX    float32 // world x of sample column 0
	minZ    float32 // world z of sample row 0

	chunk   int // samples across one chunk
	chunksX int
	chunksZ int
	levels  int
	lodDist float32
	chunks  []terrainChunk

	shader *Shader
	splat  *Texture // the given splat map, or the one pixel that means layer one
	mat    Material
}

// terrainChunk is one square of the heightfield with a mesh per level.
type terrainChunk struct {
	x0, z0 int // the chunk's first sample
	lo, hi lin.Vec3
	centre lin.Vec3
	meshes []*Mesh
	level  int // the level the last DrawTerrain chose
}

// TerrainOptions describes a heightfield to NewTerrain.
type TerrainOptions struct {
	// Heights is one world height per sample, row by row, so
	// Heights[z*Cols+x] is the height at column x of row z. NewTerrain
	// copies it.
	Heights []float32
	// Cols and Rows are the samples across (x) and deep (z). Both minus
	// one must be whole multiples of ChunkSize.
	Cols, Rows int
	// Cell is the world units between samples; zero means 1.
	Cell float32
	// Centre is where the middle of the heightfield sits in the world,
	// and its y is added to every height. Zero puts it at the origin.
	Centre lin.Vec3
	// ChunkSize is the samples across one chunk, a power of two; zero
	// means 32. Small chunks cull and refine finely and cost more draws.
	ChunkSize int
	// Levels is how many resolutions each chunk keeps, each halving the
	// samples of the one before; zero means 4, and it is clamped to what
	// ChunkSize can be halved to.
	Levels int
	// LODDistance is how far the finest level reaches; each level after
	// it covers twice the distance of the one before. Zero means eight
	// chunks' width.
	LODDistance float32
	// Splat weights the four layers by its channels, stretched over the
	// whole heightfield. Nil weights the first layer everywhere.
	Splat image.Image
	// Layers are the tiling albedo textures the splat's red, green, blue
	// and alpha channels choose between. Give them TextureOptions.Repeat,
	// since they tile. A nil layer samples white.
	Layers [4]*Texture
	// LayerScale is the world units per repeat of each layer; a zero
	// entry means 8.
	LayerScale [4]float32
	// LayerRoughness is each layer's roughness; a zero entry means 0.9,
	// which is the ground.
	LayerRoughness [4]float32
}

// NewTerrain builds the chunk meshes, the splat texture and the terrain
// shader from a heightfield. It uploads every chunk at every level at
// once, so a large terrain costs its whole geometry in device memory:
// with the default chunk size and four levels that is about a third more
// than the finest level alone.
// Layer textures must belong to this Graphics; foreign layers return an error.
func (g *Graphics) NewTerrain(opts TerrainOptions) (*Terrain, error) {
	for _, t := range opts.Layers {
		if err := g.textureOwnerError(t); err != nil {
			return nil, err
		}
	}
	t := &Terrain{cols: opts.Cols, rows: opts.Rows, cell: opts.Cell, centre: opts.Centre, chunk: opts.ChunkSize, levels: opts.Levels}
	if t.cell <= 0 {
		t.cell = 1
	}
	if t.chunk <= 0 {
		t.chunk = 32
	}
	if t.levels <= 0 {
		t.levels = 4
	}
	if t.cols < 2 || t.rows < 2 || len(opts.Heights) < t.cols*t.rows {
		return nil, fmt.Errorf("gfx: terrain needs %d by %d heights, got %d", t.cols, t.rows, len(opts.Heights))
	}
	if t.chunk < 2 || t.chunk&(t.chunk-1) != 0 {
		return nil, fmt.Errorf("gfx: terrain ChunkSize %d is not a power of two", t.chunk)
	}
	if (t.cols-1)%t.chunk != 0 || (t.rows-1)%t.chunk != 0 {
		return nil, fmt.Errorf("gfx: terrain is %d by %d samples, which is not a whole number of %d-sample chunks plus one", t.cols, t.rows, t.chunk)
	}
	for t.levels > 1 && t.chunk>>(t.levels-1) < 1 {
		t.levels--
	}
	t.heights = append([]float32(nil), opts.Heights[:t.cols*t.rows]...)
	t.minX = t.centre.X - float32(t.cols-1)*t.cell*0.5
	t.minZ = t.centre.Z - float32(t.rows-1)*t.cell*0.5
	t.chunksX, t.chunksZ = (t.cols-1)/t.chunk, (t.rows-1)/t.chunk
	t.lodDist = opts.LODDistance
	if t.lodDist <= 0 {
		t.lodDist = float32(t.chunk) * t.cell * 8
	}
	if err := t.setupMaterial(g, opts); err != nil {
		return nil, err
	}
	t.chunks = make([]terrainChunk, t.chunksX*t.chunksZ)
	for cz := range t.chunksZ {
		for cx := range t.chunksX {
			c := &t.chunks[cz*t.chunksX+cx]
			c.x0, c.z0 = cx*t.chunk, cz*t.chunk
			c.meshes = make([]*Mesh, t.levels)
			if err := t.buildChunk(g, c); err != nil {
				t.Destroy()
				return nil, err
			}
		}
	}
	return t, nil
}

// setupMaterial makes the splat texture and the shader the chunks draw
// with, and binds the layers to the shader's image slots.
func (t *Terrain) setupMaterial(g *Graphics, opts TerrainOptions) error {
	var err error
	if opts.Splat != nil {
		if t.splat, err = g.NewTexture(opts.Splat, TextureOptions{Data: true}); err != nil {
			return err
		}
	} else {
		// One pixel of pure red: the first layer everywhere.
		if t.splat, err = g.newTexture(1, 1, []byte{255, 0, 0, 0}, TextureOptions{Data: true}); err != nil {
			return err
		}
	}
	if t.shader, err = g.NewMeshShader(shaders.TerrainFrag); err != nil {
		return err
	}
	var params struct{ Scale, Rough lin.Vec4 }
	scale := [4]float32{}
	rough := [4]float32{}
	for i := range 4 {
		t.shader.SetImage(i, opts.Layers[i])
		scale[i] = opts.LayerScale[i]
		if scale[i] <= 0 {
			scale[i] = 8
		}
		rough[i] = opts.LayerRoughness[i]
		if rough[i] <= 0 {
			rough[i] = 0.9
		}
	}
	params.Scale = lin.V4(scale[0], scale[1], scale[2], scale[3])
	params.Rough = lin.V4(rough[0], rough[1], rough[2], rough[3])
	if err := t.shader.SetUniforms(&params); err != nil {
		return err
	}
	t.mat = Material{Shader: t.shader, Texture: t.splat, Roughness: 1}
	return nil
}

// SetSplat replaces the layer weights, for a splat map painted after the
// terrain was built or repainted as the ground changes: the game asks
// the terrain's own Height and Normal where sand, grass, rock and snow
// belong, then hands the answer back. The image is stretched over the
// whole heightfield, its channels weighting layers one to four, and the
// terrain owns and frees the texture it makes from it. A nil image
// restores the default of the first layer everywhere.
func (t *Terrain) SetSplat(img image.Image) error {
	if t == nil || t.splat == nil {
		return nil
	}
	g := t.splat.g
	var next *Texture
	var err error
	if img != nil {
		next, err = g.NewTexture(img, TextureOptions{Data: true})
	} else {
		next, err = g.newTexture(1, 1, []byte{255, 0, 0, 0}, TextureOptions{Data: true})
	}
	if err != nil {
		return err
	}
	// Draws already queued this frame keep the old texture: destroying one
	// inside a frame retires it rather than freeing it now.
	t.splat.Destroy()
	t.splat = next
	t.mat.Texture = next
	return nil
}

// Shader is the terrain's own mesh shader, for a game that wants to
// rebind a layer with SetImage or change the layer scales with
// SetUniforms after it is built.
func (t *Terrain) Shader() *Shader {
	if t == nil {
		return nil
	}
	return t.shader
}

// Destroy frees the chunk meshes, the shader and the splat texture the
// terrain made. Layer textures belong to the game and are left alone.
func (t *Terrain) Destroy() {
	if t == nil {
		return
	}
	for i := range t.chunks {
		for _, m := range t.chunks[i].meshes {
			if m != nil {
				m.Destroy()
			}
		}
		t.chunks[i].meshes = nil
	}
	if t.shader != nil {
		t.shader.Destroy()
		t.shader = nil
	}
	if t.splat != nil {
		t.splat.Destroy()
		t.splat = nil
	}
}

// Bounds is the world box the terrain fills.
func (t *Terrain) Bounds() (min, max lin.Vec3) {
	if t == nil || len(t.chunks) == 0 {
		return lin.Vec3{}, lin.Vec3{}
	}
	lo, hi := t.chunks[0].lo, t.chunks[0].hi
	for i := range t.chunks {
		lo, hi = lo.Min(t.chunks[i].lo), hi.Max(t.chunks[i].hi)
	}
	return lo, hi
}

// Size is the terrain's samples across and deep and the world units
// between them.
func (t *Terrain) Size() (cols, rows int, cell float32) { return t.cols, t.rows, t.cell }

// Chunks is how many chunks the terrain is split into.
func (t *Terrain) Chunks() int { return len(t.chunks) }

// ChunkLevel is the resolution the last DrawTerrain chose for a chunk: 0
// is the finest, each level after it halves the samples along each side.
// It is what to print when the terrain is refining in the wrong places.
func (t *Terrain) ChunkLevel(i int) int {
	if i < 0 || i >= len(t.chunks) {
		return -1
	}
	return t.chunks[i].level
}

// ChunkCentre is the middle of a chunk's world box, which is what the
// level is chosen by.
func (t *Terrain) ChunkCentre(i int) lin.Vec3 {
	if i < 0 || i >= len(t.chunks) {
		return lin.Vec3{}
	}
	return t.chunks[i].centre
}

// Levels is how many resolutions each chunk keeps.
func (t *Terrain) Levels() int { return t.levels }

// Heights is the terrain's own height samples, row by row, so
// Heights()[z*cols+x] is the height at column x of row z. Write into it
// to dig or raise the ground, then call Update over the samples that
// changed so the meshes and their normals follow.
func (t *Terrain) Heights() []float32 { return t.heights }

// Height is the ground's world y at a world x and z, interpolated across
// the cell the point falls in. Outside the terrain it is the height of
// the nearest edge sample.
func (t *Terrain) Height(x, z float32) float32 {
	fx := lin.Clamp((x-t.minX)/t.cell, 0, float32(t.cols-1))
	fz := lin.Clamp((z-t.minZ)/t.cell, 0, float32(t.rows-1))
	ix, iz := min(int(fx), t.cols-2), min(int(fz), t.rows-2)
	tx, tz := fx-float32(ix), fz-float32(iz)
	h00, h10 := t.sample(ix, iz), t.sample(ix+1, iz)
	h01, h11 := t.sample(ix, iz+1), t.sample(ix+1, iz+1)
	return t.centre.Y + lerp(lerp(h00, h10, tx), lerp(h01, h11, tx), tz)
}

// Normal is the ground's unit normal at a world x and z, from the
// heights either side of the nearest sample. Use it to lie a rock flat
// on a slope or to refuse to build on one.
func (t *Terrain) Normal(x, z float32) lin.Vec3 {
	fx := lin.Clamp((x-t.minX)/t.cell, 0, float32(t.cols-1))
	fz := lin.Clamp((z-t.minZ)/t.cell, 0, float32(t.rows-1))
	return t.normalAt(int(fx+0.5), int(fz+0.5))
}

// Raycast walks a ray over the heightfield and returns the first point
// where it passes under the ground, for a click that digs, a shot that
// throws up dust or a unit ordered to a spot. It steps a cell at a time
// and then narrows the step it crossed on, so it finds the nearest hit
// on ground that is no steeper than a cell is wide; reach is how far
// along the ray to look, and zero means the terrain's whole diagonal. A
// ray that starts under the ground reports its own origin.
func (t *Terrain) Raycast(r Ray, reach float32) (lin.Vec3, bool) {
	if t == nil || len(t.chunks) == 0 {
		return lin.Vec3{}, false
	}
	dir := r.Dir.Norm()
	if dir == (lin.Vec3{}) {
		return lin.Vec3{}, false
	}
	if reach <= 0 {
		lo, hi := t.Bounds()
		reach = hi.Sub(lo).Len() * 2
	}
	above := func(d float32) float32 {
		p := r.Origin.Add(dir.Mul(d))
		return p.Y - t.Height(p.X, p.Z)
	}
	if above(0) <= 0 {
		return r.Origin, true
	}
	step := t.cell
	prev := float32(0)
	for d := step; d <= reach; d, prev = d+step, d {
		if above(d) > 0 {
			continue
		}
		// Halve the crossed step twenty times: a millionth of a cell.
		lo, hi := prev, d
		for range 20 {
			mid := (lo + hi) * 0.5
			if above(mid) > 0 {
				lo = mid
			} else {
				hi = mid
			}
		}
		return r.Origin.Add(dir.Mul(hi)), true
	}
	return lin.Vec3{}, false
}

// sample reads a height, clamping to the edge.
func (t *Terrain) sample(x, z int) float32 {
	x, z = min(max(x, 0), t.cols-1), min(max(z, 0), t.rows-1)
	return t.heights[z*t.cols+x]
}

// normalAt is the surface normal at a sample, from the central
// difference of its neighbours.
func (t *Terrain) normalAt(x, z int) lin.Vec3 {
	dx := t.sample(x-1, z) - t.sample(x+1, z)
	dz := t.sample(x, z-1) - t.sample(x, z+1)
	return lin.V3(dx, 2*t.cell, dz).Norm()
}

// worldOf is a sample's world position.
func (t *Terrain) worldOf(x, z int) lin.Vec3 {
	return lin.V3(t.minX+float32(x)*t.cell, t.centre.Y+t.sample(x, z), t.minZ+float32(z)*t.cell)
}

// Update rebuilds the chunks covering a rectangle of samples after the
// game has written into Heights: the cost is one mesh upload per level
// of each chunk it touches. The rectangle is grown by one sample first,
// because a chunk's edge normals read its neighbour's heights.
func (t *Terrain) Update(minX, minZ, maxX, maxZ int) error {
	if t == nil || len(t.chunks) == 0 {
		return nil
	}
	minX, minZ = max(minX-1, 0), max(minZ-1, 0)
	maxX, maxZ = min(maxX+1, t.cols-1), min(maxZ+1, t.rows-1)
	if minX > maxX || minZ > maxZ {
		return nil
	}
	cx0, cx1 := max(minX/t.chunk, 0), min(maxX/t.chunk, t.chunksX-1)
	cz0, cz1 := max(minZ/t.chunk, 0), min(maxZ/t.chunk, t.chunksZ-1)
	g := t.splat.g
	for cz := cz0; cz <= cz1; cz++ {
		for cx := cx0; cx <= cx1; cx++ {
			if err := t.buildChunk(g, &t.chunks[cz*t.chunksX+cx]); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildChunk makes or replaces every level of one chunk's geometry and
// updates its world box.
func (t *Terrain) buildChunk(g *Graphics, c *terrainChunk) error {
	skirt := t.chunkSkirt(c)
	lo := t.worldOf(c.x0, c.z0)
	hi := lo
	for j := 0; j <= t.chunk; j++ {
		for i := 0; i <= t.chunk; i++ {
			p := t.worldOf(c.x0+i, c.z0+j)
			lo, hi = lo.Min(p), hi.Max(p)
		}
	}
	c.lo, c.hi = lo.Sub(lin.V3(0, skirt, 0)), hi
	c.centre = c.lo.Add(c.hi).Mul(0.5)
	for l := range t.levels {
		verts, idx := t.chunkGeometry(c, l, skirt)
		if c.meshes[l] == nil {
			m, err := g.NewMesh(verts, idx)
			if err != nil {
				return err
			}
			c.meshes[l] = m
			continue
		}
		if err := c.meshes[l].Update(verts, idx); err != nil {
			return err
		}
	}
	return nil
}

// chunkSkirt is how far a chunk's edge skirt hangs below its border: far
// enough to hide the crack a coarser neighbour leaves. The crack is
// bounded by how far a level's flat cell strays from the samples it
// skips, so the skirt is twice the worst of those over the chunk's
// levels, which covers both sides of any join.
func (t *Terrain) chunkSkirt(c *terrainChunk) float32 {
	worst := float32(0)
	for l := 1; l < t.levels; l++ {
		step := 1 << l
		for j := 0; j < t.chunk; j += step {
			for i := 0; i < t.chunk; i += step {
				h00 := t.sample(c.x0+i, c.z0+j)
				h10 := t.sample(c.x0+i+step, c.z0+j)
				h01 := t.sample(c.x0+i, c.z0+j+step)
				h11 := t.sample(c.x0+i+step, c.z0+j+step)
				for dj := range step + 1 {
					for di := range step + 1 {
						u := float32(di) / float32(step)
						v := float32(dj) / float32(step)
						flat := lerp(lerp(h00, h10, u), lerp(h01, h11, u), v)
						worst = max(worst, abs32(t.sample(c.x0+i+di, c.z0+j+dj)-flat))
					}
				}
			}
		}
	}
	return worst*2 + t.cell*0.25
}

// chunkGeometry builds one chunk's mesh at a level: a grid of samples
// step apart, then a skirt hanging from each of its four edges. Normals
// come from the full-resolution heightfield at every level, so the
// lighting does not shift when a chunk changes level, and the texture
// coordinates run 0 to 1 across the whole terrain, which is what the
// splat map is stretched over.
func (t *Terrain) chunkGeometry(c *terrainChunk, level int, skirt float32) ([]Vertex, []uint32) {
	step := 1 << level
	n := t.chunk/step + 1
	verts := make([]Vertex, 0, n*n+4*n)
	uvX, uvZ := 1/float32(t.cols-1), 1/float32(t.rows-1)
	for j := range n {
		for i := range n {
			x, z := c.x0+i*step, c.z0+j*step
			verts = append(verts, Vertex{
				Pos:    t.worldOf(x, z),
				Normal: t.normalAt(x, z),
				UV:     lin.V2(float32(x)*uvX, float32(z)*uvZ),
			})
		}
	}
	idx := make([]uint32, 0, (n-1)*(n-1)*6+4*(n-1)*6)
	for j := range n - 1 {
		for i := range n - 1 {
			a := uint32(j*n + i)
			b, cc, e := a+1, a+uint32(n), a+uint32(n)+1
			idx = append(idx, a, e, b, a, cc, e)
		}
	}
	// One skirt per edge: a copy of the edge's vertices dropped by skirt,
	// stitched to them. The copies keep the edge's normal and texture
	// coordinates, so the skirt shades and tiles like the ground above it.
	appendSkirt := func(border []uint32, flip bool) {
		base := uint32(len(verts))
		for _, b := range border {
			v := verts[b]
			v.Pos.Y -= skirt
			verts = append(verts, v)
		}
		for k := range len(border) - 1 {
			a, bb := border[k], border[k+1]
			c0, c1 := base+uint32(k), base+uint32(k)+1
			if flip {
				idx = append(idx, a, c0, bb, bb, c0, c1)
			} else {
				idx = append(idx, a, bb, c0, bb, c1, c0)
			}
		}
	}
	top := make([]uint32, n)
	bottom := make([]uint32, n)
	left := make([]uint32, n)
	right := make([]uint32, n)
	for i := range n {
		top[i] = uint32(i)
		bottom[i] = uint32((n-1)*n + i)
		left[i] = uint32(i * n)
		right[i] = uint32(i*n + n - 1)
	}
	appendSkirt(top, false)
	appendSkirt(bottom, true)
	appendSkirt(left, true)
	appendSkirt(right, false)
	return verts, idx
}

// levelFor is the resolution a chunk that far from the camera is drawn
// at: the finest out to LODDistance, then one coarser per doubling.
func (t *Terrain) levelFor(distance float32) int {
	l := 0
	for reach := t.lodDist; l < t.levels-1 && distance >= reach; reach *= 2 {
		l++
	}
	return l
}

// DrawTerrain queues every chunk of a terrain at the resolution its
// distance from the frame's camera deserves. Chunks are ordinary mesh
// draws, so the frustum and the frame's occluders cull them and
// ChunkLevel reports what each was drawn at.
func (g *Graphics) DrawTerrain(t *Terrain) {
	if t == nil || len(t.chunks) == 0 {
		return
	}
	if err := g.materialOwnerError(t.mat); err != nil {
		panic(err)
	}
	q := g.cur
	q.ensureCamera()
	eye := q.camera.Position
	for i := range t.chunks {
		c := &t.chunks[i]
		c.level = t.levelFor(c.centre.Distance(eye))
		g.DrawMesh(c.meshes[c.level], t.mat, lin.Identity())
	}
}
