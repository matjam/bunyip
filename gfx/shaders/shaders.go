// Package shaders holds the gfx package's GLSL sources and committed SPIR-V,
// and the preludes that game shaders are compiled against.
package shaders

import (
	_ "embed"
	"fmt"
	"regexp"
)

// The 2D and mesh fragment programs, including the engine's own, are
// game-style shader bodies compiled against the preludes by bunyip-shader.

//go:generate glslangValidator -V -o sprite.vert.spv sprite.vert
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sprite.frag.spv sprite_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sdf.frag.spv sdf_default.glsl

var (
	//go:embed sprite.vert.spv
	SpriteVert []byte
	//go:embed sprite.frag.spv
	SpriteFrag []byte
	//go:embed sdf.frag.spv
	SDFFrag []byte
)

//go:generate glslangValidator -V -o mesh.vert.spv mesh.vert
//go:generate glslangValidator -V -o mesh.frag.spv mesh.frag

var (
	//go:embed mesh.vert.spv
	MeshVert []byte
	//go:embed mesh.frag.spv
	MeshFrag []byte
)

//go:generate glslangValidator -V -o shadow.vert.spv shadow.vert
//go:generate glslangValidator -V -o pbr.vert.spv pbr.vert
//go:generate go run ../../cmd/bunyip-shader -kind mesh -o pbr.frag.spv pbr_default.glsl
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

//go:generate glslangValidator -V -o fxaa.frag.spv fxaa.frag

//go:embed fxaa.frag.spv
var FXAAFrag []byte

//go:generate glslangValidator -V -o pbr_skin.vert.spv pbr_skin.vert
//go:generate glslangValidator -V -o shadow_skin.vert.spv shadow_skin.vert

var (
	//go:embed pbr_skin.vert.spv
	PBRSkinVert []byte
	//go:embed shadow_skin.vert.spv
	ShadowSkinVert []byte
)

//go:generate glslangValidator -V -o ssao.frag.spv ssao.frag
//go:generate glslangValidator -V -o aoblur.frag.spv aoblur.frag

var (
	//go:embed ssao.frag.spv
	SSAOFrag []byte
	//go:embed aoblur.frag.spv
	AOBlurFrag []byte
)

// Kind is which pipeline a game shader is written for.
type Kind string

const (
	// Sprite shaders colour 2D fragments: sprites, text, shapes.
	Sprite Kind = "sprite"
	// Mesh shaders adjust a Surface before the engine lights it.
	Mesh Kind = "mesh"
)

var (
	//go:embed prelude_sprite.glsl
	spritePrelude string
	//go:embed prelude_mesh.glsl
	meshPrelude string
)

const spritePostlude = `
void main() {
    outColor = fragment(vUV, vColor);
}
`

const meshPostlude = `
void main() {
    Surface s;
    vec4 albedoSample = texture(albedoTex, vUV) * vBaseColor;
    s.albedo = albedoSample.rgb;
    s.alpha = albedoSample.a;
    vec3 mr = texture(metalRoughTex, vUV).rgb;
    s.metallic = clamp(vMaterial.x * mr.b, 0.0, 1.0);
    s.roughness = clamp(vMaterial.y * mr.g, 0.04, 1.0);
    vec3 n = normalize(vNormal);
    if (!gl_FrontFacing) n = -n;
    if (vMaterial.w > 0.5) n = perturbNormal(n, vWorldPos, vUV);
    s.normal = n;
    s.uv = vUV;
    s.worldPos = vWorldPos;
    s.viewDir = normalize(frame.camPos.xyz - vWorldPos);
    s.emissive = texture(emissiveTex, vUV).rgb * vMaterial.z;
    surface(s);
    vec3 color = light(s) + s.emissive;
    outColor = finish(vec4(color, s.alpha), s);
}
`

const defaultFinish = `
vec4 finish(vec4 lit, Surface s) { return lit; }
`

var (
	fragmentDef = regexp.MustCompile(`\bvec4\s+fragment\s*\(`)
	surfaceDef  = regexp.MustCompile(`\bvoid\s+surface\s*\(`)
	finishDef   = regexp.MustCompile(`\bvec4\s+finish\s*\(`)
)

// Compose wraps a game's shader source with the prelude and main for its
// kind, filling in optional hooks the source leaves out. The result is a
// complete GLSL fragment shader for glslangValidator. Line numbers in
// compiler messages are offset by the prelude; Compose's second result is
// that offset.
func Compose(kind Kind, source string) (glsl string, lineOffset int, err error) {
	switch kind {
	case Sprite:
		if !fragmentDef.MatchString(source) {
			return "", 0, fmt.Errorf("a sprite shader must define vec4 fragment(vec2 uv, vec4 color)")
		}
		return spritePrelude + source + spritePostlude, countLines(spritePrelude), nil
	case Mesh:
		if !surfaceDef.MatchString(source) {
			return "", 0, fmt.Errorf("a mesh shader must define void surface(inout Surface s)")
		}
		glsl = meshPrelude + source
		if !finishDef.MatchString(source) {
			glsl += defaultFinish
		}
		return glsl + meshPostlude, countLines(meshPrelude), nil
	}
	return "", 0, fmt.Errorf("unknown shader kind %q (want sprite or mesh)", kind)
}

func countLines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
