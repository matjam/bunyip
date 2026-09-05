package soft_test

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/phys/soft"
)

const step = 1.0 / 60

// run advances a world for the given number of seconds at the engine's
// fixed step.
func run(w *ecs.World, seconds float64) {
	for range int(seconds / step) {
		w.Update(step)
	}
}

// clothWorld hangs a sheet from its two top corners.
func clothWorld(t *testing.T, spec soft.ClothSpec) (*ecs.World, ecs.Entity) {
	t.Helper()
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
	e := w.SpawnWith(soft.NewCloth(spec))
	w.AddSystem("soft", soft.System)
	return w, e
}

func TestClothHangsAtRest(t *testing.T) {
	const cols, rows = 12, 10
	const spacing = 0.1
	spec := soft.ClothSpec{
		Width: cols, Height: rows, Spacing: spacing, Mass: 0.5,
		Origin: lin.V3(0, 2, 0), Pinned: []int{0, cols - 1},
	}
	w, e := clothWorld(t, spec)
	run(w, 4)
	c, ok := w.Get[soft.Cloth](e)
	if !ok {
		t.Fatal("the cloth component is gone")
	}
	pos := c.Positions()
	// Every edge of the grid within two percent of its rest length.
	worst := float32(0)
	for y := range rows {
		for x := range cols {
			i := c.Index(x, y)
			if x+1 < cols {
				worst = max(worst, stretch(pos[i], pos[c.Index(x+1, y)], spacing))
			}
			if y+1 < rows {
				worst = max(worst, stretch(pos[i], pos[c.Index(x, y+1)], spacing))
			}
		}
	}
	if worst > 0.02 {
		t.Errorf("worst edge is %.1f%% off its rest length, want under 2%%", worst*100)
	}
	// The pins held and the free corners hang below them.
	for _, i := range []int{0, cols - 1} {
		if pos[i].Sub(spec.Origin.Add(lin.V3(float32(i)*spacing, 0, 0))).Len() > 1e-4 {
			t.Errorf("pinned particle %d moved to %v", i, pos[i])
		}
	}
	for _, i := range []int{c.Index(0, rows-1), c.Index(cols-1, rows-1)} {
		if drop := pos[0].Y - pos[i].Y; drop < 0.5*float32(rows-1)*spacing {
			t.Errorf("free corner %d hangs only %v below the pins", i, drop)
		}
	}
	// And it has come to rest.
	fastest := float32(0)
	for _, v := range c.Velocities() {
		fastest = max(fastest, v.Len())
	}
	if fastest > 0.2 {
		t.Errorf("the cloth is still moving at %v units per second", fastest)
	}
}

// stretch is how far a pair is from its rest length, as a fraction.
func stretch(a, b lin.Vec3, rest float32) float32 {
	return float32(math.Abs(float64(a.Sub(b).Len()/rest - 1)))
}

func TestClothLandsOnACollider(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
	// A sheet dropped flat onto a sphere sitting on the ground.
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	e := w.SpawnWith(soft.NewCloth(soft.ClothSpec{
		Width: 12, Height: 12, Spacing: 0.12, Mass: 0.3,
		Origin: lin.V3(-0.66, 1.6, -0.66), Right: lin.V3(1, 0, 0), Down: lin.V3(0, 0, 1),
	}))
	w.AddSystem("soft", soft.System)
	run(w, 3)
	c, _ := w.Get[soft.Cloth](e)
	over := 0
	for _, p := range c.Positions() {
		d, _, _ := phys.SignedDistance3(phys.Sphere{Radius: 0.5}, lin.V3(0, 0.5, 0), lin.Quat{}, p)
		if d < -1e-3 {
			t.Fatalf("a particle at %v sank %v into the sphere", p, -d)
		}
		if d < 0.05 {
			over++
		}
	}
	if over == 0 {
		t.Error("the sheet missed the sphere entirely")
	}
}

func TestClothPinnedParticlesFollowMove(t *testing.T) {
	w, e := clothWorld(t, soft.ClothSpec{Width: 8, Height: 8, Spacing: 0.1, Pinned: []int{0}})
	c, _ := w.Get[soft.Cloth](e)
	if !c.Pinned(0) || c.Pinned(1) {
		t.Fatal("Pinned reports the wrong particles")
	}
	run(w, 0.5)
	c.Move(0, lin.V3(3, 3, 3))
	run(w, 0.5)
	if c.Positions()[0] != lin.V3(3, 3, 3) {
		t.Errorf("the pinned particle is at %v, not where it was moved", c.Positions()[0])
	}
	c.Free(0)
	run(w, 0.5)
	if c.Positions()[0].Y >= 3 {
		t.Error("the freed particle did not fall")
	}
}

