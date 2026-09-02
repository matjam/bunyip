// Command bunyip-shader compiles a game's shader to SPIR-V. The source
// holds only the part the game writes: for a sprite shader
//
//	vec4 fragment(vec2 uv, vec4 color)
//
// and for a mesh shader
//
//	void surface(inout Surface s)
//	vec4 finish(vec4 lit, Surface s)   // optional
//
// The engine's prelude (textures, uniforms, helpers) and main are added
// around it, and glslangValidator, which must be on the PATH, produces
// the SPIR-V. A typical use is a go:generate line beside the source:
//
//	//go:generate go run github.com/matjam/bunyip/cmd/bunyip-shader -o wave.spv wave.glsl
//	//go:embed wave.spv
//	var waveSPV []byte
//
// -kind selects sprite (the default) or mesh; -print writes the composed
// GLSL to standard output instead of compiling it, for reading compiler
// messages against.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matjam/bunyip/gfx/shaders"
)

func main() {
	kind := flag.String("kind", "sprite", "shader kind: sprite or mesh")
	out := flag.String("o", "", "output SPIR-V file; default is the source name with .spv")
	print := flag.Bool("print", false, "write the composed GLSL to stdout instead of compiling")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: bunyip-shader [-kind sprite|mesh] [-o out.spv] [-print] source.glsl")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	src := flag.Arg(0)
	if err := run(shaders.Kind(*kind), src, *out, *print); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-shader:", err)
		os.Exit(1)
	}
}

func run(kind shaders.Kind, src, out string, print bool) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	glsl, offset, err := shaders.Compose(kind, string(body))
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	if print {
		_, err := os.Stdout.WriteString(glsl)
		return err
	}
	if out == "" {
		out = strings.TrimSuffix(src, filepath.Ext(src)) + ".spv"
	}
	if _, err := exec.LookPath("glslangValidator"); err != nil {
		return fmt.Errorf("glslangValidator is not on the PATH; install the Vulkan SDK or the glslang package")
	}
	// glslang classifies the stage by file suffix, so the composed source
	// goes through a temporary .frag file.
	tmp, err := os.CreateTemp("", "bunyip-shader-*.frag")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(glsl); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	cmd := exec.Command("glslangValidator", "-V", "-o", out, tmp.Name())
	var stderr, stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// glslang reports line numbers of the composed source; shift them
		// back to the game's file.
		msg := stdout.String() + stderr.String()
		return fmt.Errorf("compiling %s (lines in messages are %d greater than in the file):\n%s", src, offset, strings.TrimSpace(msg))
	}
	return nil
}
