package bunyip

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // registered on the default mux when Config.Pprof is set
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// Stats are the previous frame's timings, kept up to date on Context.
type Stats struct {
	FPS       float64 // frames per second over the last second
	FrameMS   float64 // wall time from one frame's start to the next
	UpdateMS  float64 // time spent in Update calls this frame
	DrawMS    float64 // time spent in Draw
	PresentMS float64 // time submitting and waiting on the GPU
	Updates   int     // Update calls this frame
	Scopes    []Scope // Profile scopes recorded this frame

	// GPU work in the last finished frame: 2D draw calls after batching
	// and their vertices, mesh draw calls after instancing and the
	// instances they covered. A rising Draws2D means state changes are
	// breaking batches: textures, shaders, blend modes, clips.
	Draws2D, Vertices2D, Draws3D, Instances int
}

// Scope is one timed section recorded with Context.Profile.
type Scope struct {
	Name string
	MS   float64
}

// Profile times a section of game code until the returned function is
// called; the result shows in Stats.Scopes and the debug overlay:
//
//	defer ctx.Profile("pathfinding")()
func (c *Context) Profile(name string) func() {
	start := time.Now()
	return func() {
		c.scopes = append(c.scopes, Scope{Name: name, MS: float64(time.Since(start).Microseconds()) / 1000})
	}
}

// overlay draws frame timings in the corner when enabled.
type overlay struct {
	on   bool
	font *gfx.Font
	// A one-second window for the FPS figure.
	windowStart time.Time
	frames      int
	fps         float64
}

func (o *overlay) frame(now time.Time) {
	if o.windowStart.IsZero() {
		o.windowStart = now
	}
	o.frames++
	if d := now.Sub(o.windowStart); d >= time.Second {
		o.fps = float64(o.frames) / d.Seconds()
		o.frames, o.windowStart = 0, now
	}
}

func (o *overlay) draw(ctx *Context) error {
	if o.font == nil {
		f, err := ctx.Gfx.NewFont(goregular.TTF, 13, gfx.FontOptions{})
		if err != nil {
			return err
		}
		o.font = f
	}
	s := ctx.Stats
	lines := []string{
		fmt.Sprintf("%.0f fps  %.2f ms/frame", s.FPS, s.FrameMS),
		fmt.Sprintf("update %.2f ms x%d  draw %.2f ms  present %.2f ms", s.UpdateMS, s.Updates, s.DrawMS, s.PresentMS),
		fmt.Sprintf("voices %d  frame %d", ctx.Audio.Playing(), ctx.Frame),
		fmt.Sprintf("2D %d draws %d verts  3D %d draws %d instances", s.Draws2D, s.Vertices2D, s.Draws3D, s.Instances),
	}
	for _, sc := range s.Scopes {
		lines = append(lines, fmt.Sprintf("  %s %.2f ms", sc.Name, sc.MS))
	}
	g := ctx.Gfx
	g.ScreenSpace()
	g.SetLayer(1 << 20)
	const pad, lineH = 6, 16
	w := float32(0)
	for _, l := range lines {
		lw, _ := o.font.Measure(l, gfx.TextOptions{})
		w = max(w, lw)
	}
	g.FillRect(4, 4, w+2*pad, float32(len(lines))*lineH+2*pad, gfx.RGBA(0, 0, 0, 160))
	for i, l := range lines {
		g.DrawText(o.font, l, 4+pad, 4+pad+float32(i)*lineH, gfx.RGB(220, 230, 200))
	}
	g.SetLayer(0)
	return nil
}

// destroy frees the overlay's font before the graphics stack goes.
func (o *overlay) destroy() {
	if o.font != nil {
		o.font.Destroy()
		o.font = nil
	}
}

// toggle flips the overlay on its hotkey, F3.
func (o *overlay) toggle(in *input.State) {
	if in.KeyPressed(input.KeyF3) {
		o.on = !o.on
	}
}

// servePprof exposes Go's profiler on addr, for `go tool pprof`.
func servePprof(addr string, ctx *Context) {
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			ctx.Log.Warn("bunyip: pprof server stopped", "addr", addr, "err", err)
		}
	}()
}
