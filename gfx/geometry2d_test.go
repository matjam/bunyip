package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

func geometryQuad(c Color) ([]Vertex2D, []uint32) {
	return []Vertex2D{{Pos: lin.V2(0, 0), Color: c}, {Pos: lin.V2(24, 0), UV: lin.V2(1, 0), Color: c}, {Pos: lin.V2(24, 24), UV: lin.V2(1, 1), Color: c}, {Pos: lin.V2(0, 24), UV: lin.V2(0, 1), Color: c}}, []uint32{0, 1, 2, 0, 2, 3}
}

func TestGeometry2DMatchesImmediate(t *testing.T) {
	g := newHeadless(t, 96, 96)
	pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixels.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	pixels.SetRGBA(1, 0, color.RGBA{0, 255, 0, 255})
	pixels.SetRGBA(0, 1, color.RGBA{0, 0, 255, 255})
	pixels.SetRGBA(1, 1, color.RGBA{255, 255, 255, 255})
	tex, err := g.NewTexture(pixels, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	verts, indices := geometryQuad(Color{R: 1, G: 1, B: 1, A: 0.75})
	geometry, err := g.NewGeometry2D(verts, indices)
	if err != nil {
		t.Fatal(err)
	}
	expanded := make([]Vertex2D, len(indices))
	for i, index := range indices {
		expanded[i] = verts[index]
	}
	nonindexed, err := g.NewGeometry2D(expanded, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"only", "ordered", "sorted", "keyed", "lit", "target"} {
		t.Run(mode, func(t *testing.T) {
			rt, err := g.NewRenderTexture(96, 96)
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Destroy()
			render := func(persistent bool) *image.RGBA {
				return frame2D(t, g, func() {
					draw := func() {
						if persistent {
							before := len(g.cur.stream.verts)
							g.DrawGeometry(tex, geometry)
							if len(g.cur.stream.verts) != before {
								t.Fatal("persistent draw copied vertices into the CPU stream")
							}
						} else {
							g.DrawIndexed(tex, verts, indices)
						}
					}
					scene := func() {
						if mode == "only" {
							draw()
							return
						}
						if mode == "lit" {
							g.SetLights2D(Black, Light2D{Pos: lin.V2(55, 40), Radius: 75, Height: 5})
							if err := g.litShader.SetUniforms(&g.cur.lights); err != nil {
								t.Fatal(err)
							}
							g.Shaded(g.litShader, func() { g.Transformed(lin.Translate2(24, 24).Mul(lin.Scale2(2, 2)), draw) })
							return
						}
						g.FillRect(0, 0, 96, 96, RGB(30, 40, 50))
						for i := range 10 {
							layer := 0
							if mode == "sorted" {
								layer = 9 - i
							}
							g.Layered(layer, func() {
								if mode == "keyed" {
									g.SetSortKey(float32(9 - i))
								}
								g.WithCamera2D(Camera2D{Position: lin.V2(48+float32(i), 48), Zoom: 1}, func() {
									g.Clip(lin.R(3, 3, 85, 85), func() {
										g.Transformed(lin.Translate2(float32(i*6), float32(i*5)).Mul(lin.Shear2(0.5, 0)), draw)
									})
								})
								g.SetSortKey(0)
							})
							g.FillRect(float32(i*6), 70, 5, 8, RGB(0, 120, 170))
						}
						g.ColorMatrixed(Invert(), func() { g.Transformed(lin.Translate2(64, 4), draw) })
						g.Blended(BlendAdd, func() { g.Transformed(lin.Translate2(8, 60), draw) })
						g.FillRect(60, 60, 30, 30, RGB(10, 20, 30))
						g.Transformed(lin.Translate2(64, 64), func() {
							if persistent {
								g.DrawGeometry(tex, nonindexed)
							} else {
								g.DrawTriangles(tex, expanded)
							}
						})
					}
					if mode == "target" {
						g.DrawTo(rt, Black, scene)
						g.DrawTexture(rt.Texture(), 0, 0)
					} else {
						scene()
					}
				})
			}
			want, got := render(false), render(true)
			if diff := imageDiff(want, got); diff != 0 {
				t.Fatalf("persistent geometry differs at %d pixels", diff)
			}
		})
	}
}

func TestGeometry2DUpdateDestroySnapshots(t *testing.T) {
	g := newHeadless(t, 80, 32)
	verts, indices := geometryQuad(RGB(255, 0, 0))
	m, err := g.NewGeometry2D(verts, indices)
	if err != nil {
		t.Fatal(err)
	}
	img := frame2D(t, g, func() {
		g.DrawGeometry(nil, m)
		verts, _ = geometryQuad(RGB(0, 255, 0))
		if err := m.Update(verts, indices); err != nil {
			t.Fatal(err)
		}
		g.Transformed(lin.Translate2(26, 0), func() { g.DrawGeometry(nil, m) })
		m.Destroy()
		m.Destroy()
		g.Transformed(lin.Translate2(52, 0), func() { g.DrawGeometry(nil, m) })
	})
	for _, tc := range []struct {
		x int
		c color.RGBA
	}{{12, color.RGBA{255, 0, 0, 255}}, {38, color.RGBA{0, 255, 0, 255}}, {64, color.RGBA{0, 0, 0, 255}}} {
		if got := img.RGBAAt(tc.x, 12); !closeColor(got, tc.c) {
			t.Errorf("x=%d: %v, want %v", tc.x, got, tc.c)
		}
	}
	if err := m.Update(verts, indices); err == nil {
		t.Fatal("updated destroyed geometry")
	}
}

func TestGeometry2DParticlesOrder(t *testing.T) {
	g := newHeadless(t, 32, 32)
	v, ix := geometryQuad(RGB(255, 0, 0))
	m, err := g.NewGeometry2D(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	for _, after := range []bool{false, true} {
		img := frame2D(t, g, func() {
			particles := func() { g.DrawParticles(nil, []ParticleQuad{quad(12, 12, 20, RGB(0, 255, 0))}) }
			if !after {
				particles()
			}
			g.DrawGeometry(nil, m)
			if after {
				particles()
			}
		})
		c := img.RGBAAt(12, 12)
		if after && c.G < 200 || !after && c.R < 200 {
			t.Errorf("particles after=%v: centre %v", after, c)
		}
	}
}

func TestGeometry2DValidationAndOwnership(t *testing.T) {
	g := newHeadless(t, 32, 32)
	baseline := g.r.Device.Stats()
	v, ix := geometryQuad(White)
	m, err := g.NewGeometry2D(v, ix)
	if err != nil {
		t.Fatal(err)
	}
	want := m.Bounds()
	if want != lin.R(0, 0, 24, 24) {
		t.Fatal(want)
	}
	for _, indices := range [][]uint32{{0, 1}, {0, 1, 8}} {
		if err := m.Update(v, indices); err == nil {
			t.Fatal("accepted invalid indices")
		}
		if m.Bounds() != want {
			t.Fatal("failed update changed bounds")
		}
	}
	bad := append([]Vertex2D(nil), v...)
	bad[0].Pos.X = float32(math.NaN())
	if err := m.Update(bad, ix); err == nil {
		t.Fatal("accepted non-finite position")
	}
	if err := m.Update(nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.Bounds() != (lin.Rect{}) {
		t.Fatal("empty geometry has bounds")
	}
	m.Destroy()
	if got := g.r.Device.Stats(); got.Live != baseline.Live || got.Used != baseline.Used {
		t.Fatalf("geometry leaked memory: %v, baseline %v", got, baseline)
	}
	// Owner cleanup covers resources the application never destroyed.
	m, err = g.NewGeometry2D(v[:3], nil)
	if err != nil {
		t.Fatal(err)
	}
	g.destroy()
	if m.data != nil {
		t.Fatal("graphics did not destroy geometry")
	}
}

func TestCompiledPathMatchesImmediate(t *testing.T) {
	g := newHeadless(t, 64, 64)
	gr, err := g.NewGradient(GradientStop{0, RGB(255, 0, 0)}, GradientStop{1, RGB(0, 0, 255)})
	if err != nil {
		t.Fatal(err)
	}
	gr.Linear(lin.V2(8, 8), lin.V2(48, 48))
	p := new(Path).RoundRect(8, 8, 40, 40, 7)
	for _, mode := range []string{"default", "stroke", "both"} {
		t.Run(mode, func(t *testing.T) {
			opts := PathOptions{}
			if mode != "default" {
				opts.Stroke = &StrokeOptions{Width: 3, Join: JoinRound, Dash: []float32{5, 2}}
				opts.StrokeColor = RGB(0, 255, 0)
			}
			if mode == "both" {
				opts.Fill = &FillOptions{Gradient: gr}
			}
			cp, err := g.CompilePath(p, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer cp.Destroy()
			want := frame2D(t, g, func() {
				if mode == "default" {
					g.FillPath(p, White, FillOptions{})
				}
				if opts.Fill != nil {
					g.FillPath(p, White, *opts.Fill)
				}
				if opts.Stroke != nil {
					g.StrokePath(p, opts.StrokeColor, *opts.Stroke)
				}
			})
			got := frame2D(t, g, func() { g.DrawPath(cp); cp.Destroy() })
			if diff := imageDiff(want, got); diff != 0 {
				t.Fatalf("compiled path differs at %d pixels", diff)
			}
		})
	}
}

func TestGeometry2DFailedUploadAndFrameCleanup(t *testing.T) {
	for _, submit := range []bool{false, true} {
		t.Run(map[bool]string{false: "aborted", true: "submitted"}[submit], func(t *testing.T) {
			old := newHeadless(t, 32, 32)
			// Warm the renderer's reusable screenshot buffer before measuring
			// its allocation baseline; it outlives each Graphics context.
			frame2D(t, old, func() {})
			old.destroy()
			baseline := old.r.Device.Stats()
			g, err := newGraphics(old.r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(g.destroy)
			v, ix := geometryQuad(RGB(255, 0, 0))
			m, err := g.NewGeometry2D(v, ix)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := g.begin(Black); err != nil || !ok {
				t.Fatalf("begin: %v", err)
			}
			g.DrawGeometry(nil, m)
			previous := m.data
			create := vk.VkCreateBuffer
			func() {
				defer func() { vk.VkCreateBuffer = create }()
				vk.VkCreateBuffer = func(device vk.VkDevice, info *vk.VkBufferCreateInfo, allocator *vk.VkAllocationCallbacks, buffer *vk.VkBuffer) vk.VkResult {
					if info.Usage&vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT != 0 {
						return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
					}
					return create(device, info, allocator, buffer)
				}
				if err := m.Update(v, ix); err == nil {
					t.Fatal("index allocation failure was not returned")
				}
			}()
			if m.data != previous {
				t.Fatal("failed upload replaced queued geometry")
			}
			cp, err := g.CompilePath(new(Path).Circle(16, 16, 8), PathOptions{Fill: &FillOptions{}, Stroke: &StrokeOptions{Width: 2}})
			if err != nil {
				t.Fatal(err)
			}
			g.DrawPath(cp)
			if submit {
				if _, err := g.end(true); err != nil {
					t.Fatal(err)
				}
			}
			g.destroy()
			if m.data != nil || len(cp.parts) != 0 {
				t.Fatal("graphics retained owned geometry")
			}
			if got := g.r.Device.Stats(); got.Live != baseline.Live || got.Used != baseline.Used {
				t.Fatalf("GPU memory after cleanup %v; renderer baseline %v", got, baseline)
			}
		})
	}
}
