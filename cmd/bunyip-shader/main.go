// Command bunyip-shader compiles WGSL to SPIR-V using the pure-Go compiler.
// Game sources define Bunyip's fragment or surface hooks. -raw compiles a
// complete WGSL module with its own bindings and entry points.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matjam/bunyip/gfx/shaders"
)

func main() {
	kind := flag.String("kind", "sprite", "shader kind: sprite or mesh")
	stage := flag.String("stage", "", "compile one stage: frag, oitfrag, vert, skinvert, shadowvert, shadowskinvert")
	out := flag.String("o", "", "output file; default replaces .wgsl with .spv")
	printSource := flag.Bool("print", false, "print composed WGSL instead of compiling")
	raw := flag.Bool("raw", false, "compile complete WGSL without Bunyip composition")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: bunyip-shader [-kind sprite|mesh] [-stage name] [-raw] [-print] [-o output.spv] source.wgsl")
		os.Exit(2)
	}
	if err := run(context.Background(), shaders.Kind(*kind), *stage, flag.Arg(0), *out, *printSource, *raw); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-shader:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, kind shaders.Kind, stageName, src, out string, printSource, raw bool) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if out == "" {
		out = strings.TrimSuffix(src, filepath.Ext(src)) + ".spv"
	}
	var stage shaders.Stage
	if stageName != "" {
		var ok bool
		stage, ok = shaders.ParseStage(stageName)
		if !ok {
			return fmt.Errorf("unknown stage %q", stageName)
		}
	}
	if raw && stageName != "" {
		return fmt.Errorf("-raw and -stage cannot be combined")
	}
	if printSource {
		text := string(body)
		if !raw {
			text, _, err = shaders.Compose(kind, stage, text)
			if err != nil {
				return err
			}
		}
		_, err = os.Stdout.WriteString(text)
		return err
	}
	compiler := shaders.Compiler{}
	var data []byte
	switch {
	case raw:
		data, err = compiler.CompileRaw(ctx, string(body))
	case stageName != "":
		data, err = compiler.CompileStage(ctx, kind, stage, string(body))
	default:
		data, err = compiler.Compile(ctx, kind, string(body))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	return os.WriteFile(out, data, 0644)
}
