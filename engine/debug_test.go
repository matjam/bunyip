package engine

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/matjam/bunyip/audio"
)

// overlayPrintf is the overlay's text as it was built with fmt.Sprintf
// before it moved to strconv; format has to produce the same lines.
func overlayPrintf(ctx *Context, budget int) []string {
	s := ctx.Stats
	lines := []string{
		fmt.Sprintf("%.0f fps  %.2f ms/frame", s.FPS, s.FrameMS),
		fmt.Sprintf("update %.2f ms x%d  draw %.2f ms  present %.2f ms", s.UpdateMS, s.Updates, s.DrawMS, s.PresentMS),
		fmt.Sprintf("voices %d  frame %d", ctx.Audio.Playing(), ctx.Frame),
		fmt.Sprintf("2D %d draws %d verts  3D %d draws %d instances", s.Draws2D, s.Vertices2D, s.Draws3D, s.Instances),
	}
	if s.GPUFrameMS > 0 {
		lines = append(lines, fmt.Sprintf("gpu %.2f ms", s.GPUFrameMS))
	}
	if s.Waits > 0 {
		lines = append(lines, fmt.Sprintf("GPU STALLS: %d this frame", s.Waits))
	}
	for _, sc := range s.Scopes {
		lines = append(lines, fmt.Sprintf("  %s %.2f ms", sc.Name, sc.MS))
	}
	if budget > 0 && s.Draws2D+s.Draws3D > budget {
		lines = append(lines, fmt.Sprintf("OVER DRAW BUDGET: %d draws, budget %d", s.Draws2D+s.Draws3D, budget))
	}
	return lines
}

func TestOverlayFormatMatchesPrintf(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stats  Stats
		budget int
	}{
		{"quiet", Stats{FPS: 59.5, FrameMS: 16.6666, UpdateMS: 0.004, Updates: 1, DrawMS: 1.005, PresentMS: 0}, 0},
		{"busy", Stats{FPS: 144.49, FrameMS: 6.94, UpdateMS: 2.345, Updates: 3, DrawMS: 3.999, PresentMS: 12.3456,
			Draws2D: 120, Vertices2D: 48000, Draws3D: 900, Instances: 12345, GPUFrameMS: 5.555, Waits: 2,
			Scopes: []Scope{{"physics", 0.5}, {"path finding", 12.3456}}}, 1000},
		{"odd numbers", Stats{FPS: math.Inf(1), FrameMS: math.NaN(), UpdateMS: -0.001, Updates: 0, DrawMS: 1e9}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{Audio: audio.NewMixer(audioRate), Frame: 1<<40 + 7, Stats: tc.stats}
			o := &overlay{budget: tc.budget, waits: tc.stats.Waits}
			o.format(ctx)
			want := overlayPrintf(ctx, tc.budget)
			if !slices.Equal(o.lines, want) {
				t.Errorf("lines:\n got %q\nwant %q", o.lines, want)
			}
			if o.over != (tc.budget > 0 && tc.stats.Draws2D+tc.stats.Draws3D > tc.budget) {
				t.Errorf("over = %v", o.over)
			}
		})
	}
}

// The overlay's figures change four times a second of game time, and a
// frame that stalled between two refreshes is still reported at the next.
func TestOverlayRefreshesFourTimesASecond(t *testing.T) {
	ctx := &Context{Audio: audio.NewMixer(audioRate)}
	o := &overlay{}
	if !o.stale(0) {
		t.Fatal("an overlay with nothing on show is not stale")
	}
	o.format(ctx)
	for _, tc := range []struct {
		t     float64
		stale bool
	}{{0.1, false}, {0.2499, false}, {0.25, true}, {-1, true}} {
		if got := o.stale(tc.t); got != tc.stale {
			t.Errorf("stale at %v = %v, want %v", tc.t, got, tc.stale)
		}
	}
	o.waits = max(o.waits, 3) // a stalled frame between refreshes
	ctx.Stats.Waits = 0       // and a clean one at the refresh
	o.format(ctx)
	if !slices.Contains(o.lines, "GPU STALLS: 3 this frame") {
		t.Errorf("stall between refreshes was lost: %q", o.lines)
	}
	o.format(ctx)
	if slices.Contains(o.lines, "GPU STALLS: 3 this frame") {
		t.Errorf("stall reported twice: %q", o.lines)
	}
}

// BenchmarkOverlayFormat is one refresh of the overlay's text with a
// couple of profile scopes, which runs four times a second.
func BenchmarkOverlayFormat(b *testing.B) {
	ctx := &Context{Audio: audio.NewMixer(audioRate), Stats: Stats{FPS: 60, FrameMS: 16.6, Updates: 1,
		Scopes: []Scope{{"physics", 0.5}, {"ai", 1.25}}}}
	o := &overlay{}
	b.ReportAllocs()
	for b.Loop() {
		o.format(ctx)
	}
}
