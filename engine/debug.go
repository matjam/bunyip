package engine

import (
	"net/http"
	_ "net/http/pprof" // registers handlers; Config.Pprof starts the listener
	"strconv"
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
	// Waits counts the times the last finished frame stopped for the GPU
	// to go idle. Uploads and destroys inside a frame do not, so a
	// running game reports zero; the overlay names it when it is not.
	Waits int

	// GPUFrameMS is how long the GPU spent on a recent frame, from its
	// first pass to the end of its last. GPU breaks that down by pass:
	// the shadow atlas, the opaque and blended scene, the reflections, the
	// decals, bloom, ambient occlusion, the composite and the 2D stream.
	// Both come from
	// timestamp queries read back a frame or two later, so they lag the
	// frame on screen, and both are zero and empty on a device without
	// timestamp queries.
	GPUFrameMS float64
	GPU        []Scope
}

// Scope is one timed section recorded with Context.Profile.
type Scope struct {
	Name string
	MS   float64
}

// ProfileScope is a section being timed, returned by Context.Profile and
// closed with End. It is a small value that costs no allocation, so a
// game may profile a section that runs many times a frame.
type ProfileScope struct {
	c     *Context
	name  string
	start time.Time
}

// Profile starts timing a section of game code, until End is called on
// what it returns; the result shows in Stats.Scopes and the debug
// overlay:
//
//	defer ctx.Profile("pathfinding").End()
//
// Timing runs whether or not the overlay is shown, so the figures are
// there the moment F3 opens it.
func (c *Context) Profile(name string) ProfileScope {
	return ProfileScope{c: c, name: name, start: time.Now()}
}

// End closes the scope and records how long it took. Ending a scope
// twice records it twice; ending the zero ProfileScope does nothing.
func (p ProfileScope) End() {
	if p.c == nil {
		return
	}
	p.c.scopes = append(p.c.scopes, Scope{Name: p.name, MS: float64(time.Since(p.start).Microseconds()) / 1000})
}

// overlay draws frame timings in the corner when enabled.
type overlay struct {
	on     bool
	f3Down bool // F3 last seen held, so one press toggles once
	font   *gfx.Font
	budget int // Config.DrawBudget
	// A one-second window for the FPS figure.
	windowStart time.Time
	frames      int
	fps         float64

	// The text on show, rebuilt every overlayRefresh of game time rather
	// than every frame: numbers that change each frame cannot be read,
	// and text that changes each frame is laid out again each frame.
	// lines holds the strings, widths their measured widths and buf the
	// bytes they are formatted in; shownAt is ctx.Time when they were
	// made and shown whether there are any.
	lines   []string
	widths  []float32
	buf     []byte
	shownAt float64
	shown   bool
	over    bool // the last line is the over-budget warning
	// waits is the most GPU stalls in one frame since the text was made,
	// so a single stalled frame between refreshes is still reported.
	waits int
}

// overlayRefresh is how often the overlay's figures change, in seconds.
const overlayRefresh = 0.25

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
	o.waits = max(o.waits, ctx.Stats.Waits)
	if o.stale(ctx.Time) {
		o.format(ctx)
		o.widths = o.widths[:0]
		for _, l := range o.lines {
			lw, _ := o.font.Measure(l, gfx.TextOptions{})
			o.widths = append(o.widths, lw)
		}
	}
	g := ctx.Gfx
	g.ScreenSpace()
	g.SetLayer(1 << 20)
	const pad, lineH = 6, 16
	w := float32(0)
	for _, lw := range o.widths {
		w = max(w, lw)
	}
	g.FillRect(4, 4, w+2*pad, float32(len(o.lines))*lineH+2*pad, gfx.RGBA(0, 0, 0, 160))
	for i, l := range o.lines {
		col := gfx.RGB(220, 230, 200)
		if o.over && i == len(o.lines)-1 {
			col = gfx.RGB(255, 90, 70)
		}
		g.DrawText(o.font, l, 4+pad, 4+pad+float32(i)*lineH, col)
	}
	g.SetLayer(0)
	return nil
}

// stale reports whether the text on show is due to be rebuilt at game
// time t: when there is none, when overlayRefresh has passed, or when
// the clock went back (a device loss restarts it).
func (o *overlay) stale(t float64) bool {
	return !o.shown || t-o.shownAt >= overlayRefresh || t < o.shownAt
}

// format rebuilds the overlay's lines from this frame's figures. The
// numbers are appended with strconv into one reused buffer, so each line
// costs one string and nothing else.
func (o *overlay) format(ctx *Context) {
	s := &ctx.Stats
	o.shownAt, o.shown = ctx.Time, true
	o.lines, o.over = o.lines[:0], false
	b := o.buf[:0]
	f2 := func(v float64) { b = strconv.AppendFloat(b, v, 'f', 2, 64) }
	num := func(v int) { b = strconv.AppendInt(b, int64(v), 10) }
	line := func() {
		o.lines = append(o.lines, string(b))
		b = b[:0]
	}

	b = strconv.AppendFloat(b, s.FPS, 'f', 0, 64)
	b = append(b, " fps  "...)
	f2(s.FrameMS)
	b = append(b, " ms/frame"...)
	line()

	b = append(b, "update "...)
	f2(s.UpdateMS)
	b = append(b, " ms x"...)
	num(s.Updates)
	b = append(b, "  draw "...)
	f2(s.DrawMS)
	b = append(b, " ms  present "...)
	f2(s.PresentMS)
	b = append(b, " ms"...)
	line()

	b = append(b, "voices "...)
	num(ctx.Audio.Playing())
	b = append(b, "  frame "...)
	b = strconv.AppendUint(b, ctx.Frame, 10)
	line()

	b = append(b, "2D "...)
	num(s.Draws2D)
	b = append(b, " draws "...)
	num(s.Vertices2D)
	b = append(b, " verts  3D "...)
	num(s.Draws3D)
	b = append(b, " draws "...)
	num(s.Instances)
	b = append(b, " instances"...)
	line()

	if s.GPUFrameMS > 0 {
		b = append(b, "gpu "...)
		f2(s.GPUFrameMS)
		b = append(b, " ms"...)
		line()
	}
	if o.waits > 0 {
		b = append(b, "GPU STALLS: "...)
		num(o.waits)
		b = append(b, " this frame"...)
		line()
		o.waits = 0
	}
	for _, sc := range s.Scopes {
		b = append(b, "  "...)
		b = append(b, sc.Name...)
		b = append(b, ' ')
		f2(sc.MS)
		b = append(b, " ms"...)
		line()
	}
	if budget := o.budget; budget > 0 && s.Draws2D+s.Draws3D > budget {
		o.over = true
		b = append(b, "OVER DRAW BUDGET: "...)
		num(s.Draws2D + s.Draws3D)
		b = append(b, " draws, budget "...)
		num(budget)
		line()
	}
	o.buf = b
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
	// The toggle runs once per loop iteration, which may be more often
	// than updates, so it watches the key's level rather than an edge.
	down := in.KeyDown(input.KeyF3)
	if down && !o.f3Down {
		o.on = !o.on
	}
	o.f3Down = down
}

// servePprof exposes Go's profiler on addr, for `go tool pprof`.
func servePprof(addr string, ctx *Context) {
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			ctx.Log.Warn("bunyip: pprof server stopped", "addr", addr, "err", err)
		}
	}()
}
