package gfx

import (
	"reflect"
	"testing"

	"github.com/matjam/bunyip/lin"
)

type scopeState struct {
	layer   int32
	blend   Blend
	shader  *Shader
	matrix  *ColorMatrix
	xform   lin.Affine
	xforms  []lin.Affine
	clips   []lin.Rect
	cam     Camera2D
	hasCam  bool
	proj    lin.Mat4
	visible lin.Rect
}

func snapshotScope(q *drawQueue) scopeState {
	return scopeState{q.layer, q.blend, q.shader, q.colorMatrix, q.xform,
		append([]lin.Affine(nil), q.xforms...), append([]lin.Rect(nil), q.clips...),
		q.cam2D, q.hasCam2D, q.spriteProj, q.visible}
}

func TestDrawingScopesRestore(t *testing.T) {
	g := newHeadless(t, 64, 64)
	for _, tc := range []struct {
		name string
		run  func(func())
	}{
		{"layer", func(fn func()) { g.Layered(7, fn) }},
		{"camera", func(fn func()) { g.WithCamera2D(Camera2D{Position: lin.V2(4, 5), Zoom: 2}, fn) }},
		{"blend", func(fn func()) { g.Blended(BlendAdd, fn) }},
		{"transform", func(fn func()) { g.Transformed(lin.Translate2(10, 20), fn) }},
		{"shader", func(fn func()) { g.Shaded(&Shader{g: g}, fn) }},
		{"color matrix", func(fn func()) { g.ColorMatrixed(Invert(), fn) }},
		{"clip", func(fn func()) { g.Clip(lin.Rect{X: 2, Y: 3, W: 20, H: 25}, fn) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshotScope(g.cur)
			tc.run(func() {
				outer := snapshotScope(g.cur)
				tc.run(func() {})
				if !reflect.DeepEqual(snapshotScope(g.cur), outer) {
					t.Fatal("inner scope did not restore outer state")
				}
			})
			if !reflect.DeepEqual(snapshotScope(g.cur), before) {
				t.Fatal("normal return did not restore state")
			}
			func() {
				defer func() {
					if recover() != "scope panic" {
						t.Error("panic was not propagated")
					}
				}()
				tc.run(func() { tc.run(func() { panic("scope panic") }) })
			}()
			if !reflect.DeepEqual(snapshotScope(g.cur), before) {
				t.Fatal("panic did not restore state")
			}
		})
	}
}

func TestConfigurePostPreservesZerosAndPanic(t *testing.T) {
	g := &Graphics{}
	g.SetPost(DefaultPost())
	want := g.Post()
	want.Exposure, want.Saturation, want.Bloom = 0, 0, 0
	g.ConfigurePost(func(p *PostSettings) {
		p.Exposure, p.Saturation, p.Bloom = 0, 0, 0
	})
	if g.Post() != want {
		t.Fatalf("post settings=%+v, want %+v", g.Post(), want)
	}
	func() {
		defer func() {
			if recover() != "edit panic" {
				t.Error("panic was not propagated")
			}
		}()
		g.ConfigurePost(func(p *PostSettings) { p.Contrast = 9; panic("edit panic") })
	}()
	if g.Post() != want {
		t.Fatal("panicking edit committed partial post settings")
	}
}

func TestDrawToPanicRestoresScopes(t *testing.T) {
	g := newHeadless(t, 16, 16)
	a, err := g.NewRenderTexture(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.NewRenderTexture(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	img := render2D(t, g, Black, func() {
		original := g.cur
		g.ColorMatrixed(Invert(), func() {
			before := snapshotScope(original)
			func() {
				defer func() {
					if recover() != "target panic" {
						t.Error("panic was not propagated")
					}
				}()
				g.Layered(8, func() {
					g.WithCamera2D(Camera2D{Zoom: 2}, func() {
						g.DrawTo(a, Black, func() {
							g.ColorMatrixed(Grayscale(), func() {
								g.DrawTo(b, Black, func() {
									g.Transformed(lin.Translate2(2, 3), func() {
										g.Clip(lin.Rect{W: 4, H: 4}, func() { panic("target panic") })
									})
								})
							})
						})
					})
				})
			}()
			if g.cur != original || !reflect.DeepEqual(snapshotScope(original), before) {
				t.Fatal("nested target panic changed the original output or drawing state")
			}
			if a.queue.colorMatrix != nil || len(b.queue.clips) != 0 || len(b.queue.xforms) != 0 {
				t.Fatal("nested target state was not restored")
			}
			g.FillRect(0, 0, 16, 16, Color{R: 1, A: 1})
		})
	})
	p := img.RGBAAt(8, 8)
	if p.R > 30 || p.G < 200 || p.B < 200 {
		t.Fatalf("shared color-matrix uniforms not restored after panic: %v, want cyan", p)
	}
}
