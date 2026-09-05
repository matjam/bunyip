package gfx

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

func TestMorphDrawSnapshotsWeights(t *testing.T) {
	for _, animated := range []bool{false, true} {
		t.Run(map[bool]string{false: "model", true: "players"}[animated], func(t *testing.T) {
			g := newHeadless(t, 32, 32)
			m, err := g.LoadModel(morphGridDoc(1))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy()
			beginMorphRetireFrame(t, g)
			for _, weight := range []float32{0.25, 0.75} {
				if animated {
					p := m.NewAnimPlayer()
					p.SetMorphWeights(0, []float32{weight})
					g.DrawModelAnimated(m, Transform{}, p)
				} else {
					if err := m.SetMorphWeights(0, []float32{weight}); err != nil {
						t.Fatal(err)
					}
					g.DrawModel(m, lin.Identity())
				}
			}
			for i, want := range []float32{0.25, 0.75} {
				var in meshInstance
				g.cur.draws[i].morph.instance(&in)
				if in.morphW[0] != want {
					t.Errorf("draw %d weight = %v, want %v", i, in.morphW[0], want)
				}
			}
			endMorphRetireFrame(t, g)
		})
	}
}

func morphSnapshotDoc(skinned bool) *gltf.Document {
	doc := morphGridDoc(9)
	if skinned {
		p := &doc.Meshes[0].Primitives[0]
		p.Joints = make([][4]uint8, len(p.Positions))
		p.Weights = make([][4]float32, len(p.Positions))
		for i := range p.Weights {
			p.Weights[i][0] = 1
		}
		doc.Nodes[0].Skin, doc.Instances[0].Skin = 0, 0
		doc.Skins = []gltf.Skin{{Joints: []int{0}, InverseBind: []lin.Mat4{lin.Identity()}}}
	}
	return doc
}

// Compare shared-model draws with separately uploaded models, so expected
// geometry cannot be overwritten by another instance's CPU blend.
func TestMorphDrawSnapshotsRendered(t *testing.T) {
	few := func(target int, w float32) []float32 {
		weights := make([]float32, 9)
		weights[target] = w
		return weights
	}
	all := func(w float32) []float32 {
		weights := make([]float32, 9)
		for i := range weights {
			weights[i] = w
		}
		return weights
	}
	for _, skinned := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			weights [][]float32
		}{
			{"gpu", [][]float32{few(0, 0.25), few(8, 0.75)}},
			{"cpu", [][]float32{all(0.25), all(0.75)}},
			{"gpu to cpu", [][]float32{few(0, 0.25), all(0.75)}},
			{"cpu to gpu", [][]float32{all(0.25), few(8, 0.75)}},
			{"gpu cpu gpu", [][]float32{few(0, 0.25), all(0.75), few(8, 0.5)}},
		} {
			name := map[bool]string{false: "plain/", true: "skinned/"}[skinned] + tc.name
			t.Run(name, func(t *testing.T) {
				g := newHeadless(t, 192, 96)
				enableGeometryReadback(t)
				g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
				load := func() *Model {
					m, err := g.LoadModel(morphSnapshotDoc(skinned))
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(m.Destroy)
					return m
				}
				shared := load()
				models := make([]*Model, len(tc.weights))
				players := make([]*AnimPlayer, len(tc.weights))
				for i, w := range tc.weights {
					models[i] = load()
					if err := models[i].SetMorphWeights(0, w); err != nil {
						t.Fatal(err)
					}
					players[i] = shared.NewAnimPlayer()
					players[i].SetMorphWeights(0, w)
				}
				var snapshots []*Mesh
				render := func(sharedModel bool) *image.RGBA {
					return frames(t, g, func() {
						snapshots = snapshots[:0]
						g.SetCamera(Camera{Position: lin.V3(0, 3, 5), Target: lin.V3(0, 0.2, 0)})
						g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: Color{2, 2, 2, 1}})
						for i, w := range tc.weights {
							at := Transform{Position: lin.V3((float32(i)-float32(len(tc.weights)-1)/2)*2.1, 0, 0)}
							if sharedModel {
								if skinned {
									g.DrawModelAnimated(shared, at, players[i])
								} else {
									if err := shared.SetMorphWeights(0, w); err != nil {
										t.Fatal(err)
									}
									g.DrawModel(shared, at.Matrix())
								}
							} else if skinned {
								p := models[i].NewAnimPlayer()
								p.SetMorphWeights(0, w)
								g.DrawModelAnimated(models[i], at, p)
							} else {
								g.DrawModel(models[i], at.Matrix())
							}
							snapshots = append(snapshots, g.cur.draws[len(g.cur.draws)-1].mesh)
						}
					})
				}
				want := render(false)
				independent := append([]*Mesh(nil), snapshots...)
				got := render(true)
				if diff := imageDiff(want, got); diff != 0 {
					t.Errorf("shared morph instances differ from independent models in %d pixels", diff)
					morphImageDiagnostic(t, "independent", want)
					morphImageDiagnostic(t, "shared", got)
					for i, mesh := range independent {
						morphBufferDiagnostic(t, "independent", i, mesh)
					}
					for i, mesh := range snapshots {
						morphBufferDiagnostic(t, "shared", i, mesh)
					}
				}
			})
		}
	}
}

