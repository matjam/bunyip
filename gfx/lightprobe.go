package gfx

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/lin"
)

// maxGridProbes caps a grid's cells, so a bake cannot run for hours or a
// buffer grow past what a frame can upload.
const maxGridProbes = 4096

// gridStorageSize is the starting size of a queue's probe grid buffer, a
// hundred and twenty cells of nine harmonics. WriteStorage grows it for a
// larger grid, once, when the grid is first uploaded.
const gridStorageSize = 120 * 9 * 16

// LightProbeGrid is the diffuse light of a scene sampled on a lattice of
// points: the red bounce along a red wall, the dark under a bridge, the
// warm glow near a fire. Each cell holds the irradiance around it as nine
// spherical harmonics, baked from the scene by BakeLightProbes and
// uploaded once a frame by SetLightProbes. Where the grid covers a
// fragment it replaces the single environment ambient, blended between
// the eight cells around the point; outside the grid the environment or
// sky ambient stands.
//
// A grid lights the diffuse term. Reflections come from the light's
// Environment, the Sky or a ReflectionProbe.
type LightProbeGrid struct {
	// Origin is the world position of cell (0, 0, 0).
	Origin lin.Vec3
	// Spacing is the distance between neighbouring cells on each axis;
	// a zero component means 1.
	Spacing lin.Vec3
	// Counts is how many cells the grid has along x, y and z. Each is at
	// least 1, and their product is at most 4096.
	Counts [3]int
	// Resolution is the cube face size each cell is rendered at before it
	// is projected onto harmonics; zero means 16. The harmonics keep only
	// the low frequencies, so small is enough.
	Resolution int
	// Intensity multiplies the grid's light; zero means 1.
	Intensity float32

	sh []lin.Vec4 // nine per cell, x fastest then y then z
}

// counts is the grid's shape with the defaults filled in.
func (grid *LightProbeGrid) counts() (nx, ny, nz int) {
	return max(grid.Counts[0], 1), max(grid.Counts[1], 1), max(grid.Counts[2], 1)
}

// spacing is the grid's step with the defaults filled in.
func (grid *LightProbeGrid) spacing() lin.Vec3 {
	s := grid.Spacing
	if s.X == 0 {
		s.X = 1
	}
	if s.Y == 0 {
		s.Y = 1
	}
	if s.Z == 0 {
		s.Z = 1
	}
	return s
}

// Baked reports whether the grid holds harmonics yet.
func (grid *LightProbeGrid) Baked() bool { return grid != nil && len(grid.sh) > 0 }

// Position is where one cell sits in the world.
func (grid *LightProbeGrid) Position(x, y, z int) lin.Vec3 {
	s := grid.spacing()
	return grid.Origin.Add(lin.V3(float32(x)*s.X, float32(y)*s.Y, float32(z)*s.Z))
}

// Probe returns one cell's nine irradiance harmonics, the same form an
// Environment keeps, or zeros before the grid is baked or outside it.
func (grid *LightProbeGrid) Probe(x, y, z int) [9]lin.Vec4 {
	var out [9]lin.Vec4
	nx, ny, nz := grid.counts()
	if !grid.Baked() || x < 0 || y < 0 || z < 0 || x >= nx || y >= ny || z >= nz {
		return out
	}
	base := ((z*ny+y)*nx + x) * 9
	copy(out[:], grid.sh[base:base+9])
	return out
}

// BakeLightProbes renders the scene from every cell of the grid and
// projects what it sees onto spherical harmonics. Call it from Init or
// Update, not from Draw: it submits its own command buffers and waits for
// them, once per cell. The scene function queues the draws and lights the
// bake sees, exactly as Draw would. Baking again replaces the harmonics.
func (g *Graphics) BakeLightProbes(grid *LightProbeGrid, scene func()) error {
	if grid == nil {
		return fmt.Errorf("gfx: BakeLightProbes needs a grid")
	}
	if g.frame != nil {
		return fmt.Errorf("gfx: BakeLightProbes cannot run inside Draw; call it from Init or Update")
	}
	nx, ny, nz := grid.counts()
	if nx*ny*nz > maxGridProbes {
		return fmt.Errorf("gfx: a light probe grid of %dx%dx%d has more than %d cells", nx, ny, nz, maxGridProbes)
	}
	size := grid.Resolution
	if size <= 0 {
		size = 16
	}
	b, err := g.newBaker(size, scene)
	if err != nil {
		return err
	}
	defer b.destroy()
	sh := make([]lin.Vec4, nx*ny*nz*9)
	for z := range nz {
		for y := range ny {
			for x := range nx {
				faces, err := b.capture(grid.Position(x, y, z))
				if err != nil {
					return err
				}
				cell := shProject(faces.sample, 32, 16)
				copy(sh[((z*ny+y)*nx+x)*9:], cell[:])
			}
		}
	}
	grid.sh = sh
	return nil
}

// SetLightProbes uses a baked grid for this frame's ambient light, or
// nil for none. An unbaked grid is ignored.
func (g *Graphics) SetLightProbes(grid *LightProbeGrid) {
	if grid != nil && !grid.Baked() {
		grid = nil
	}
	g.cur.grid = grid
}

// gridUniforms is the part of the frame block a grid fills: its origin
// and intensity, its spacing, and its cell counts. A zero intensity in
// the origin's w means the frame has no grid.
func (grid *LightProbeGrid) gridUniforms() (origin, spacing, counts lin.Vec4) {
	if !grid.Baked() {
		return lin.Vec4{}, lin.Vec4{}, lin.Vec4{}
	}
	nx, ny, nz := grid.counts()
	s := grid.spacing()
	intensity := grid.Intensity
	if intensity == 0 {
		intensity = 1
	}
	return grid.Origin.Vec4(intensity), s.Vec4(0), lin.V4(float32(nx), float32(ny), float32(nz), 0)
}

// writeGrid uploads the grid's harmonics into the queue's storage buffer.
// An empty grid writes nothing and the shader never reads it, because the
// frame block says there is no grid.
func (q *drawQueue) writeGrid(slot int) error {
	if q.grid == nil || !q.grid.Baked() {
		return nil
	}
	sh := q.grid.sh
	data := unsafe.Slice((*byte)(unsafe.Pointer(&sh[0])), len(sh)*16)
	return q.uniforms.WriteStorage(slot, data)
}
