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