// Isolate frame-local geometry uploads from shared-model snapshots: each
// model has one pose and one draw. Uploading that pose while a frame is
// open must produce the same image as uploading it during setup.
func TestMorphUploadContext(t *testing.T) {
	for _, skinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "skinned"}[skinned], func(t *testing.T) {
			g := newHeadless(t, 96, 96)
			enableGeometryReadback(t)
			g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
			load := func() *Model {
				m, err := g.LoadModel(morphSnapshotDoc(skinned))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(m.Destroy)
				return m
			}
			before, during := load(), load()
			weights := make([]float32, 9)
			for i := range weights {
				weights[i] = 0.75
			}
			if err := before.SetMorphWeights(0, weights); err != nil {
				t.Fatal(err)
			}
			render := func(m *Model, update bool) *image.RGBA {
				return renderMaterial(t, g, func() {
					g.SetCamera(Camera{Position: lin.V3(0, 3, 5), Target: lin.V3(0, 0.2, 0)})
					g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: Color{2, 2, 2, 1}})
					if update {
						if err := m.SetMorphWeights(0, weights); err != nil {
							t.Fatal(err)
						}
					}
					if skinned {
						p := m.NewAnimPlayer()
						p.SetMorphWeights(0, weights)
						g.DrawModelAnimated(m, Transform{}, p)
					} else {
						g.DrawModel(m, lin.Identity())
					}
				})
			}
			want, got := render(before, false), render(during, true)
			if diff := imageDiff(want, got); diff != 0 {
				t.Errorf("frame upload differs from setup upload in %d pixels", diff)
				morphImageDiagnostic(t, "setup", want)
				morphImageDiagnostic(t, "frame", got)
				morphBufferDiagnostic(t, "setup", 0, before.morphs[0].mesh)
				morphBufferDiagnostic(t, "frame", 1, during.morphs[0].mesh)
			}
		})
	}
}

// Small failure images in the log let CI-only driver differences be
// inspected without leaving files in the checkout or changing assertions.
func morphImageDiagnostic(t *testing.T, label string, img *image.RGBA) {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Logf("%s PNG: %v", label, err)
		return
	}
	t.Logf("%s PNG base64: %s", label, base64.StdEncoding.EncodeToString(data.Bytes()))
}

func morphBufferDiagnostic(t *testing.T, label string, draw int, m *Mesh) {
	t.Helper()
	data := readGeometryBuffer(t, m.g, m.vbuf)
	stride := vertexSize
	if m.skinned {
		stride = skinVertexSize
	}
	mismatches := 0
	for i, v := range m.verts {
		for component, want := range []float32{v.Pos.X, v.Pos.Y, v.Pos.Z, v.Normal.X, v.Normal.Y, v.Normal.Z} {
			offset := i*stride + component*4
			got := math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
			if got != want {
				if mismatches == 0 {
					t.Logf("%s draw %d first vertex mismatch: vertex=%d component=%d got=%g want=%g", label, draw, i, component, got, want)
				}
				mismatches++
			}
		}
	}
	t.Logf("%s draw %d GPU vertex position/normal mismatches: %d of %d", label, draw, mismatches, len(m.verts)*6)
	data = readGeometryBuffer(t, m.g, m.ibuf)
	mismatches = 0
	for i, want := range m.indices {
		got := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		if got != want {
			if mismatches == 0 {
				t.Logf("%s draw %d first index mismatch: index=%d got=%d want=%d", label, draw, i, got, want)
			}
			mismatches++
		}
	}
	t.Logf("%s draw %d GPU index mismatches: %d of %d", label, draw, mismatches, len(m.indices))
}

func TestMorphDrawSnapshotsAllocateNothingOnGPU(t *testing.T) {
	g := newHeadless(t, 32, 32)
	m, err := g.LoadModel(morphGridDoc(1))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	beginMorphRetireFrame(t, g)
	weights := []float32{0.25}
	allocs := testing.AllocsPerRun(100, func() {
		g.cur.draws = g.cur.draws[:0]
		weights[0] = 1 - weights[0]
		if err := m.SetMorphWeights(0, weights); err != nil {
			t.Fatal(err)
		}
		g.DrawModel(m, lin.Identity())
	})
	if allocs != 0 {
		t.Errorf("steady GPU morph queue allocates %v times, want zero", allocs)
	}
	endMorphRetireFrame(t, g)
}
