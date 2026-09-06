package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestView2DMapping(t *testing.T) {
	v := View2D{Viewport: lin.R(20, 30, 200, 150), Size: lin.V2(1000, 750)}
	if got := v.LocalToParent(lin.V2(500, 375)); got != lin.V2(120, 105) {
		t.Fatal(got)
	}
	if got := v.ParentToLocal(lin.V2(10, 20)); got != lin.V2(-50, -50) {
		t.Fatal(got)
	}
	cam := Camera2D{Position: lin.V2(100, 200), Zoom: 2, Rotation: 0.4}
	for _, p := range []lin.Vec2{lin.V2(100, 200), lin.V2(-30, 600), {}} {
		if got := v.ParentToWorld(v.WorldToParent(p, cam), cam); got.Sub(p).Len() > 0.001 {
			t.Errorf("roundtrip %v -> %v", p, got)
		}
	}
	if got := v.WorldToParent(cam.Position, cam); got.Sub(v.Viewport.Center()).Len() > 0.001 {
		t.Fatalf("camera centre mapped to %v", got)
	}
	v.Size = lin.V2(0, 300)
	if got := v.LocalToParent(lin.V2(200, 300)); got != lin.V2(220, 180) {
		t.Fatal(got)
	}
	child := View2D{Viewport: lin.R(10, 20, 50, 60), Size: lin.V2(100, 120)}
	p := lin.V2(25, 50)
	if got := child.ParentToLocal(v.ParentToLocal(v.LocalToParent(child.LocalToParent(p)))); got.Sub(p).Len() > 0.001 {
		t.Fatal(got)
	}
}

func TestView2DRejectsInvalidBeforeMutation(t *testing.T) {
	for _, v := range []View2D{
		{}, {Viewport: lin.R(0, 0, -1, 10)}, {Viewport: lin.R(0, 0, 10, 10), Size: lin.V2(-1, 0)},
		{Viewport: lin.R(float32(math.NaN()), 0, 10, 10)}, {Viewport: lin.R(0, 0, 10, 10), Size: lin.V2(0, float32(math.Inf(1)))},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid view did not panic")
				}
			}()
			// No queue exists: validation must fail before looking at it.
			new(Graphics).WithView(v, func() { t.Error("invalid view called body") })
		}()
	}
}

func TestWithViewInheritedCameraAndClipping(t *testing.T) {
	g := newHeadless(t, 96, 64)
	cam := Camera2D{Position: lin.V2(400, -200), Zoom: 2, Rotation: 0.7}
	view := View2D{Viewport: lin.R(8, 8, 40, 24), Size: lin.V2(80, 48)}
	img := frame2D(t, g, func() {
		g.WithCamera2D(cam, func() {
			g.WithView(view, func() {
				if w, h := g.View(); w != 80 || h != 48 {
					t.Fatalf("view size %g,%g", w, h)
				}
				g.Draw(nil, Sprite{Pos: cam.Position, Size: lin.V2(12, 12), Origin: lin.V2(0.5, 0.5), Color: RGB(0, 255, 0)})
			})
		})
		g.Clip(lin.R(60, 10, 20, 20), func() {
			g.WithView(View2D{Viewport: lin.R(55, 5, 30, 30), Size: lin.V2(60, 60)}, func() {
				g.Clip(lin.R(0, 0, 30, 60), func() { g.FillRect(-50, -50, 200, 200, RGB(0, 0, 255)) })
			})
		})
		g.FillRect(0, 50, 4, 4, RGB(255, 0, 0))
	})
	if c := img.RGBAAt(28, 20); c.G < 200 {
		t.Fatalf("camera moved the viewport centre: %v", c)
	}
	for _, p := range [][2]int{{7, 20}, {49, 20}, {59, 15}, {71, 15}, {65, 31}} {
		if c := img.RGBAAt(p[0], p[1]); c.R > 10 || c.G > 10 || c.B > 10 {
			t.Errorf("outside view/clip %v: %v", p, c)
		}
	}
	if c := img.RGBAAt(65, 15); c.B < 200 {
		t.Fatal(c)
	}
	if c := img.RGBAAt(2, 52); c.R < 200 {
		t.Fatal("view state was not restored", c)
	}
}