// softWorld drops a soft body onto the ground plane.
func softWorld(t *testing.T, spec soft.SoftBody3Spec) (*ecs.World, ecs.Entity) {
	t.Helper()
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Ground: true, Substeps: 8})
	e := w.SpawnWith(soft.NewSoftBody3(spec))
	w.AddSystem("soft", soft.System)
	return w, e
}

func TestSoftBodyKeepsItsVolume(t *testing.T) {
	verts, idx := gfx.SphereMesh(12, 16)
	w, e := softWorld(t, soft.SoftBody3Spec{
		Vertices: verts, Indices: idx, Scale: 0.5, Position: lin.V3(0, 2, 0), Mass: 2,
	})
	b, _ := w.Get[soft.SoftBody3](e)
	rest := b.RestVolume()
	if rest <= 0 {
		t.Fatalf("the rest volume is %v", rest)
	}
	run(w, 3)
	// It came to rest above the plane, with none of it below.
	lowest := float32(math.Inf(1))
	for _, p := range b.Particles() {
		lowest = min(lowest, p.Y)
	}
	if lowest < -1e-3 {
		t.Errorf("the body sank %v below the ground", -lowest)
	}
	// A ball of radius 0.5 resting on the plane keeps its centre near
	// that height, squashed a little by its own weight.
	if c := b.Center(); c.Y < 0.35 || c.Y > 0.6 {
		t.Errorf("the body settled with its centre at %v, not resting on the ground", c.Y)
	}
	if off := math.Abs(float64(b.Volume()/rest - 1)); off > 0.05 {
		t.Errorf("the volume is %.1f%% off its rest volume, want under 5%%", off*100)
	}
	fastest := float32(0)
	for _, v := range b.Velocities() {
		fastest = max(fastest, v.Len())
	}
	if fastest > 0.5 {
		t.Errorf("the body is still moving at %v units per second", fastest)
	}
}

func TestSoftBodyWelds(t *testing.T) {
	verts, idx := gfx.CubeMesh()
	b := soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx})
	if got := len(b.Particles()); got != 8 {
		t.Errorf("a cube welded to %d particles, want 8", got)
	}
	if off := math.Abs(float64(b.RestVolume() - 1)); off > 1e-4 {
		t.Errorf("a unit cube measured %v, want 1", b.RestVolume())
	}
}

func TestSoftBodyImpulse(t *testing.T) {
	verts, idx := gfx.CubeMesh()
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{})
	e := w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Mass: 2}))
	w.AddSystem("soft", soft.System)
	b, _ := w.Get[soft.SoftBody3](e)
	b.Damping = 0
	b.AddImpulse(lin.V3(2, 0, 0))
	before := b.Center()
	run(w, 1)
	if moved := b.Center().Sub(before).X; moved < 0.5 || moved > 1.5 {
		t.Errorf("an impulse of 2 on a mass of 2 moved the body %v in a second, want about 1", moved)
	}
}

func TestSoftBodyRestsOnACollider(t *testing.T) {
	verts, idx := gfx.CubeMesh()
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Substeps: 8})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, -0.5, 0)}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(4, 0.5, 4)}})
	e := w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Position: lin.V3(0, 1.5, 0), Mass: 1}))
	w.AddSystem("soft", soft.System)
	run(w, 3)
	b, _ := w.Get[soft.SoftBody3](e)
	for _, p := range b.Particles() {
		if p.Y < -1e-2 {
			t.Fatalf("a particle at %v sank through the box", p)
		}
	}
	if c := b.Center(); c.Y > 0.75 {
		t.Errorf("the cube settled with its centre at %v, higher than it should rest", c.Y)
	}
}

// fluidWorld fills the lower half of a tank and lets it settle.
func fluidWorld(bounds lin.Rect, fill lin.Rect, settings soft.Settings) (*ecs.World, ecs.Entity) {
	w := ecs.NewWorld()
	w.SetResource(settings)
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: bounds, Spacing: 8})
	f.Fill(fill)
	e := w.SpawnWith(f)
	w.AddSystem("soft", soft.System)
	return w, e
}

