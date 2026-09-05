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
