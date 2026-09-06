package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestBlendOptionsPresets(t *testing.T) {
	for b := Blend(0); b < blendCount; b++ {
		if got, want := *b.Options().factors(), *b.factors(); got != want {
			t.Errorf("%s options = %v, want %v", b, got, want)
		}
	}
}

func TestCustomBlendedRenderedAndRestored(t *testing.T) {
	g := newHeadless(t, 64, 32)
	options := BlendReplace.Options()
	options.SrcColor, options.DstColor = FactorZero, FactorOne
	options.SrcAlpha, options.DstAlpha = FactorZero, FactorOne
	v, ix := geometryQuad(RGB(255, 0, 0))
	geometry, err := g.NewGeometry2D(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	img := frame2D(t, g, func() {
		g.FillRect(0, 0, 64, 32, RGB(0, 0, 255))
		g.CustomBlended(options, func() {
			g.DrawGeometry(nil, geometry)
			g.DrawParticles(nil, []ParticleQuad{quad(40, 12, 20, RGB(255, 0, 0))})
			g.Blended(BlendReplace, func() { g.FillRect(0, 24, 8, 8, RGB(0, 255, 0)) })
			g.FillRect(8, 24, 8, 8, RGB(255, 0, 0))
		})
		func() {
			defer func() { _ = recover() }()
			g.CustomBlended(BlendOptions{}, func() { panic("blend") })
		}()
		g.FillRect(56, 0, 8, 8, RGB(255, 0, 0))
		g.CustomBlended(BlendOptions{}, func() { g.FillRect(56, 24, 8, 8, White) })
	})
	for _, p := range [][2]int{{12, 12}, {40, 12}, {12, 28}} {
		if c := img.RGBAAt(p[0], p[1]); c.B < 200 || c.R > 10 {
			t.Errorf("custom destination-only blend at %v: %v", p, c)
		}
	}
	if c := img.RGBAAt(4, 28); c.G < 200 {
		t.Fatal("built-in scope did not replace custom equations", c)
	}
	if c := img.RGBAAt(60, 4); c.R < 200 {
		t.Fatal("blend state was not restored", c)
	}
	if c := img.RGBAAt(60, 28); c.R > 10 || c.G > 10 || c.B > 10 {
		t.Fatal("zero factors were replaced with defaults", c)
	}
}

func TestStencilFailAndWrappingOperations(t *testing.T) {
	g := newHeadless(t, 32, 16)
	img := frame2D(t, g, func() {
		g.ClearStencil(255)
		g.Stenciled(StencilOptions{Pass: StencilIncrementWrap, NoColor: true}, func() { g.FillRect(0, 0, 16, 16, White) })
		g.Stenciled(StencilOptions{Test: StencilNever, Fail: StencilZero, NoColor: true}, func() { g.FillRect(16, 0, 16, 16, White) })
		g.Stenciled(StencilOptions{Test: StencilLessEqual, Reference: 0, DisableWrite: true}, func() { g.FillRect(0, 0, 32, 16, RGB(0, 255, 0)) })
	})
	for _, x := range []int{8, 24} {
		if c := img.RGBAAt(x, 8); c.G < 200 {
			t.Errorf("stencil wrapping/fail operation at %d: %v", x, c)
		}
	}
}

func TestRenderControlsRejectInvalidOptionsBeforeMutation(t *testing.T) {
	for _, attempt := range []func(){
		func() {
			new(Graphics).CustomBlended(BlendOptions{SrcColor: blendFactorCount}, func() { t.Error("invalid blend body") })
		},
		func() {
			new(Graphics).CustomBlended(BlendOptions{AlphaOp: blendEquationCount}, func() { t.Error("invalid blend body") })
		},
		func() {
			new(Graphics).Stenciled(StencilOptions{DepthFail: stencilOpCount}, func() { t.Error("invalid stencil body") })
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid options did not panic")
				}
			}()
			attempt()
		}()
	}
}

