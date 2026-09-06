package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/gfx/shaders"
)

func TestCompileAndPreserveOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "color.wgsl")
	out := filepath.Join(dir, "color.spv")
	write := func(source string) {
		t.Helper()
		if err := os.WriteFile(src, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("fn fragment(uv: vec2f, color: vec4f) -> vec4f { return color; }")
	if err := run(context.Background(), shaders.Sprite, "", src, "", false, false); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 20 {
		t.Fatal("missing SPIR-V output")
	}
	write("fn fragment(")
	if err := run(context.Background(), shaders.Sprite, "", src, "", false, false); err == nil {
		t.Fatal("invalid source accepted")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("failed compilation changed output")
	}
	write("@fragment fn main() -> @location(0) vec4f { return vec4f(1.0); }")
	if err := run(context.Background(), shaders.Sprite, "frag", src, out, false, true); err == nil {
		t.Fatal("raw stage combination accepted")
	}
	if err := run(context.Background(), shaders.Sprite, "", src, out, false, true); err != nil {
		t.Fatal(err)
	}
}
