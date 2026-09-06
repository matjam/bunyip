package gfx

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"testing"
)

func TestRuntimeShaderCompilationAndReload(t *testing.T) {
	g := newHeadless(t, 16, 16)
	ctx := context.Background()
	const source = `struct Params { tint: vec4f, }; @group(1) @binding(0) var<uniform> u: Params;
fn fragment(uv: vec2f, color: vec4f) -> vec4f { return u.tint; }`
	s, err := g.CompileShader(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUniforms(struct{ Tint Color }{RGB(255, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	tex, err := g.NewBlankTexture(1, 1, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s.SetImage(0, tex)
	check := func(want color.RGBA) {
		t.Helper()
		ok, err := g.begin(Black)
		if err != nil || !ok {
			t.Fatalf("begin: %v, %v", ok, err)
		}
		g.Shaded(s, func() { g.FillRect(0, 0, 16, 16, White) })
		img, err := g.end(true)
		if err != nil {
			t.Fatal(err)
		}
		if got := img.RGBAAt(8, 8); got != want {
			t.Fatalf("pixel = %v, want %v", got, want)
		}
	}
	check(color.RGBA{255, 0, 0, 255})
	before := bytes.Clone(s.block)
	if err := s.ReloadSource(ctx, `fn fragment(uv: vec2f, color: vec4f) -> vec4f { invalid; }`); err == nil {
		t.Fatal("invalid source accepted")
	}
	check(color.RGBA{255, 0, 0, 255})
	if err := s.ReloadSource(ctx, `struct Params { tint: vec4f, }; @group(1) @binding(0) var<uniform> u: Params;
fn fragment(uv: vec2f, color: vec4f) -> vec4f { return u.tint.grba; }`); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, s.block) || s.images[0] != tex {
		t.Fatal("reload lost uniforms or image binding")
	}
	check(color.RGBA{0, 255, 0, 255})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.ReloadSource(canceled, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("reload cancellation: %v", err)
	}
	check(color.RGBA{0, 255, 0, 255})
	mesh, err := g.CompileMeshShader(ctx, `fn surface(input: Surface) -> Surface { var s = input; s.unlit = true; return s; }
fn vertex(input: VertexData) -> VertexData { var v = input; v.position.x += 0.01; return v; }`)
	if err != nil {
		t.Fatal(err)
	}
	if !mesh.mesh || len(mesh.stages) != 4 || !mesh.orderIndependent() {
		t.Fatal("runtime mesh omitted required variants")
	}
	if err := mesh.ReloadSource(ctx, `fn surface(s: Surface) -> Surface { return s; }`); err != nil {
		t.Fatal(err)
	}
	if len(mesh.stages) != 0 || !mesh.orderIndependent() {
		t.Fatal("mesh reload retained obsolete vertex variants")
	}
}
