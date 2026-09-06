package gfx

import (
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// materialTone predicts the linear pixel after the default ACES tone map.
func materialTone(x float32) float32 {
	return min(max(x*(2.51*x+0.03)/(x*(2.43*x+0.59)+0.14), 0), 1)
}

// TestBlendedMaterialFadesWithAlpha checks absolute brightness, since two
// transparency paths can agree while both ignore the material's alpha.
func TestBlendedMaterialFadesWithAlpha(t *testing.T) {
	g := newHeadless(t, 64, 64)
	finish, err := g.CompileMeshShader(context.Background(), `
fn surface(s: Surface) -> Surface { return s; }
fn finish(lit: vec4f, s: Surface) -> vec4f { return vec4f(lit.rgb, lit.a * 0.5); }
`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(finish.Destroy)
	for _, path := range []struct {
		name string
		oit  bool
	}{{"sorted", false}, {"order-independent", true}} {
		t.Run(path.name, func(t *testing.T) {
			if path.oit && !g.r.Device.IndependentBlend() {
				t.Skip("device does not support order-independent transparency")
			}
			for _, tc := range []struct {
				name        string
				alpha       float32
				vertexAlpha float32
				texture     bool
				texAlpha    uint8
				emissive    float32
				finish      bool
				fog         bool
				opaque      bool
				cutoff      float32
			}{
				{name: "base", alpha: 0.25, vertexAlpha: 1},
				{name: "zero", alpha: 0, vertexAlpha: 1},
				{name: "one", alpha: 1, vertexAlpha: 1},
				{name: "vertex", alpha: 1, vertexAlpha: 64.0 / 255},
				{name: "texture", alpha: 1, vertexAlpha: 1, texture: true, texAlpha: 64},
				{name: "combined", alpha: 0.5, vertexAlpha: 128.0 / 255, texture: true, texAlpha: 128},
				{name: "transparent-texel", alpha: 1, vertexAlpha: 1, texture: true},
				{name: "emissive", alpha: 0.25, vertexAlpha: 1, emissive: 0.5},
				{name: "finish", alpha: 0.5, vertexAlpha: 1, finish: true},
				{name: "fog", alpha: 0.25, vertexAlpha: 1, fog: true},
				{name: "opaque", alpha: 0.25, vertexAlpha: 1, opaque: true},
				{name: "cutout", alpha: 1, vertexAlpha: 1, texture: true, texAlpha: 128, opaque: true, cutoff: 0.25},
			} {
				t.Run(tc.name, func(t *testing.T) {
					quad := facingQuad(t, g)
					verts := append([]Vertex(nil), quad.Vertices()...)
					for i := range verts {
						verts[i].Color = Color{1, 1, 1, tc.vertexAlpha}
					}
					if err := quad.Update(verts, quad.Indices()); err != nil {
						t.Fatal(err)
					}
					base := Color{0.8, 0.4, 0.2, tc.alpha}
					mat := Material{BaseColor: base, Unlit: true, Blend: !tc.opaque, Emissive: tc.emissive, AlphaCutoff: tc.cutoff}
					want := [3]float32{base.R, base.G, base.B}
					alpha := tc.alpha * tc.vertexAlpha
					if tc.texture {
						texel := color.NRGBA{200, 140, 90, tc.texAlpha}
						img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
						img.SetNRGBA(0, 0, texel)
						mat.Texture, err = g.NewTexture(img, TextureOptions{})
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(mat.Texture.Destroy)
						for i, c := range [3]uint8{texel.R, texel.G, texel.B} {
							want[i] *= srgbToLinear(c)
						}
						alpha *= float32(tc.texAlpha) / 255
					}
					if tc.finish {
						mat.Shader = finish
						alpha *= 0.5
					}
					if tc.opaque {
						alpha = 1
					}
					for i := range want {
						want[i] *= 1 + tc.emissive
					}
					fog := Color{0.2, 0.4, 0.8, 1}
					if tc.fog {
						want = [3]float32{fog.R, fog.G, fog.B}
					}
					img := renderMaterial(t, g, func() {
						p := g.Post()
						p.OrderIndependent = path.oit
						g.SetPost(p)
						if tc.fog {
							g.SetLight(Light{Fog: Fog{Color: fog, End: 1}})
						}
						g.DrawMesh(quad, mat, lin.Identity())
					})
					if path.oit && !tc.opaque && g.main.visOIT != 1 {
						t.Fatalf("expected one order-independent draw, got %d", g.main.visOIT)
					}
					px := img.RGBAAt(32, 32)
					for i, c := range [3]uint8{px.R, px.G, px.B} {
						got, expected := srgbToLinear(c), materialTone(want[i]*alpha)
						if d := got - expected; d > 0.025 || d < -0.025 {
							t.Errorf("channel %d = %.3f, want %.3f (linear source %.3f, alpha %.3f)", i, got, expected, want[i], alpha)
						}
					}
				})
			}
		})
	}
}
