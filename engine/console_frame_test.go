package engine

import (
	"slices"
	"testing"

	"github.com/matjam/bunyip/console"
)

var frameSink console.Frame

// ConsoleFrame runs every frame the console exists, open or not, and
// Config.Console promises a closed console costs nothing: the frame it
// reports is built without an allocation, and it still carries working
// callbacks and this frame's scopes.
func TestConsoleFrameAllocatesNothing(t *testing.T) {
	ctx := &Context{timeScale: 1}
	f := ctx.ConsoleFrame()
	if f.Stats.Scopes != nil || f.Stats.GPU != nil {
		t.Errorf("a frame without scopes reported %v and %v", f.Stats.Scopes, f.Stats.GPU)
	}
	ctx.Stats.Scopes = []Scope{{Name: "physics", MS: 1.5}}
	ctx.Stats.GPU = []Scope{{Name: "shadows", MS: 0.25}, {Name: "scene", MS: 2}}
	f = ctx.ConsoleFrame()
	if !slices.Equal(f.Stats.Scopes, []console.Scope{{Name: "physics", MS: 1.5}}) ||
		!slices.Equal(f.Stats.GPU, []console.Scope{{Name: "shadows", MS: 0.25}, {Name: "scene", MS: 2}}) {
		t.Errorf("scopes = %v, GPU = %v", f.Stats.Scopes, f.Stats.GPU)
	}
	f.SetTimeScale(2)
	f.Screenshot("shot.png")
	f.Quit()
	if f.TimeScale() != 2 || ctx.TimeScale() != 2 || ctx.shot != "shot.png" || !ctx.quit {
		t.Error("the frame's callbacks do not reach the context")
	}
	if n := testing.AllocsPerRun(100, func() { frameSink = ctx.ConsoleFrame() }); n != 0 {
		t.Errorf("ConsoleFrame allocates %v times a call", n)
	}
}