func TestWithViewNestedGeometryParticlesAndLetterbox(t *testing.T) {
	g := newHeadless(t, 128, 96)
	if err := g.SetViewport(lin.R(16, 8, 96, 80)); err != nil {
		t.Fatal(err)
	}
	g.SetView(96, 80)
	v, ix := geometryQuad(RGB(255, 0, 0))
	m, err := g.NewGeometry2D(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	img := frame2D(t, g, func() {
		g.WithView(View2D{Viewport: lin.R(8, 8, 64, 56), Size: lin.V2(128, 112)}, func() {
			g.WithView(View2D{Viewport: lin.R(16, 16, 64, 64), Size: lin.V2(32, 32)}, func() {
				g.DrawGeometry(nil, m)
				g.DrawParticles(nil, []ParticleQuad{quad(24, 24, 40, RGB(0, 255, 0))})
			})
		})
		g.FillRect(0, 70, 8, 8, RGB(0, 0, 255))
	})
	// Child top-left in pixels: letterbox(16,8)+parent(8,8)+child(8,8).
	if c := img.RGBAAt(33, 25); c.R < 200 {
		t.Fatal(c)
	}
	if c := img.RGBAAt(50, 42); c.G < 200 {
		t.Fatal(c)
	}
	for _, p := range [][2]int{{31, 40}, {65, 40}, {45, 57}, {10, 10}} {
		if c := img.RGBAAt(p[0], p[1]); c.R > 10 || c.G > 10 || c.B > 10 {
			t.Errorf("viewport leaked at %v: %v", p, c)
		}
	}
	if c := img.RGBAAt(20, 82); c.B < 200 {
		t.Fatal("particle scissor leaked into later draw", c)
	}
}

func TestWithViewFrameSnapshotsAndPanic(t *testing.T) {
	g := newHeadless(t, 96, 64)
	g.SetView(48, 32)
	frame2D(t, g, func() {
		q := g.cur
		beforeProj := q.proj
		g.FillRect(0, 0, 1, 1, White)
		func() {
			defer func() {
				if recover() != "view panic" {
					t.Error("panic was not propagated")
				}
			}()
			g.WithView(View2D{Viewport: lin.R(4, 4, 20, 12), Size: lin.V2(100, 60)}, func() {
				g.FillRect(0, 0, 1, 1, White)
				g.DrawParticles(nil, []ParticleQuad{quad(1, 1, 1, White)})
				g.WithView(View2D{Viewport: lin.R(10, 10, 40, 20), Size: lin.V2(80, 40)}, func() {
					g.FillRect(0, 0, 1, 1, White)
					g.DrawParticles(nil, []ParticleQuad{quad(1, 1, 1, White)})
					panic("view panic")
				})
			})
		}()
		if q.viewW != 48 || q.viewH != 32 || q.proj != beforeProj || !q.layout.IsIdentity() || len(q.clips) != 0 {
			t.Fatal("panic did not restore view")
		}
		want := []lin.Vec4{lin.V4(g.time, 48, 32, 2), lin.V4(g.time, 100, 60, 0.4), lin.V4(g.time, 80, 40, 0.2)}
		if len(q.stream.items) != len(want) {
			t.Fatal("lost queued draws")
		}
		for i, frame := range want {
			if q.stream.items[i].state.frame != frame {
				t.Errorf("draw%d frame %v, want %v", i, q.stream.items[i].state.frame, frame)
			}
		}
		for i, p := range q.parts.flat {
			if p.frame != want[i+1] {
				t.Errorf("particle%d frame %v", i, p.frame)
			}
		}
	})
}

func TestWithViewRejectsComposedOverflowAndOutputMutation(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.WithView(View2D{Viewport: lin.R(0, 0, 1e20, 1e20), Size: lin.V2(1, 1)}, func() {
		q := g.cur
		layout, projection, clipCount := q.layout, q.proj, len(q.clips)
		for _, attempt := range []func(){
			func() {
				g.WithView(View2D{Viewport: lin.R(0, 0, 1e20, 1e20), Size: lin.V2(1, 1)}, func() { t.Error("overflow called body") })
			},
			func() { g.SetView(200, 100) },
			func() { _ = g.SetViewport(lin.R(0, 0, 20, 20)) },
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Error("invalid nested mutation did not panic")
					}
				}()
				attempt()
			}()
			if q.layout != layout || q.proj != projection || len(q.clips) != clipCount || g.viewScopes != 1 {
				t.Fatal("rejected operation changed view state")
			}
		}
	})
	if g.viewScopes != 0 {
		t.Fatal("view scope count leaked")
	}
}

func TestWithViewRenderTextureNesting(t *testing.T) {
	g := newHeadless(t, 64, 64)
	rt, err := g.NewRenderTexture(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	img := frame2D(t, g, func() {
		g.WithView(View2D{Viewport: lin.R(16, 16, 32, 32), Size: lin.V2(64, 64)}, func() {
			g.DrawTo(rt, Black, func() {
				if w, h := g.View(); w != 32 || h != 32 {
					t.Fatalf("render texture inherited outer view size %g,%g", w, h)
				}
				g.WithView(View2D{Viewport: lin.R(8, 8, 16, 16)}, func() {
					g.ScreenSpace()
					g.FillRect(-8, -8, 32, 32, RGB(0, 255, 0))
				})
			})
			if w, h := g.View(); w != 64 || h != 64 {
				t.Fatalf("outer view not restored: %g,%g", w, h)
			}
			g.Draw(rt.Texture(), Sprite{Size: lin.V2(64, 64)})
		})
	})
	if c := img.RGBAAt(32, 32); c.G < 200 {
		t.Fatal("target view did not render", c)
	}
	for _, p := range [][2]int{{20, 20}, {10, 32}, {50, 32}} {
		if c := img.RGBAAt(p[0], p[1]); c.R > 10 || c.G > 10 || c.B > 10 {
			t.Errorf("target or outer view clipping leaked at %v: %v", p, c)
		}
	}
}
