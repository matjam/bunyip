package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestGPUTimestamps draws a lit mesh for enough frames that the query
// results come back, and checks that the frame reports a nonzero opaque
// pass and a frame total no smaller than it. A device without timestamp
// queries reports nothing at all, which is also correct, so the test
// says so and stops rather than failing.
func TestGPUTimestamps(t *testing.T) {
	g := newHeadless(t, 128, 128)
	if g.timestamps == nil {
		t.Skip("the device has no timestamp queries")
	}
	quad := facingQuad(t, g)
	var stats FrameStats
	// The results describe the frame that last used this slot, so the
	// first frames report nothing; a handful of frames is enough for the
	// ring to come round twice over.
	for range 8 {
		renderMaterial(t, g, func() {
			g.DrawMesh(quad, Material{BaseColor: White}, lin.Identity())
		})
		stats = g.Stats()
	}
	if len(stats.GPU) == 0 {
		t.Fatal("no GPU pass times after eight frames")
	}
	var opaque float64
	var sum float64
	for _, s := range stats.GPU {
		sum += s.MS
		if s.Name == "opaque" {
			opaque = s.MS
		}
	}
	if opaque <= 0 {
		t.Errorf("opaque pass timed %g ms, want more than zero (spans %v)", opaque, stats.GPU)
	}
	if stats.GPUFrameMS < opaque {
		t.Errorf("frame total %g ms is under the opaque pass's %g ms", stats.GPUFrameMS, opaque)
	}
	// Every pass is inside the frame, so the parts cannot outrun the
	// whole by more than rounding.
	if sum > stats.GPUFrameMS+0.001 {
		t.Errorf("passes sum to %g ms, over the frame's %g ms", sum, stats.GPUFrameMS)
	}
	// A frame that drew a mesh through the post chain runs more than one
	// pass, so the breakdown is worth having.
	if len(stats.GPU) < 2 {
		t.Errorf("only %d pass timed: %v", len(stats.GPU), stats.GPU)
	}
}

// TestGPUTimestampsSkipOneShots checks that work recorded outside the
// frame ring, which a probe bake does on its own command buffers, writes
// no timestamps: those buffers never reset the queries, so writing into
// them would be invalid and would spend the open frame's queries on
// spans nothing reads back.
func TestGPUTimestampsSkipOneShots(t *testing.T) {
	g := newHeadless(t, 64, 64)
	if g.timestamps == nil {
		t.Skip("the device has no timestamp queries")
	}
	quad := facingQuad(t, g)
	room := func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: White})
		g.DrawMesh(quad, Material{BaseColor: White}, lin.Identity())
	}
	probe := &ReflectionProbe{Position: lin.V3(0, 0, 0), Resolution: 16}
	defer probe.Destroy()
	// A bake between frames renders six faces through renderScene, which
	// is where the pass timings are recorded from.
	if err := g.BakeProbe(probe, room); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		renderMaterial(t, g, func() { g.DrawMesh(quad, Material{BaseColor: White}, lin.Identity()) })
	}
	spans := g.Stats().GPU
	if len(spans) == 0 {
		t.Fatal("no GPU pass times after the bake")
	}
	// One frame draws each pass once, so a bake leaking into the ring
	// would show as an opaque pass many times the length of the frame.
	for _, s := range spans {
		if s.MS > g.Stats().GPUFrameMS+0.001 {
			t.Errorf("pass %q took %g ms, over the frame's %g ms", s.Name, s.MS, g.Stats().GPUFrameMS)
		}
	}
}