func TestFluidStaysInTheTank(t *testing.T) {
	bounds := lin.Rect{X: 0, Y: 0, W: 400, H: 300}
	w, e := fluidWorld(bounds, lin.Rect{X: 20, Y: 20, W: 160, H: 160},
		soft.Settings{Gravity2: lin.V2(0, 900)})
	f, _ := w.Get[soft.Fluid2](e)
	if f.Count() < 200 {
		t.Fatalf("the tank filled with only %d particles", f.Count())
	}
	run(w, 4)
	r := f.Spacing() / 2
	for i, p := range f.Positions() {
		if p.X < bounds.X+r-0.5 || p.X > bounds.X+bounds.W-r+0.5 || p.Y < bounds.Y+r-0.5 || p.Y > bounds.Y+bounds.H-r+0.5 {
			t.Fatalf("particle %d escaped the tank at %v", i, p)
		}
		if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) {
			t.Fatalf("particle %d is not a number", i)
		}
	}
}

func TestFluidHoldsItsDensity(t *testing.T) {
	bounds := lin.Rect{X: 0, Y: 0, W: 400, H: 300}
	w, e := fluidWorld(bounds, lin.Rect{X: 20, Y: 20, W: 160, H: 160},
		soft.Settings{Gravity2: lin.V2(0, 900)})
	f, _ := w.Get[soft.Fluid2](e)
	run(w, 4)
	// Away from the surface, where the kernel runs out of neighbours, the
	// density should sit near the rest density.
	var sum float32
	var n int
	for i := range f.Count() {
		if d := f.Density(i); d > 0.6*f.RestDensity() {
			sum += d
			n++
		}
	}
	if n < f.Count()/2 {
		t.Fatalf("only %d of %d particles have a full set of neighbours", n, f.Count())
	}
	mean := sum / float32(n)
	if off := math.Abs(float64(mean/f.RestDensity() - 1)); off > 0.03 {
		t.Errorf("the mean density is %.1f%% off the rest density, want under 3%%", off*100)
	}
}

func TestFluidSettlesFlat(t *testing.T) {
	bounds := lin.Rect{X: 0, Y: 0, W: 400, H: 400}
	// A column of liquid in one corner: it should collapse and spread.
	w, e := fluidWorld(bounds, lin.Rect{X: 20, Y: 200, W: 100, H: 180},
		soft.Settings{Gravity2: lin.V2(0, 900)})
	f, _ := w.Get[soft.Fluid2](e)
	run(w, 8)
	// The surface height across the tank, in columns a spacing wide.
	const columns = 8
	top := make([]float32, columns)
	for i := range top {
		top[i] = float32(math.Inf(1))
	}
	for _, p := range f.Positions() {
		c := int(p.X / bounds.W * columns)
		c = min(max(c, 0), columns-1)
		top[c] = min(top[c], p.Y)
	}
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	for i, v := range top {
		if math.IsInf(float64(v), 1) {
			t.Fatalf("column %d of the tank has no liquid in it", i)
		}
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi-lo > 2*f.Spacing() {
		t.Errorf("the surface varies by %v across the tank, want under %v", hi-lo, 2*f.Spacing())
	}
}

func TestFluidCollidesWithAStaticShape(t *testing.T) {
	bounds := lin.Rect{X: 0, Y: 0, W: 400, H: 400}
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity2: lin.V2(0, 900)})
	w.SpawnWith(gfx.Transform2{Position: lin.V2(200, 300)}, phys.Collider2{Shape: phys.Circle{Radius: 60}})
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: bounds, Spacing: 8})
	f.Fill(lin.Rect{X: 140, Y: 40, W: 120, H: 120})
	e := w.SpawnWith(f)
	w.AddSystem("soft", soft.System)
	run(w, 4)
	got, _ := w.Get[soft.Fluid2](e)
	for i, p := range got.Positions() {
		if p.Sub(lin.V2(200, 300)).Len() < 60-got.Spacing() {
			t.Fatalf("particle %d at %v is inside the obstacle", i, p)
		}
	}
}

func TestSettingsTakeGravityFromPhys(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
	verts, idx := gfx.CubeMesh()
	e := w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx}))
	w.AddSystem("soft", soft.System)
	b, _ := w.Get[soft.SoftBody3](e)
	run(w, 0.5)
	if b.Center().Y > -0.5 {
		t.Errorf("the body fell to %v; it should have taken gravity from the phys settings", b.Center().Y)
	}
}

func TestEmptyValuesAreSafe(t *testing.T) {
	w := ecs.NewWorld()
	w.SpawnWith(soft.NewCloth(soft.ClothSpec{}))
	w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{}))
	w.SpawnWith(soft.NewFluid2(soft.Fluid2Spec{}))
	w.AddSystem("soft", soft.System)
	run(w, 0.2)
}
