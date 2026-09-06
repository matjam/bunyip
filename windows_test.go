package bunyip

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/render"
)

func TestAdditionalWindowRendersAndClosesIndependently(t *testing.T) {
	cfg := lifecycleConfig(t)
	var child *Window
	var root *Context
	var order []string
	rootShot, childShot := filepath.Join(t.TempDir(), "root.png"), filepath.Join(t.TempDir(), "child.png")
	err := Run(cfg, GameFuncs{InitFunc: func(c *Context) error {
		root = c
		c.Cleanup(func() { order = append(order, "root cleanup") })
		var err error
		child, err = c.NewWindow(Config{Width: 32, Height: 24}, GameFuncs{
			InitFunc: func(cc *Context) error {
				if cc.Audio != c.Audio || cc.Input == c.Input || cc.Gfx == c.Gfx {
					t.Error("incorrect per-window ownership")
				}
				if _, ok := cc.win.(*headlessWindow); !ok {
					t.Error("child did not inherit headless mode")
				}
				cc.Cleanup(func() { order = append(order, "child cleanup") })
				return nil
			},
			DrawFunc: func(cc *Context) error {
				cc.Gfx.FillRect(0, 0, cc.Width, cc.Height, gfx.RGB(0, 255, 0))
				cc.Screenshot(childShot)
				child.Close()
				if child.Closed() {
					t.Error("Closed reported a request as completed teardown")
				}
				return nil
			}, ShutdownFunc: func(*Context) { order = append(order, "child shutdown") },
		})
		return err
	}, DrawFunc: func(c *Context) error {
		c.Gfx.FillRect(0, 0, c.Width, c.Height, gfx.RGB(255, 0, 0))
		if c.Frame == 1 {
			if !child.Closed() {
				t.Error("child survived its close request")
			}
			c.Screenshot(rootShot)
			c.Quit()
		}
		return nil
	}, ShutdownFunc: func(*Context) { order = append(order, "root shutdown") }})
	if err != nil {
		t.Fatal(err)
	}
	if root.Frame != 2 || child.ctx.Frame != 1 || !slices.Equal(order, []string{"child shutdown", "child cleanup", "root shutdown", "root cleanup"}) {
		t.Fatalf("frames=%d/%d, order=%v", root.Frame, child.ctx.Frame, order)
	}
	for _, tc := range []struct {
		path          string
		width, height int
		red           bool
	}{{rootShot, 64, 64, true}, {childShot, 32, 24, false}} {
		f, err := os.Open(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := img.At(tc.width/2, tc.height/2).RGBA()
		if img.Bounds().Dx() != tc.width || img.Bounds().Dy() != tc.height || b > 1000 || tc.red && (r < 60000 || g > 1000) || !tc.red && (g < 60000 || r > 1000) {
			t.Fatalf("incorrect independent capture %s: bounds=%v color=%d,%d,%d", tc.path, img.Bounds(), r, g, b)
		}
	}
}

func TestAdditionalWindowInitRollback(t *testing.T) {
	for _, closeParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "parent_close"}[closeParent], func(t *testing.T) {
			cfg := lifecycleConfig(t)
			var order []string
			var grandchild *Window
			failure := errors.New("child init failed")
			err := Run(cfg, GameFuncs{InitFunc: func(c *Context) error {
				child, err := c.NewWindow(Config{Width: 8, Height: 8}, GameFuncs{InitFunc: func(cc *Context) error {
					cc.Cleanup(func() {
						order = append(order, "child cleanup")
						if _, err := cc.NewWindow(Config{}, GameFuncs{}); err == nil {
							t.Error("child cleanup created a window")
						}
					})
					var err error
					grandchild, err = cc.NewWindow(Config{Width: 8, Height: 8}, GameFuncs{ShutdownFunc: func(*Context) { order = append(order, "grandchild shutdown") }})
					if err != nil {
						return err
					}
					if closeParent {
						c.Quit()
						if _, err := cc.NewWindow(Config{}, GameFuncs{}); err == nil {
							t.Error("descendant created after ancestor Quit")
						}
						return nil
					}
					return failure
				}, ShutdownFunc: func(*Context) { order = append(order, "child shutdown") }})
				if child != nil || err == nil || !closeParent && !errors.Is(err, failure) {
					t.Errorf("rollback result=%v,%v", child, err)
				}
				if grandchild == nil || !grandchild.Closed() {
					t.Error("rollback retained descendant")
				}
				c.Quit()
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"grandchild shutdown", "child cleanup"}
			if closeParent {
				want = []string{"grandchild shutdown", "child shutdown", "child cleanup"}
			}
			if !slices.Equal(order, want) {
				t.Fatalf("rollback order=%v, want=%v", order, want)
			}
		})
	}
}

func TestAdditionalWindowDeviceLossClosesFamilyBeforeRecovery(t *testing.T) {
	cfg := lifecycleConfig(t)
	var child *Window
	var old *Context
	recovered := false
	g := recoverFuncs{GameFuncs: GameFuncs{InitFunc: func(c *Context) error {
		old = c
		var err error
		child, err = c.NewWindow(Config{Width: 8, Height: 8}, GameFuncs{UpdateFunc: func(*Context) error { return render.ErrDeviceLost }})
		return err
	}}, recover: func(c *Context) error {
		recovered = true
		if !child.Closed() || c == old {
			t.Error("recovery retained old window family")
		}
		if _, err := old.NewWindow(Config{}, GameFuncs{}); err == nil {
			t.Error("old context created window during recovery")
		}
		_, err := c.NewWindow(Config{Width: 8, Height: 8}, GameFuncs{})
		c.Quit()
		return err
	}}
	if err := Run(cfg, g); err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("child device loss did not reach application recovery")
	}
}
