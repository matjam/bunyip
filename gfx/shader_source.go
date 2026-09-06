package gfx

import (
	"context"

	"github.com/matjam/bunyip/gfx/shaders"
)

// CompileShader compiles Bunyip sprite WGSL source and creates an owned GPU
// shader. The source defines fn fragment(uv: vec2f, color: vec4f) -> vec4f;
// the engine supplies bindings and the entry point. Compilation uses Go only.
// This synchronous method belongs on the game goroutine, like NewShader.
// Cancellation is checked between compilation phases, not during a phase or
// GPU creation. To compile on a worker, use
// shaders.Compiler.Compile, then call NewShader on the game goroutine.
func (g *Graphics) CompileShader(ctx context.Context, source string) (*Shader, error) {
	return g.compileShader(ctx, shaders.Sprite, source)
}

// CompileMeshShader compiles Bunyip mesh WGSL source and creates an owned GPU
// shader. The source defines fn surface(s: Surface) -> Surface and may define
// finish and vertex hooks. All required lit, transparency and vertex variants
// are compiled together. Compiler requirements and threading are the same as
// CompileShader. See the shader guide for hook signatures and engine bindings.
func (g *Graphics) CompileMeshShader(ctx context.Context, source string) (*Shader, error) {
	return g.compileShader(ctx, shaders.Mesh, source)
}

func (g *Graphics) compileShader(ctx context.Context, kind shaders.Kind, source string) (*Shader, error) {
	data, err := (shaders.Compiler{}).Compile(ctx, kind, source)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return g.newShader(data, kind == shaders.Mesh)
}

// ReloadSource compiles WGSL for this shader's existing kind, then replaces
// its GPU programs. Compilation or pipeline creation errors preserve the old
// shader. Uniforms and images are retained. Call on the game goroutine; this
// method compiles in Go and blocks until compilation finishes.
// For background compilation, compile through
// shaders.Compiler and call Reload with the resulting bytes on the game
// goroutine. Cancellation is checked between compilation phases, not during
// a phase or GPU pipeline creation.
func (s *Shader) ReloadSource(ctx context.Context, source string) error {
	kind := shaders.Sprite
	if s.mesh {
		kind = shaders.Mesh
	}
	data, err := (shaders.Compiler{}).Compile(ctx, kind, source)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Reload(data)
}
