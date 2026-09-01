// Package shaders holds the gfx package's GLSL sources and committed SPIR-V.
package shaders

import _ "embed"

//go:generate glslangValidator -V -o sprite.vert.spv sprite.vert
//go:generate glslangValidator -V -o sprite.frag.spv sprite.frag

var (
	//go:embed sprite.vert.spv
	SpriteVert []byte
	//go:embed sprite.frag.spv
	SpriteFrag []byte
)

//go:generate glslangValidator -V -o mesh.vert.spv mesh.vert
//go:generate glslangValidator -V -o mesh.frag.spv mesh.frag

var (
	//go:embed mesh.vert.spv
	MeshVert []byte
	//go:embed mesh.frag.spv
	MeshFrag []byte
)

//go:generate glslangValidator -V -o sdf.frag.spv sdf.frag

//go:embed sdf.frag.spv
var SDFFrag []byte
