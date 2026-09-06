package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/matjam/bunyip/console"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// Window is an additional output managed by Run. Its callbacks and controls
// run on the same game goroutine as the main window. Each output owns its
// Graphics and Input; GPU resources cannot be shared between outputs. Audio
// is shared by every window in the application.
type Window struct {
	ctx                          *Context
	loop                         *loop
	parent                       *Window
	family                       *windowFamily
	release                      func()
	initialized, closed, closing bool
}

// Context returns the context supplied to this window's callbacks. After
// Closed becomes true its graphics resources have been released.
func (w *Window) Context() *Context {
	if w == nil {
		return nil
	}
	return w.ctx
}

// Close requests closure of this window and its descendants after the active
// callbacks finish. It does not close its parent. Call on the game goroutine.
func (w *Window) Close() {
	if w != nil && !w.closed && w.ctx != nil {
		w.ctx.Quit()
	}
}

// Closed reports whether teardown has completed, rather than just requested.
// A nil handle is closed.
func (w *Window) Closed() bool { return w == nil || w.closed }

// NewWindow creates an additional window and calls its optional Init before
// returning. Update and Draw begin in the next scheduler iteration. Failure
// releases everything created for the window, including children and Cleanup
// registrations; Shutdown runs only after successful Init.
//
// The new window inherits the application's headless/native mode and Vulkan
// validation. FixedClock is inherited when the application uses it. Window,
// view, timing and console settings otherwise use Config's normal defaults.
// Log defaults to the creating context's logger. LogFile and Pprof are
// application settings and cannot be set here; NoAudio cannot disable the
// shared mixer. A child context's Quit closes that child and its descendants;
// the main context's Quit ends Run. Callback errors return from Run.
//
// Call on the game goroutine from Init, Update or Draw. Device-loss recovery
// closes all children; recreate them from the main game's Recover callback.
func (c *Context) NewWindow(cfg Config, game Game) (result *Window, err error) {
	if !c.canCreateWindow() {
		return nil, errors.New("bunyip: cannot create a window outside an active window lifetime")
	}
	if game == nil {
		return nil, errors.New("bunyip: window game is nil")
	}
	f := c.owner.family
	if cfg.LogFile != "" || cfg.Pprof != "" {
		return nil, errors.New("bunyip: LogFile and Pprof belong to the application configuration")
	}
	if cfg.NoAudio && !f.root.loop.cfg.NoAudio && !f.root.loop.cfg.Headless {
		return nil, errors.New("bunyip: additional windows share the application audio mixer")
	}
	if cfg.Validation && !f.root.loop.cfg.Validation {
		return nil, errors.New("bunyip: validation must be enabled in the application configuration")
	}
	cfg.Headless = f.root.loop.cfg.Headless
	cfg.Validation = f.root.loop.cfg.Validation
	if f.root.loop.cfg.FixedClock {
		cfg.FixedClock = true
	}
	if cfg.Log == nil {
		cfg.Log = c.Log
	}
	cfg = normalizeWindowConfig(cfg)
	parent, err := cfg.Parent.platform()
	if err != nil {
		return nil, err
	}
	if cfg.Headless && parent != nil {
		return nil, ErrUnsupported
	}
	resources := &Context{}
	w := &Window{parent: c.owner, family: f, release: resources.cleanup}
	complete := false
	defer func() {
		if !complete {
			w.destroy()
		}
	}()
	var native window
	var surface render.SurfaceFunc
	var extensions []string
	if cfg.Headless {
		native = &headlessWindow{w: cfg.Width, h: cfg.Height}
		surface = render.NewHeadlessSurface
		extensions = render.HeadlessSurfaceExtensions()
	} else {
		a, ok := f.app.(*platform.App)
		if !ok {
			return nil, ErrUnsupported
		}
		n, err := a.NewWindow(platform.Config{Parent: parent, Title: cfg.Title, Width: cfg.Width, Height: cfg.Height, Resizable: cfg.Resizable})
		if err != nil {
			return nil, err
		}
		native = n
		surface = n.CreateSurface
		extensions = platform.RequiredInstanceExtensions()
	}
	resources.Cleanup(native.Close)
	pw, ph := native.PixelSize()
	r, err := f.root.loop.renderer.NewOutput(extensions, surface, vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, !cfg.NoVSync)
	if err != nil {
		return nil, err
	}
	resources.Cleanup(r.Destroy)
	gd, err := hook.NewGraphics(r)
	if err != nil {
		return nil, err
	}
	resources.Cleanup(gd.Destroy)
	in := hook.NewInput()
	l := &loop{cfg: cfg, app: f.app, win: native, game: game, gfx: gd, input: in, audio: f.root.loop.audio, renderer: r, family: f}
	w.loop = l
	l.handle = w
	l.ctx = &Context{Gfx: gd.Game().(*gfx.Graphics), Input: in.Game().(*input.State), Audio: f.root.ctx.Audio, Log: cfg.Log, Clear: gfx.RGB(24, 24, 32), win: native, app: f.app, Alpha: 1, focused: cfg.Parent == nil, visible: true, timeScale: 1, budget: cfg.DrawBudget, owner: w, redraw: true}
	w.ctx = l.ctx
	if cfg.Console {
		con := console.New(console.Options{Key: cfg.ConsoleKey})
		l.ctx.Console = con
		l.ctx.Log = slog.New(con.Handler(cfg.Log.Handler()))
		resources.Cleanup(con.Destroy)
	}
	l.overlay.on = cfg.Debug
	l.overlay.budget = cfg.DrawBudget
	resources.Cleanup(l.overlay.destroy)
	resources.Cleanup(l.ctx.cleanup)
	if cfg.Icon != nil {
		native.SetIcon(cfg.Icon)
	}
	l.applySize()
	f.attach(w)
	if init, ok := game.(Initer); ok {
		if err := init.Init(l.ctx); err != nil {
			return nil, fmt.Errorf("bunyip: initialize window %q: %w", cfg.Title, err)
		}
	}
	w.initialized = true
	if !c.canCreateWindow() {
		return nil, errors.New("bunyip: parent closed while initializing window")
	}
	l.resetClock()
	complete = true
	return w, nil
}

func (c *Context) canCreateWindow() bool {
	if c == nil || c.owner == nil || c.owner.family.closing {
		return false
	}
	for w := c.owner; w != nil; w = w.parent {
		if w.closed || w.closing || w.ctx.quit || w.loop.win.Closed() {
			return false
		}
	}
	return true
}

func (w *Window) destroy() {
	if w == nil || w.closed || w.closing {
		return
	}
	w.closing = true
	defer func() { w.closed = true; w.closing = false }()
	if w.release != nil {
		defer w.release()
	}
	if w.initialized {
		if shut, ok := w.loop.game.(Shutdowner); ok {
			defer shut.Shutdown(w.ctx)
		}
	}
	w.family.closeChildren(w)
}

func normalizeWindowConfig(cfg Config) Config {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
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
	return cfg
}
