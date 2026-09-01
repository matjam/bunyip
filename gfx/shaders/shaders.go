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

//go:generate glslangValidator -V -o shadow.vert.spv shadow.vert
//go:generate glslangValidator -V -o pbr.vert.spv pbr.vert
//go:generate glslangValidator -V -o pbr.frag.spv pbr.frag
//go:generate glslangValidator -V -o post.vert.spv post.vert
//go:generate glslangValidator -V -o post.frag.spv post.frag
//go:generate glslangValidator -V -o bright.frag.spv bright.frag
//go:generate glslangValidator -V -o blur.frag.spv blur.frag

var (
	//go:embed shadow.vert.spv
	ShadowVert []byte
	//go:embed pbr.vert.spv
	PBRVert []byte
	//go:embed pbr.frag.spv
	PBRFrag []byte
	//go:embed post.vert.spv
	PostVert []byte
	//go:embed post.frag.spv
	PostFrag []byte
	//go:embed bright.frag.spv
	BrightFrag []byte
	//go:embed blur.frag.spv
	BlurFrag []byte
)

//go:generate glslangValidator -V -o shadow.frag.spv shadow.frag

//go:embed shadow.frag.spv
var ShadowFrag []byte
