package shaders

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"text/scanner"

	"github.com/gogpu/naga"
)

// Compiler translates WGSL to SPIR-V in Go, without external tools or cgo.
// Its zero value is ready to use and may be used concurrently.
// Compilation is synchronous. Context cancellation is checked between compiler
// phases; it cannot interrupt a phase already executing.
type Compiler struct{}

// Compile composes game source with Bunyip's bindings and entry points.
// Kind defaults to Sprite. Sprite returns fragment SPIR-V; Mesh returns a
// bundle containing both fragment variants and, when source defines vertex,
// all four vertex variants. Returned bytes can be stored and loaded later.
func (c Compiler) Compile(ctx context.Context, kind Kind, source string) ([]byte, error) {
	if kind == "" {
		kind = Sprite
	}
	stages := []Stage{StageFrag}
	if kind == Mesh {
		stages = append(stages, StageOITFrag)
		if HasVertexHook(source) {
			stages = append(stages, StageVert, StageSkinVert, StageShadowVert, StageShadowSkinVert)
		}
	}
	built := make(map[Stage][]byte, len(stages))
	for _, stage := range stages {
		data, err := c.CompileStage(ctx, kind, stage, source)
		if err != nil {
			return nil, err
		}
		built[stage] = data
	}
	if kind == Sprite {
		return built[StageFrag], nil
	}
	return Bundle(built), nil
}

// CompileStage composes and compiles one Bunyip shader stage. Use Compile
// for complete game shaders. Diagnostics refer to the composed source;
// errors include the prefix line count for locating the original body.
func (c Compiler) CompileStage(ctx context.Context, kind Kind, stage Stage, source string) ([]byte, error) {
	if kind == "" {
		kind = Sprite
	}
	body, offset, err := Compose(kind, stage, source)
	if err != nil {
		return nil, fmt.Errorf("compose %s %s: %w", kind, stage, err)
	}
	data, err := c.CompileRaw(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("compile %s %s (source follows %d prefix lines): %w", kind, stage, offset, err)
	}
	return data, nil
}

// CompileRaw compiles a complete WGSL module with explicit entry points and
// bindings. It does not add Bunyip helpers. Output uses SPIR-V 1.3 and Vulkan
// coordinates without an automatic Y flip. The caller must match the renderer's
// pipeline and descriptor interface. This method creates no GPU resources.
// Module-level private initializers are currently rejected because Naga can
// drop them; use constants or assign in the entry point or hook instead.
func (Compiler) CompileRaw(ctx context.Context, source string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ast, err := naga.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse WGSL: %w", err)
	}
	if name := initializedPrivateGlobal(source); name != "" {
		return nil, fmt.Errorf("private global %q has an initializer unsupported by the current WGSL compiler; assign its value in an entry point or hook instead", name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := naga.Lower(ast)
	if err != nil {
		return nil, fmt.Errorf("lower WGSL: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	problems, err := naga.Validate(module)
	if err != nil {
		return nil, fmt.Errorf("validate WGSL: %w", err)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("validate WGSL: %v", problems)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := compileModule(module)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateCompiledSPIRV(data); err != nil {
		return nil, err
	}
	return data, nil
}

// initializedPrivateGlobal checks declaration tokens before lowering: naga
// v0.19 can discard a constructor initializer, leaving no trace in the IR.
// The public parsed AST is opaque. Parsing has already succeeded, so this
// scanner only distinguishes module declarations from locals and locates an
// initializer outside the declaration's type parameters.
func initializedPrivateGlobal(source string) string {
	var tokens scanner.Scanner
	tokens.Init(strings.NewReader(withoutComments(source)))
	tokens.Mode = scanner.ScanIdents
	depth := 0
	for token := tokens.Scan(); token != scanner.EOF; token = tokens.Scan() {
		switch token {
		case '{':
			depth++
		case '}':
			depth--
		case scanner.Ident:
			if depth != 0 || tokens.TokenText() != "var" {
				continue
			}
			private := true // A module var without an address space is private.
			token = tokens.Scan()
			if token == '<' {
				tokens.Scan()
				private = tokens.TokenText() == "private"
				for token = tokens.Scan(); token != '>' && token != scanner.EOF; token = tokens.Scan() {
				}
				token = tokens.Scan()
			}
			name := tokens.TokenText()
			angles, parentheses, brackets := 0, 0, 0
			for token = tokens.Scan(); token != ';' && token != scanner.EOF; token = tokens.Scan() {
				switch token {
				case '(':
					parentheses++
				case ')':
					parentheses--
				case '[':
					brackets++
				case ']':
					brackets--
				case '<':
					if parentheses == 0 && brackets == 0 {
						angles++
					}
				case '>':
					if parentheses == 0 && brackets == 0 {
						angles--
					}
				case '=':
					if private && angles == 0 && parentheses == 0 && brackets == 0 {
						return name
					}
				}
			}
		}
	}
	return ""
}

// validateCompiledSPIRV checks instruction framing, not semantic validity.
func validateCompiledSPIRV(data []byte) error {
	if len(data) < 20 || len(data)%4 != 0 {
		return fmt.Errorf("invalid SPIR-V length")
	}
	word := func(i int) uint32 { return binary.LittleEndian.Uint32(data[i*4:]) }
	if word(0) != 0x07230203 || word(1) == 0 || word(3) == 0 || word(4) != 0 {
		return fmt.Errorf("invalid SPIR-V header")
	}
	for i := 5; i < len(data)/4; {
		n := int(word(i) >> 16)
		if n == 0 || n > len(data)/4-i {
			return fmt.Errorf("invalid SPIR-V instruction at word %d", i)
		}
		i += n
	}
	return nil
}
