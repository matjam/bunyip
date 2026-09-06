package engine

import (
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"math"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/console"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// audioRate is the mixer and device sample rate.
const audioRate = 48000

// platformApp returns the process's one connection to the window system.
// platform.NewApp may only run once: it registers Objective-C classes on
// macOS and a window class on Windows, and both refuse a second
// registration. Run rebuilds the window and the renderer after a device
// loss, so the app it hangs them on has to be reused.
var platformApp = sync.OnceValues(platform.NewApp)

// Run opens the window, drives game until it quits or the window closes,
// and tears everything down. It must be called from the main goroutine.
func Run(cfg Config, game Game) (err error) {
	if cfg.Log == nil && cfg.LogFile != "" {
		f, ferr := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if ferr != nil {
			return fmt.Errorf("bunyip: log file: %w", ferr)
		}
		defer f.Close()
		cfg.Log = slog.New(slog.NewTextHandler(f, nil))
		// A crash is the report a player can send: log the panic with its
		// stack before it takes the process down.
		defer func() {
			if r := recover(); r != nil {
				cfg.Log.Error("bunyip: panic", "err", fmt.Sprint(r), "stack", string(debug.Stack()))
				panic(r)
			}
		}()
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default() // the loss below logs through it
	}
	for {
		err := runOnce(cfg, game)
		if !errors.Is(err, render.ErrDeviceLost) {
			return err
		}
		if _, ok := game.(Recoverer); !ok {
			return err
		}
		cfg.Log.Warn("bunyip: GPU device lost; rebuilding", "err", err)
		cfg.recovering = true
	}
}

func runOnce(cfg Config, game Game) error {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if os.Getenv("BUNYIP_HEADLESS") != "" {
		cfg.Headless = true // build machines and the examples test
	}
	if os.Getenv("BUNYIP_FIXED_CLOCK") != "" {
		cfg.FixedClock = true // the examples test, comparing against stored images
	}
	parent, err := cfg.Parent.platform()
	if err != nil {
		return err
	}
	if cfg.Headless && parent != nil {
		return ErrUnsupported
	}
	if cfg.FixedStep <= 0 {
		cfg.FixedStep = time.Second / 60
	}
	if cfg.Width <= 0 {
		cfg.Width = 1280
	}
	if cfg.Height <= 0 {
		cfg.Height = 720
	}
	if cfg.Title == "" {
		cfg.Title = "Bunyip"
	}
	var (
		app         eventSource
		win         window
		surfaceExts []string
		makeSurface render.SurfaceFunc
	)
	if cfg.Headless {
		win = &headlessWindow{w: cfg.Width, h: cfg.Height}
		app = &headlessApp{step: cfg.FixedStep}
		surfaceExts, makeSurface = render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface
	} else {
		pa, err := platformApp()
		if err != nil {
			return err
		}
		cfg.Log.Debug("bunyip: window backend", "backend", platform.Backend())
		pw, err := pa.NewWindow(platform.Config{Parent: parent, Title: cfg.Title, Width: cfg.Width, Height: cfg.Height, Resizable: cfg.Resizable})
		if err != nil {
			return err
		}
		app, win = pa, pw
		surfaceExts, makeSurface = platform.RequiredInstanceExtensions(), pw.CreateSurface
	}
	defer win.Close()
	pw, ph := win.PixelSize()
	r, err := render.NewRenderer(render.Config{AppName: cfg.Title, Validation: cfg.Validation, Log: cfg.Log},
		surfaceExts, makeSurface, vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, !cfg.NoVSync)
	if err != nil {
		return err
	}
	defer r.Destroy()
	gd, err := hook.NewGraphics(r)
	if err != nil {
		return err
	}
	defer gd.Destroy()
	in := hook.NewInput()
	mix := hook.NewMixer(audioRate)
	defer mix.CloseOutput()
	if cfg.NoAudio || cfg.Headless {
		// No device at all was asked for, so the mixer refuses to record
		// from one either.
		mix.SetDevice(false)
	} else {
		err := mix.OpenOutput()
		if err != nil {
			cfg.Log.Warn("bunyip: audio output unavailable, continuing silent", "err", err)
		}
	}
	// The drivers stay in the loop; the game sees the public values.
	l := &loop{cfg: cfg, app: app, win: win, game: game, gfx: gd, input: in, audio: mix,
		ctx: &Context{Gfx: gd.Game().(*gfx.Graphics), Input: in.Game().(*input.State), Log: cfg.Log,
			Audio: mix.Game().(*audio.Mixer), Clear: gfx.RGB(24, 24, 32), win: win, app: app, Alpha: 1,
			focused: cfg.Parent == nil, visible: true, timeScale: 1, budget: cfg.DrawBudget}}
	if cfg.Console {
		// The console tees the log, so it is built before anything the
		// game logs during Init.
		con := console.New(console.Options{Key: cfg.ConsoleKey})
		l.ctx.Console = con
		l.ctx.Log = slog.New(con.Handler(cfg.Log.Handler()))
		cfg.Log = l.ctx.Log
		defer con.Destroy() // before the graphics context goes
	}
	l.overlay.on = cfg.Debug
	l.overlay.budget = cfg.DrawBudget
	if cfg.Icon != nil {
		win.SetIcon(cfg.Icon)
	}
	defer l.overlay.destroy() // before the graphics context, destroyed by the defer above
	if cfg.Pprof != "" && !cfg.recovering {
		servePprof(cfg.Pprof, l.ctx)
	}
	l.applySize()
	defer l.ctx.cleanup()
	l.renderer = r
	family := newWindowFamily(l)
	defer func() { family.closing = true; family.closeChildren(family.root) }()
	if cfg.recovering {
		if err := game.(Recoverer).Recover(l.ctx); err != nil {
			return err
		}
	} else if init, ok := game.(Initer); ok {
		if err := init.Init(l.ctx); err != nil {
			return err
		}
	}
	if shut, ok := game.(Shutdowner); ok {
		defer func() {
			family.closing = true
			defer shut.Shutdown(l.ctx)
			family.closeChildren(family.root)
		}()
	}
	return l.run()
}

type loop struct {
	cfg              Config
	app              eventSource
	win              window
	game             Game
	ctx              *Context
	overlay          overlay
	renderer         *render.Renderer
	family           *windowFamily
	handle           *Window
	eventWindow      *platform.Window
	clock            windowClock
	ready, wasPaused bool

	// The engine's side of the public values in ctx: the frame, the event
	// feed and the audio pull, none of which a game calls.
	gfx   hook.Graphics
	input hook.Input
	audio hook.Audio

	// pausedNow is the pause the loop last put on the mixer, so that it
	// writes the mixer on a change and never otherwise.
	pausedNow bool

	// Timings gathered during the frame, published to ctx.Stats at its end.
	frameStart          time.Time
	updateTime, drawDur time.Duration
	updates             int
}

// applySize reads the window's size and places the view in it: the whole
// window in points, or a fixed view scaled by the configured policy and
// centred, with the rest left black.
func (l *loop) applySize() {
	w := l.win
	pw, ph := w.PixelSize()
	width, height := w.Size()
	l.ctx.pixelsPerPoint = float32(w.Scale())
	l.gfx.Resize(pw, ph)
	cfg := l.cfg
	if cfg.ViewWidth <= 0 || cfg.ViewHeight <= 0 {
		l.ctx.viewport = lin.R(0, 0, float32(pw), float32(ph))
		l.ctx.Width, l.ctx.Height, l.ctx.Scale = float32(width), float32(height), l.ctx.pixelsPerPoint
		l.ctx.Gfx.SetView(float32(width), float32(height))
		if err := l.ctx.Gfx.SetViewport(lin.Rect{}); err != nil {
			l.ctx.Log.Error("bunyip: viewport", "err", err)
		}
		return
	}
	vw, vh := float32(cfg.ViewWidth), float32(cfg.ViewHeight)
	sx, sy := float32(pw)/vw, float32(ph)/vh
	switch cfg.Scaling {
	case ScaleStretch:
	case ScaleInteger:
		s := float32(math.Floor(float64(min(sx, sy))))
		if s < 1 {
			s = min(sx, sy)
		}
		sx, sy = s, s
	default:
		s := min(sx, sy)
		sx, sy = s, s
	}
	rw := float32(math.Round(float64(vw * sx)))
	rh := float32(math.Round(float64(vh * sy)))
	l.ctx.viewport = lin.R(float32(math.Floor(float64((float32(pw)-rw)/2))), float32(math.Floor(float64((float32(ph)-rh)/2))), rw, rh)
	l.ctx.Width, l.ctx.Height, l.ctx.Scale = vw, vh, rw/vw
	l.ctx.Gfx.SetView(vw, vh)
	if err := l.ctx.Gfx.SetViewport(l.ctx.viewport); err != nil {
		l.ctx.Log.Error("bunyip: viewport", "err", err)
	}
}

// toView maps a pointer position in window points into view units.
func (l *loop) toView(x, y float64) (float32, float32) {
	c := l.ctx
	px, py := float32(x)*c.pixelsPerPoint, float32(y)*c.pixelsPerPoint
	return (px - c.viewport.X) / c.viewport.W * c.Width, (py - c.viewport.Y) / c.viewport.H * c.Height
}

// toViewDelta maps a pointer movement in window points into view units.
func (l *loop) toViewDelta(dx, dy float64) (float32, float32) {
	c := l.ctx
	return float32(dx) * c.pixelsPerPoint / c.viewport.W * c.Width, float32(dy) * c.pixelsPerPoint / c.viewport.H * c.Height
}

func (l *loop) run() error {
	if l.family == nil {
		newWindowFamily(l)
	}
	l.resetClock()
	return l.family.run()
}

func (l *loop) update() error {
	start := time.Now()
	err := l.game.Update(l.ctx)
	l.updateTime += time.Since(start)
	l.updates++
	l.input.EndUpdate()
	l.ctx.closeReq = false // the game has seen it
	return err
}

// beginFrame resets the per-frame timing accumulators.
func (l *loop) beginFrame(now time.Time) {
	if !l.frameStart.IsZero() {
		l.ctx.Stats.FrameMS = ms(now.Sub(l.frameStart))
	}
	l.frameStart = now
	l.updateTime, l.drawDur, l.updates = 0, 0, 0
	l.ctx.scopes = l.ctx.scopes[:0]
	l.overlay.frame(now)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// scrollLinePoints is how many points of smooth (trackpad) scrolling
// count as one line of wheel scrolling.
const scrollLinePoints = 24

func (l *loop) draw() error {
	// Draw sees every input edge since the last frame, so an interface
	// built here reacts to clicks that Update already consumed.
	l.input.SetDrawing(true)
	l.gfx.SetTime(l.ctx.Time)
	defer l.input.SetDrawing(false)
	c := l.ctx.Clear
	ok, err := l.gfx.Begin([4]float32{c.R, c.G, c.B, c.A})
	if err != nil || !ok {
		// No frame was drawn (the swapchain is being rebuilt), so the
		// edges latched for Draw stay for the next frame that is.
		return err
	}
	defer l.input.EndFrame()
	drawStart := time.Now()
	if err := l.game.Draw(l.ctx); err != nil {
		return err
	}
	l.drawDur = time.Since(drawStart)
	l.publishStats()
	if l.overlay.on {
		if err := l.overlay.draw(l.ctx); err != nil {
			return err
		}
	}
	if err := l.ctx.Console.Draw(l.ctx); err != nil {
		return err
	}
	capture := l.ctx.shot != ""
	presentStart := time.Now()
	img, err := l.gfx.End(capture)
	if err != nil {
		return err
	}
	l.ctx.Stats.PresentMS = ms(time.Since(presentStart))
	l.ctx.Frame++
	if capture {
		path := l.ctx.shot
		l.ctx.shot = ""
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("screenshot: %w", err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			return fmt.Errorf("screenshot: %w", err)
		}
		l.ctx.Log.Info("bunyip: screenshot written", "path", path, "width", img.Bounds().Dx(), "height", img.Bounds().Dy())
	}
	return nil
}

// publishStats copies this frame's timings into ctx.Stats so the game's
// Draw (next frame) and the overlay (this frame) can show them.
func (l *loop) publishStats() {
	s := &l.ctx.Stats
	s.FPS = l.overlay.fps
	s.UpdateMS = ms(l.updateTime)
	s.DrawMS = ms(l.drawDur)
	s.Updates = l.updates
	s.Scopes = append(s.Scopes[:0], l.ctx.scopes...)
	gs := l.ctx.Gfx.Stats()
	s.Draws2D, s.Vertices2D, s.Draws3D, s.Instances = gs.Draws2D, gs.Vertices2D, gs.Draws3D, gs.Instances
	s.Waits = gs.Waits
	s.GPUFrameMS = gs.GPUFrameMS
	s.GPU = s.GPU[:0]
	for _, sp := range gs.GPU {
		s.GPU = append(s.GPU, Scope{Name: sp.Name, MS: sp.MS})
	}
}

// paused reports whether the game stands still: Config.PauseUnfocused
// with the focus elsewhere, or Config.PauseHidden with the window out of
// sight. Both are off by default, so a loop that sets neither never
// pauses.
func (l *loop) paused() bool {
	return (l.cfg.PauseUnfocused && !l.ctx.focused) || (l.cfg.PauseHidden && !l.ctx.visible)
}

// applyPause silences the mixer when the pause state changes, and only
// then. One place decides it, so a window that loses focus and is hidden
// at once does not have the two settings undo each other, and an event
// that changes nothing leaves the mixer alone: a game that paused its own
// mixer keeps it paused across a focus change it did not ask to react to.
func (l *loop) applyPause() {
	if l.family != nil {
		l.family.applyPause()
		return
	}
	paused := l.paused()
	if paused == l.pausedNow {
		return
	}
	l.pausedNow = paused
	if l.ctx.Audio != nil {
		l.ctx.Audio.SetPaused(paused)
	}
}

func (l *loop) handleEvents(events []platform.Event) {
	in := l.input
	for _, e := range events {
		switch e.Kind {
		case platform.EventClose:
			if l.cfg.HandleClose {
				l.ctx.closeReq = true
			} else {
				l.ctx.quit = true
			}
		case platform.EventResize:
			l.applySize()
		case platform.EventFocus:
			l.ctx.focused = e.Focused
			l.applyPause()
			if !e.Focused {
				in.FeedFocusLost()
			}
		case platform.EventVisible:
			l.ctx.visible = e.Visible
			l.applyPause()
		case platform.EventMouseEnter:
			// The system resets the pointer's shape at the window's edge.
			if w, ok := l.win.(interface{ RefreshCursor() }); ok {
				w.RefreshCursor()
			} else if l.ctx.cursor != CursorArrow {
				l.win.SetCursor(platform.CursorShape(l.ctx.cursor))
			}
		case platform.EventKeyDown:
			in.FeedKey(uint8(e.Key), true, e.Repeat, uint8(e.Mods))
		case platform.EventKeyUp:
			in.FeedKey(uint8(e.Key), false, false, uint8(e.Mods))
		case platform.EventModifiers:
			in.FeedModifiers(uint8(e.Mods))
		case platform.EventChar:
			in.FeedChar(e.Rune)
		case platform.EventCompose:
			in.FeedComposition(e.Text)
		case platform.EventMouseMove:
			in.FeedMouseDelta(l.toViewDelta(e.DX, e.DY))
			if !l.win.CursorCaptured() {
				in.FeedMouseMove(l.toView(e.X, e.Y))
			}
		case platform.EventMouseDown:
			x, y := l.toView(e.X, e.Y)
			in.FeedMouseButton(uint8(e.Button), true, x, y)
		case platform.EventMouseUp:
			x, y := l.toView(e.X, e.Y)
			in.FeedMouseButton(uint8(e.Button), false, x, y)
		case platform.EventScroll:
			dx, dy := float32(e.DX), float32(e.DY)
			if e.Precise {
				// A trackpad reports points; the input state counts lines.
				dx, dy = dx/scrollLinePoints, dy/scrollLinePoints
			}
			in.FeedScroll(dx, dy)
		}
	}
}
