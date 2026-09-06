package engine

import (
	"context"
	"errors"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/vk"
)

func TestGameFuncs(t *testing.T) {
	var empty GameFuncs
	ctx := &Context{}
	if empty.Init(ctx) != nil || empty.Update(ctx) != nil || empty.Draw(ctx) != nil {
		t.Fatal("empty callbacks should succeed")
	}
	empty.Shutdown(ctx)
	if _, ok := any(empty).(Recoverer); ok {
		t.Fatal("GameFuncs must not opt into device recovery")
	}
	want := errors.New("callback failure")
	var order []string
	callback := func(name string) func(*Context) error {
		return func(got *Context) error {
			if got != ctx {
				t.Fatal("callback received another context")
			}
			order = append(order, name)
			return want
		}
	}
	g := GameFuncs{InitFunc: callback("init"), UpdateFunc: callback("update"), DrawFunc: callback("draw"), ShutdownFunc: func(*Context) { order = append(order, "shutdown") }}
	if !errors.Is(g.Init(ctx), want) || !errors.Is(g.Update(ctx), want) || !errors.Is(g.Draw(ctx), want) {
		t.Fatal("callback error was lost")
	}
	g.Shutdown(ctx)
	if !slices.Equal(order, []string{"init", "update", "draw", "shutdown"}) {
		t.Fatalf("callbacks: %v", order)
	}
}

func TestCleanupDrainsAfterPanic(t *testing.T) {
	ctx := &Context{}
	var order []int
	ctx.Cleanup(nil)
	ctx.Cleanup(func() { order = append(order, 1) })
	ctx.Cleanup(func() { order = append(order, 2); panic("second") })
	ctx.Cleanup(func() {
		order = append(order, 3)
		ctx.Cleanup(func() { order = append(order, 4) })
		panic("first")
	})
	var got any
	func() {
		defer func() { got = recover() }()
		ctx.cleanup()
	}()
	if got != "second" || !slices.Equal(order, []int{3, 4, 2, 1}) {
		t.Fatalf("panic=%v, order=%v", got, order)
	}
	ctx.cleanup()
	if len(order) != 4 || len(ctx.cleanups) != 0 {
		t.Fatal("cleanup ran twice or retained callbacks")
	}
}

type lifecycleValidation struct {
	slog.Handler
	t *testing.T
}

func (h lifecycleValidation) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.t.Errorf("renderer validation: %s", r.Message)
	}
	return h.Handler.Handle(ctx, r)
}

func lifecycleConfig(t *testing.T) Config {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	return Config{Width: 64, Height: 64, Headless: true, NoAudio: true, FixedClock: true, Validation: true,
		Log: slog.New(lifecycleValidation{Handler: slog.NewTextHandler(os.Stderr, nil), t: t})}
}

type recoverFuncs struct {
	GameFuncs
	recover func(*Context) error
}

func (g recoverFuncs) Recover(ctx *Context) error { return g.recover(ctx) }

func TestRunCleanupOrder(t *testing.T) {
	for _, phase := range []string{"success", "init_failure", "recover_failure", "draw_failure"} {
		t.Run(phase, func(t *testing.T) {
			cfg := lifecycleConfig(t)
			var order []string
			var tex *gfx.Texture
			wantErr := errors.New("game failure")
			setup := func(ctx *Context) error {
				order = append(order, "setup")
				var err error
				tex, err = ctx.Gfx.NewBlankTexture(2, 2, gfx.TextureOptions{})
				if err != nil {
					return err
				}
				ctx.Cleanup(func() {
					order = append(order, "first")
					// Write works with open and completed frames and proves the
					// texture/device remain alive. Read rejects the unsubmitted
					// frame left by a Draw failure, before cleanup tears it down.
					if err := tex.Write(0, 0, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil || ctx.Audio == nil {
						t.Error("engine resources closed before cleanup")
					}
				})
				ctx.Cleanup(func() { order = append(order, "last") })
				if phase == "init_failure" || phase == "recover_failure" {
					return wantErr
				}
				return nil
			}
			callbacks := GameFuncs{InitFunc: setup, DrawFunc: func(ctx *Context) error {
				ctx.Gfx.DrawTexture(tex, 0, 0)
				ctx.Quit()
				if phase == "draw_failure" {
					return wantErr
				}
				return nil
			}, ShutdownFunc: func(ctx *Context) {
				order = append(order, "shutdown")
				ctx.Cleanup(func() { order = append(order, "from shutdown") })
			}}
			var game Game = callbacks
			if phase == "recover_failure" {
				cfg.recovering = true
				game = recoverFuncs{GameFuncs: callbacks, recover: setup}
			}
			err := runOnce(cfg, game)
			if phase == "success" && err != nil || phase != "success" && !errors.Is(err, wantErr) {
				t.Fatalf("run result: %v", err)
			}
			wantOrder := []string{"setup", "last", "first"}
			if phase == "success" || phase == "draw_failure" {
				wantOrder = []string{"setup", "shutdown", "from shutdown", "last", "first"}
			}
			if !slices.Equal(order, wantOrder) {
				t.Fatalf("cleanup order %v, want %v", order, wantOrder)
			}
			if _, err := tex.Read(); err == nil {
				t.Fatal("GPU resource survived engine shutdown")
			}
		})
	}
}

func TestWindowDimensionsDefaultIndependently(t *testing.T) {
	for _, tc := range []struct{ width, height, wantW, wantH int }{{320, 0, 320, 720}, {0, 240, 1280, 240}} {
		cfg := lifecycleConfig(t)
		cfg.Width, cfg.Height = tc.width, tc.height
		err := Run(cfg, GameFuncs{InitFunc: func(ctx *Context) error {
			if ctx.Width != float32(tc.wantW) || ctx.Height != float32(tc.wantH) {
				t.Errorf("window=%gx%g, want %dx%d", ctx.Width, ctx.Height, tc.wantW, tc.wantH)
			}
			ctx.Quit()
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestConsoleDrawsAutomatically(t *testing.T) {
	cfg := lifecycleConfig(t)
	cfg.Width, cfg.Height, cfg.Console = 320, 240, true
	shot := filepath.Join(t.TempDir(), "console.png")
	err := Run(cfg, GameFuncs{InitFunc: func(ctx *Context) error {
		ctx.Console.SetOpen(true)
		return nil
	}, DrawFunc: func(ctx *Context) error {
		ctx.Gfx.FillRect(0, 0, ctx.Width, ctx.Height, gfx.RGB(255, 0, 0))
		ctx.Screenshot(shot)
		ctx.Quit()
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	red := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r > 0xf000 && g < 0x1000 && b < 0x1000
	}
	if red(160, 20) || !red(160, 220) {
		t.Fatal("console did not overlay the top of the game's red frame")
	}
}
