package bunyip

import (
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"time"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/audioout"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// audioRate is the mixer and device sample rate.
const audioRate = 48000

// Run opens the window, drives game until it quits or the window closes,
// and tears everything down. It must be called from the main goroutine.
func Run(cfg Config, game Game) error {
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
	if cfg.FixedStep <= 0 {
		cfg.FixedStep = time.Second / 60
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		cfg.Width, cfg.Height = 1280, 720
	}
	if cfg.Title == "" {
		cfg.Title = "Bunyip"
	}
	app, err := platform.NewApp()
	if err != nil {
		return err
	}
	win, err := app.NewWindow(platform.Config{Title: cfg.Title, Width: cfg.Width, Height: cfg.Height, Resizable: cfg.Resizable})
	if err != nil {
		return err
	}
	defer win.Close()
	pw, ph := win.PixelSize()
	r, err := render.NewRenderer(render.Config{AppName: cfg.Title, Validation: cfg.Validation, Log: cfg.Log},
		platform.RequiredInstanceExtensions(), win.CreateSurface, vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, !cfg.NoVSync)
	if err != nil {
		return err
	}
	defer r.Destroy()
	g, err := gfx.New(r)
	if err != nil {
		return err
	}
	defer g.Destroy()

	mixer := audio.NewMixer(audioRate)
	if !cfg.NoAudio {
		dev, err := audioout.Open(audioRate, mixer.Mix)
		if err != nil {
			cfg.Log.Warn("bunyip: audio output unavailable, continuing silent", "err", err)
		} else {
			defer dev.Close()
		}
	}
	l := &loop{cfg: cfg, app: app, win: win, game: game, ctx: &Context{Gfx: g, Input: &input.State{}, Log: cfg.Log, Audio: mixer, Clear: gfx.RGB(24, 24, 32), win: win, app: app}}
	l.overlay.on = cfg.Debug
	defer l.overlay.destroy() // before g.Destroy, which was deferred earlier
	if cfg.Pprof != "" && !cfg.recovering {
		servePprof(cfg.Pprof, l.ctx)
	}
	l.applySize(win)
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
		defer shut.Shutdown(l.ctx)
	}
	return l.run()
}

type loop struct {
	cfg     Config
	app     *platform.App
	win     *platform.Window
	game    Game
	ctx     *Context
	overlay overlay

	// Timings gathered during the frame, published to ctx.Stats at its end.
	frameStart          time.Time
	updateTime, drawDur time.Duration
	updates             int
}

func (l *loop) applySize(w *platform.Window) {
	pw, ph := w.PixelSize()
	width, height := w.Size()
	l.ctx.Width, l.ctx.Height, l.ctx.Scale = float32(width), float32(height), float32(w.Scale())
	l.ctx.Gfx.Resize(pw, ph)
	l.ctx.Gfx.SetView(float32(width), float32(height))
}

func (l *loop) run() error {
	start := time.Now()
	last := start
	var accumulator time.Duration
	step := l.cfg.FixedStep
	for !l.ctx.quit && !l.win.Closed() {
		wait := l.cfg.TurnBased && !l.ctx.redraw
		l.ctx.redraw = false
		l.handleEvents(l.app.Poll(wait))
		if l.ctx.quit || l.win.Closed() {
			break
		}
		for i, g := range l.app.Gamepads() {
			l.ctx.Input.FeedGamepad(i, g.Connected, g.Name, g.Buttons, g.Axes)
		}
		l.overlay.toggle(l.ctx.Input)
		now := time.Now()
		l.beginFrame(now)
		l.ctx.Time = now.Sub(start).Seconds()
		if l.cfg.TurnBased {
			l.ctx.Delta = now.Sub(last).Seconds()
			last = now
			if err := l.update(); err != nil {
				return err
			}
		} else {
			accumulator += now.Sub(last)
			last = now
			if accumulator > 250*time.Millisecond { // do not spiral after a stall
				accumulator = 250 * time.Millisecond
			}
			l.ctx.Delta = step.Seconds()
			for accumulator >= step {
				accumulator -= step
				if err := l.update(); err != nil {
					return err
				}
			}
		}
		if err := l.draw(); err != nil {
			return err
		}
	}
	return nil
}

func (l *loop) update() error {
	start := time.Now()
	err := l.game.Update(l.ctx)
	l.updateTime += time.Since(start)
	l.updates++
	l.ctx.Input.EndUpdate()
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

func (l *loop) draw() error {
	// Draw sees every input edge since the last frame, so an interface
	// built here reacts to clicks that Update already consumed.
	l.ctx.Input.SetDrawing(true)
	l.ctx.Gfx.SetTime(l.ctx.Time)
	defer func() {
		l.ctx.Input.SetDrawing(false)
		l.ctx.Input.EndFrame()
	}()
	ok, err := l.ctx.Gfx.Begin(l.ctx.Clear)
	if err != nil || !ok {
		return err
	}
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
	capture := l.ctx.shot != ""
	presentStart := time.Now()
	img, err := l.ctx.Gfx.End(capture)
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
}

func (l *loop) handleEvents(events []platform.Event) {
	in := l.ctx.Input
	for _, e := range events {
		switch e.Kind {
		case platform.EventClose:
			l.ctx.quit = true
		case platform.EventResize:
			l.applySize(e.Window)
		case platform.EventFocus:
			if !e.Focused {
				in.FeedFocusLost()
			}
		case platform.EventKeyDown:
			in.FeedKey(e.Key, true, e.Repeat, e.Mods)
		case platform.EventKeyUp:
			in.FeedKey(e.Key, false, false, e.Mods)
		case platform.EventChar:
			in.FeedChar(e.Rune)
		case platform.EventCompose:
			in.FeedComposition(e.Text)
		case platform.EventMouseMove:
			in.FeedMouseDelta(e.DX, e.DY)
			if !l.win.CursorCaptured() {
				in.FeedMouseMove(e.X, e.Y)
			}
		case platform.EventMouseDown:
			in.FeedMouseButton(e.Button, true, e.X, e.Y)
		case platform.EventMouseUp:
			in.FeedMouseButton(e.Button, false, e.X, e.Y)
		case platform.EventScroll:
			in.FeedScroll(e.DX, e.DY)
		}
	}
}