func TestMaskedNestedLayersGeometryAndParticles(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, ix := geometryQuad(RGB(255, 0, 0))
	geometry, err := g.NewGeometry2D(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	img := frame2D(t, g, func() {
		g.Layered(100, func() { g.FillRect(0, 0, 64, 64, RGB(0, 0, 255)) })
		g.Masked(func() {
			g.SetLayer(1000)
			g.SetSortKey(1000)
			g.FillRect(8, 8, 48, 48, White)
		}, func() {
			g.SetLayer(-1000)
			g.SetSortKey(-1000)
			g.Transformed(lin.Scale2(3, 3), func() { g.DrawGeometry(nil, geometry) })
			g.Masked(func() {
				g.Layered(10000, func() { g.FillRect(0, 0, 64, 32, White) })
			}, func() {
				g.Layered(-10000, func() { g.DrawParticles(nil, []ParticleQuad{quad(32, 32, 64, RGB(0, 255, 0))}) })
			})
		})
		// The mask bit must be gone, and an earlier high layer must not
		// sort across mask setup/body boundaries.
		g.Stenciled(StencilOptions{Test: StencilEqual, Reference: 0, DisableWrite: true}, func() {
			g.Layered(-20000, func() { g.FillRect(0, 60, 64, 4, White) })
		})
	})
	if c := img.RGBAAt(32, 20); c.G < 200 || c.R > 10 || c.B > 10 {
		t.Fatal("nested particle mask", c)
	}
	if c := img.RGBAAt(32, 44); c.R < 200 || c.G > 10 || c.B > 10 {
		t.Fatal("outer geometry mask", c)
	}
	if c := img.RGBAAt(4, 20); c.B < 200 || c.R > 10 || c.G > 10 {
		t.Fatal("mask changed colour outside coverage", c)
	}
	if c := img.RGBAAt(32, 62); c.R < 200 || c.G < 200 || c.B < 200 {
		t.Fatal("mask cleanup did not clear owned bits", c)
	}
}

func TestStencilMasksClearClipAndPanic(t *testing.T) {
	g := newHeadless(t, 64, 32)
	img := frame2D(t, g, func() {
		g.ClearStencil(0x80)
		g.Stenciled(StencilOptions{Test: StencilEqual, Reference: 0x80, ReadMask: 0x80, DisableWrite: true}, func() {
			before := g.cur.stencil2D
			func() {
				defer func() { _ = recover() }()
				g.Masked(func() { g.FillRect(0, 0, 64, 32, White) }, func() { panic("mask") })
			}()
			if g.cur.stencil2D != before || g.cur.maskDepth != 0 {
				t.Fatal("masked panic did not restore advanced stencil state")
			}
			g.FillRect(0, 0, 64, 32, RGB(255, 0, 0))
		})
		g.Clip(lin.R(32, 0, 32, 32), func() { g.ClearStencil(0) })
		g.Stenciled(StencilOptions{Test: StencilEqual, Reference: 0, DisableWrite: true}, func() {
			g.FillRect(0, 0, 64, 32, RGB(0, 255, 0))
		})
	})
	if c := img.RGBAAt(16, 16); c.R < 200 || c.G > 10 {
		t.Fatal("mask changed a stencil bit it did not own, or clipped clear leaked", c)
	}
	if c := img.RGBAAt(48, 16); c.G < 200 || c.R > 10 {
		t.Fatal("clipped clear did not apply", c)
	}
}

func TestMaskedComposesInsideMaskCallback(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// This drawing helper itself uses a mask. It must compose when used
	// as coverage for another mask, including a further nested body mask.
	helper := func() {
		g.Masked(func() {
			g.FillRect(8, 8, 32, 40, White)
		}, func() {
			g.Masked(func() { g.FillRect(0, 0, 64, 24, White) }, func() {
				g.FillRect(0, 0, 64, 64, RGB(255, 0, 0))
			})
		})
	}
	img := frame2D(t, g, func() {
		g.FillRect(0, 0, 64, 64, RGB(0, 0, 255))
		g.Masked(helper, func() {}) // coverage drawing must not write colour
		g.Masked(helper, func() { g.FillRect(0, 0, 64, 64, RGB(0, 255, 0)) })
	})
	if c := img.RGBAAt(20, 16); c.G < 200 || c.R > 10 || c.B > 10 {
		t.Fatal("composed mask did not cover helper's intersection", c)
	}
	for _, p := range [][2]int{{4, 16}, {44, 16}, {20, 32}} {
		if c := img.RGBAAt(p[0], p[1]); c.B < 200 || c.R > 10 || c.G > 10 {
			t.Errorf("composed mask wrote colour or leaked at %v: %v", p, c)
		}
	}
}

func TestStencilRejectsDepthlessAndExcessNesting(t *testing.T) {
	g := newHeadless(t, 32, 32)
	rt, err := g.NewRenderTextureOptions(16, 16, RenderTextureOptions{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}
	frame2D(t, g, func() {
		g.DrawTo(rt, Black, func() {
			for _, attempt := range []func(){
				func() { g.Masked(func() { t.Error("depthless mask callback") }, func() {}) },
				func() { g.Stenciled(StencilOptions{}, func() { t.Error("depthless stencil callback") }) },
				func() { g.ClearStencil(0) },
			} {
				func() {
					defer func() {
						if recover() == nil {
							t.Error("depthless stencil did not panic")
						}
					}()
					attempt()
				}()
			}
		})
		var nest func(int)
		nest = func(n int) {
			if n == 8 {
				count, group := len(g.cur.stream.items), g.cur.stream.group
				func() {
					defer func() {
						if recover() == nil {
							t.Error("ninth mask did not panic")
						}
					}()
					g.Masked(func() { t.Error("ninth mask callback") }, func() {})
				}()
				if len(g.cur.stream.items) != count || g.cur.stream.group != group || g.cur.maskDepth != 8 {
					t.Fatal("rejected ninth mask mutated queue")
				}
				return
			}
			g.Masked(func() { g.FillRect(0, 0, 32, 32, White) }, func() { nest(n + 1) })
		}
		nest(0)
	})
}
