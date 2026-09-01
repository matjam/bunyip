// Package shaders holds the engine's GLSL sources and their SPIR-V builds.
// The .spv files are committed so that a checkout builds without a shader
// compiler; run go generate here after editing a source, and CI checks the
// committed output is current.
package shaders

import _ "embed"

//go:generate glslangValidator -V -o triangle.vert.spv triangle.vert
//go:generate glslangValidator -V -o triangle.frag.spv triangle.frag

var (
	//go:embed triangle.vert.spv
	TriangleVert []byte
	//go:embed triangle.frag.spv
	TriangleFrag []byte
)
