// Command bunyip-shader compiles a game's shader to SPIR-V. The source
// holds only the part the game writes: for a sprite shader
//
//	vec4 fragment(vec2 uv, vec4 color)
//
// and for a mesh shader
//
//	void surface(inout Surface s)
//	vec4 finish(vec4 lit, Surface s)     // optional: after lighting
//	void vertex(inout VertexData v)      // optional: move vertices first
//
// The engine's prelude (textures, uniforms, helpers) and main are added
// around it, and glslangValidator, which must be on the PATH, produces
// the SPIR-V. A typical use is a go:generate line beside the source:
//
//	//go:generate go run github.com/matjam/bunyip/cmd/bunyip-shader -o wave.spv wave.glsl
//	//go:embed wave.spv
//	var waveSPV []byte
//
// -kind selects sprite (the default) or mesh. A mesh shader with a
// vertex() hook is written as a bundle holding its fragment program and
// the four vertex programs (static and skinned, lit and shadow), which
// NewMeshShader reads; without the hook the output is plain fragment
// SPIR-V. -stage compiles just one program (frag, vert, skinvert,
// shadowvert, shadowskinvert), which is how the engine builds its own.
// -print writes the composed GLSL of a stage to standard output instead
// of compiling it, for reading compiler messages against.
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
	stage := flag.String("stage", "", "compile one stage only: frag, vert, skinvert, shadowvert, shadowskinvert")
	out := flag.String("o", "", "output file; default is the source name with .spv")
	print := flag.Bool("print", false, "write the composed GLSL (of -stage, default frag) to stdout instead of compiling")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: bunyip-shader [-kind sprite|mesh] [-stage name] [-o out.spv] [-print] source.glsl")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(shaders.Kind(*kind), *stage, flag.Arg(0), *out, *print); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-shader:", err)
		os.Exit(1)
	}
}

func run(kind shaders.Kind, stageName, src, out string, print bool) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	source := string(body)
	if out == "" {
		out = strings.TrimSuffix(src, filepath.Ext(src)) + ".spv"
	}
	var only *shaders.Stage
	if stageName != "" {
		st, ok := shaders.ParseStage(stageName)
		if !ok {
			return fmt.Errorf("unknown stage %q", stageName)
		}
		only = &st
	}
	if print {
		st := shaders.StageFrag
		if only != nil {
			st = *only
		}
		glsl, _, err := shaders.Compose(kind, st, source)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		_, err = os.Stdout.WriteString(glsl)
		return err
	}
	if _, err := exec.LookPath("glslangValidator"); err != nil {
		return fmt.Errorf("glslangValidator is not on the PATH; install the Vulkan SDK or the glslang package")
	}
	// Which stages to build: one when asked; otherwise the fragment
	// program, plus the vertex programs when a mesh shader hooks vertices.
	var stages []shaders.Stage
	switch {
	case only != nil:
		stages = []shaders.Stage{*only}
	case kind == shaders.Mesh && shaders.HasVertexHook(source):
		stages = []shaders.Stage{shaders.StageFrag, shaders.StageVert, shaders.StageSkinVert, shaders.StageShadowVert, shaders.StageShadowSkinVert}
	default:
		stages = []shaders.Stage{shaders.StageFrag}
	}
	built := map[shaders.Stage][]byte{}
	for _, st := range stages {
		glsl, offset, err := shaders.Compose(kind, st, source)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		spv, err := compile(glsl, st)
		if err != nil {
			return fmt.Errorf("compiling %s stage of %s (lines in messages are %d greater than in the file):\n%s", st, src, offset, err)
		}
		built[st] = spv
	}
	var data []byte
	if len(built) == 1 {
		for _, spv := range built {
			data = spv
		}
	} else {
		data = shaders.Bundle(built)
	}
	return os.WriteFile(out, data, 0o644)
}

// compile runs glslangValidator over composed GLSL for a stage. glslang
// classifies the stage by file suffix, so the source goes through a
// temporary file.
func compile(glsl string, st shaders.Stage) ([]byte, error) {
	suffix := ".frag"
	if st != shaders.StageFrag {
		suffix = ".vert"
	}
	tmp, err := os.CreateTemp("", "bunyip-shader-*"+suffix)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(glsl); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()
	outFile := tmp.Name() + ".spv"
	defer os.Remove(outFile)
	cmd := exec.Command("glslangValidator", "-V", "-o", outFile, tmp.Name())
	var stderr, stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(stdout.String()+stderr.String()))
	}
	return os.ReadFile(outFile)
}
