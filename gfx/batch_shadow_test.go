package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestStaticBatchShadowsOffscreen puts a static batch prop outside the
// camera's view but inside a light's shadow volume. A prop queued with
// DrawMesh casts its shadow there, and so must the same prop in a batch,
// whose hierarchy walk rejects it for the camera.
func TestStaticBatchShadowsOffscreen(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	// The camera looks down at the floor around the origin; the prop hangs
	// at (0, 4, 0), above the top of the view.
	cam := Camera{Position: lin.V3(0, 1, 5), Target: lin.V3(0, 0, 0)}
	prop := lin.Translate(lin.V3(0, 4, 0))
	floor := lin.Translate(lin.V3(0, -0.1, 0)).Mul(lin.Scale(lin.V3(20, 0.2, 20)))
	mat := Material{BaseColor: White, Roughness: 1}
	if cam.Frustum(1).ContainsSphere(cube.boundingSphere(prop)) {
		t.Fatal("the prop is inside the camera's view; the test needs it outside")
	}
	batch := g.NewStaticBatch([]BatchItem{{Mesh: cube, Material: mat, Model: prop}})

	lights := []struct {
		name  string
		light func()
	}{
		{"spot", func() {
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Black, Ambient: Color{0.02, 0.02, 0.02, 1}})
			g.AddSpot(SpotLight{Position: lin.V3(0, 8, 0), Direction: lin.V3(0, -1, 0), Color: Color{8, 8, 8, 1},
				Range: 20, OuterAngle: lin.Radians(70), Shadows: true})
		}},
		{"point", func() {
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Black, Ambient: Color{0.02, 0.02, 0.02, 1}})
			g.AddPoint(PointLight{Position: lin.V3(0, 8, 0), Color: Color{8, 8, 8, 1}, Range: 20, Shadows: true})
		}},
		{"cascade", func() {
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: White, Ambient: Color{0.02, 0.02, 0.02, 1},
				Shadows: true, ShadowDistance: 20})
		}},
	}
	for _, l := range lights {
		t.Run(l.name, func(t *testing.T) {
			render := func(drawProp func()) (centre uint8, shadowDraws int) {
				img := frames(t, g, func() {
					g.SetCamera(cam)
					l.light()
					g.DrawMesh(cube, mat, floor)
					if drawProp != nil {
						drawProp()
					}
				})
				return img.RGBAAt(32, 32).R, g.Stats().ShadowDraws
			}
			open, openDraws := render(nil)
			mesh, meshDraws := render(func() { g.DrawMesh(cube, mat, prop) })
			batched, batchDraws := render(func() { g.DrawBatch(batch) })
			if int(mesh) > int(open)-20 {
				t.Fatalf("the prop drawn with DrawMesh leaves the floor at %d, open floor is %d: the scene does not show its shadow", mesh, open)
			}
			if batchDraws != meshDraws {
				t.Errorf("the batch records %d shadow instances, DrawMesh records %d (the floor alone records %d)", batchDraws, meshDraws, openDraws)
			}
			if d := int(batched) - int(mesh); d < -2 || d > 2 {
				t.Errorf("the floor under the batched prop is %d, under the DrawMesh prop %d, open floor %d", batched, mesh, open)
			}
		})
	}
}
