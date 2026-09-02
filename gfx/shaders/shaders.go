// Package shaders holds the gfx package's GLSL sources and committed SPIR-V,
// and the preludes that game shaders are compiled against.
package shaders

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"regexp"
)

// The 2D and mesh programs, including the engine's own, are game-style
// shader bodies compiled against the preludes by bunyip-shader.

//go:generate glslangValidator -V -o sprite.vert.spv sprite.vert
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sprite.frag.spv sprite_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sdf.frag.spv sdf_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o colormatrix.frag.spv colormatrix_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o lit.frag.spv lit_default.glsl

var (
	//go:embed sprite.vert.spv
	SpriteVert []byte
	//go:embed sprite.frag.spv
	SpriteFrag []byte
	//go:embed colormatrix.frag.spv
	MatrixFrag []byte
	//go:embed lit.frag.spv
	LitFrag []byte
	//go:embed sdf.frag.spv
	SDFFrag []byte
)

//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage frag -o pbr.frag.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage vert -o pbr.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage skinvert -o pbr_skin.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowvert -o shadow.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowskinvert -o shadow_skin.vert.spv pbr_default.glsl
//go:generate glslangValidator -V -o shadow.frag.spv shadow.frag
//go:generate glslangValidator -V -o post.vert.spv post.vert
//go:generate glslangValidator -V -o post.frag.spv post.frag
//go:generate glslangValidator -V -o bright.frag.spv bright.frag
//go:generate glslangValidator -V -o blur.frag.spv blur.frag
//go:generate glslangValidator -V -o fxaa.frag.spv fxaa.frag
//go:generate glslangValidator -V -o ssao.frag.spv ssao.frag
//go:generate glslangValidator -V -o aoblur.frag.spv aoblur.frag
//go:generate glslangValidator -V -o sky.frag.spv sky.frag
//go:generate glslangValidator -V -o skyparam.frag.spv skyparam.frag
//go:generate glslangValidator -V -o line.vert.spv line.vert
//go:generate glslangValidator -V -o line.frag.spv line.frag
//go:generate glslangValidator -V -o outline.vert.spv outline.vert
//go:generate glslangValidator -V -o solid.frag.spv solid.frag
//go:generate glslangValidator -V -o decal.vert.spv decal.vert
//go:generate glslangValidator -V -o decal.frag.spv decal.frag

var (
	//go:embed sky.frag.spv
	SkyFrag []byte
	//go:embed skyparam.frag.spv
	SkyParamFrag []byte
	//go:embed line.vert.spv
	LineVert []byte
	//go:embed line.frag.spv
	LineFrag []byte
	//go:embed outline.vert.spv
	OutlineVert []byte
	//go:embed solid.frag.spv
	SolidFrag []byte
	//go:embed decal.vert.spv
	DecalVert []byte
	//go:embed decal.frag.spv
	DecalFrag []byte
	//go:embed pbr.frag.spv
	PBRFrag []byte
	//go:embed pbr.vert.spv
	PBRVert []byte
	//go:embed pbr_skin.vert.spv
	PBRSkinVert []byte
	//go:embed shadow.vert.spv
	ShadowVert []byte
	//go:embed shadow_skin.vert.spv
	ShadowSkinVert []byte
	//go:embed shadow.frag.spv
	ShadowFrag []byte
	//go:embed post.vert.spv
	PostVert []byte
	//go:embed post.frag.spv
	PostFrag []byte
	//go:embed bright.frag.spv
	BrightFrag []byte
	//go:embed blur.frag.spv
	BlurFrag []byte
	//go:embed fxaa.frag.spv
	FXAAFrag []byte
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
	// Mesh shaders adjust a Surface before the engine lights it, and may
	// move vertices first.
	Mesh Kind = "mesh"
)

// Stage is one program of a mesh shader.
type Stage uint32

const (
	StageFrag           Stage = iota // the lit fragment program
	StageVert                        // static meshes, lit pass
	StageSkinVert                    // skinned meshes, lit pass
	StageShadowVert                  // static meshes, shadow pass
	StageShadowSkinVert              // skinned meshes, shadow pass
	stageCount
)

// String names a stage as bunyip-shader's -stage flag spells it.
func (s Stage) String() string {
	names := [...]string{"frag", "vert", "skinvert", "shadowvert", "shadowskinvert"}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("Stage(%d)", int(s))
}

// ParseStage reads a -stage flag value.
func ParseStage(s string) (Stage, bool) {
	for st := Stage(0); st < stageCount; st++ {
		if st.String() == s {
			return st, true
		}
	}
	return 0, false
}

var (
	//go:embed prelude_sprite.glsl
	spritePrelude string
	//go:embed prelude_mesh.glsl
	meshPrelude string
	//go:embed vert_common.glsl
	vertCommon string
	//go:embed vert_skin.glsl
	vertSkin string
)

const spritePostlude = `
void main() {
    outColor = fragment(vUV, vColor);
}
`

const meshPostlude = `
void main() {
    Surface s;
    vec2 uv = uvTransform(vUV);
    vec4 albedoSample = texture(albedoTex, uv) * vBaseColor * vColor;
    s.albedo = albedoSample.rgb;
    s.alpha = albedoSample.a;
    vec3 mr = texture(metalRoughTex, uv).rgb;
    s.metallic = clamp(vMaterial.x * mr.b, 0.0, 1.0);
    s.roughness = clamp(vMaterial.y * mr.g, 0.04, 1.0);
    vec3 n = normalize(vNormal);
    if (!gl_FrontFacing) n = -n;
    float flags = vMaterial.w;
    if (mod(flags, 2.0) >= 1.0) n = perturbNormal(n, vWorldPos, uv);
    s.normal = n;
    s.uv = uv;
    s.uv2 = vUV2;
    s.color = vColor;
    s.worldPos = vWorldPos;
    s.viewDir = normalize(frame.camPos.xyz - vWorldPos);
    vec3 glow = mod(flags, 16.0) >= 8.0 ? albedoSample.rgb : texture(emissiveTex, uv).rgb;
    s.emissive = glow * vMaterial.z;
    vec2 aoUV = mod(flags, 8.0) >= 4.0 ? vUV2 : uv;
    s.occlusion = mix(1.0, texture(occlusionTex, aoUV).r, vExtra.z);
    s.unlit = mod(flags, 4.0) >= 2.0;
    s.clearcoat = vUVT1.z;
    s.clearcoatRoughness = vUVT1.w;
    s.sheen = vSheen.rgb;
    s.sheenRoughness = vSheen.w;
    s.subsurface = vExtra.w;
    s.thickness = texture(thicknessTex, uv).r;
    s.transmission = vVolume.x * texture(transmissionTex, uv).r;
    s.ior = vVolume.y;
    s.volume = vVolume.z * s.thickness;
    s.attenuation = vAtten.rgb;
    s.attenuationDistance = vVolume.w;
    surface(s);
    if (vExtra.y > 0.0 && s.alpha < vExtra.y) discard;
    vec3 color = s.unlit ? s.albedo : light(s);
    vec4 lit = finish(vec4(color + s.emissive, s.alpha), s);
    lit.rgb = applyFog(lit.rgb, vWorldPos, vViewDepth);
    outColor = lit;
}
`

const defaultFinish = `
vec4 finish(vec4 lit, Surface s) { return lit; }
`

const defaultVertex = `
void vertex(inout VertexData v) {}
`

// Lit-pass vertex main, static and skinned.
const litVertPostlude = `
layout(location = 0) out vec3 vWorldPos;
layout(location = 1) out vec3 vNormal;
layout(location = 2) out vec2 vUV;
layout(location = 3) out float vViewDepth;
layout(location = 4) flat out vec4 vBaseColor;
layout(location = 5) flat out vec4 vMaterial;
layout(location = 6) flat out vec4 vExtra;
layout(location = 7) out vec2 vUV2;
layout(location = 8) out vec4 vColor;
layout(location = 9) flat out vec4 vUVT0;
layout(location = 10) flat out vec4 vUVT1;
layout(location = 11) flat out vec4 vSheen;
layout(location = 12) flat out vec4 vVolume;
layout(location = 13) flat out vec4 vAtten;

void main() {
    VertexData v = VertexData(iPos, iNormal, iUV, iUV2, iColor);
    vertex(v);
    mat4 m = model()SKIN;
    vec4 world = m * vec4(v.position, 1.0);
    gl_Position = frame.viewProj * world;
    vWorldPos = world.xyz;
    vNormal = normalize(mat3(m) * v.normal);
    vUV = v.uv;
    vUV2 = v.uv2;
    vColor = v.color;
    vViewDepth = -(frame.view * world).z;
    vBaseColor = iBaseColor;
    vMaterial = iMaterial;
    vExtra = iExtra;
    vUVT0 = iUVT0;
    vUVT1 = iUVT1;
    vSheen = iSheen;
    vVolume = iVolume;
    vAtten = iAtten;
}
`

// Shadow-pass vertex main: position from the light, plus what the
// depth fragment needs for alpha cutouts.
const shadowVertPostlude = `
layout(push_constant) uniform PC { int cascade; } pc;

layout(location = 0) out vec2 vUV;
layout(location = 1) flat out vec2 vCutout; // x base alpha, y cutoff

void main() {
    VertexData v = VertexData(iPos, iNormal, iUV, iUV2, iColor);
    vertex(v);
    mat4 m = model()SKIN;
    mat4 lightProj = pc.cascade < 3 ? frame.lightViewProj[pc.cascade] : frame.spotViewProj[pc.cascade - 3];
    gl_Position = lightProj * m * vec4(v.position, 1.0);
    vUV = uvTransform(v.uv);
    vCutout = vec2(iBaseColor.a * v.color.a, iExtra.y);
}
`

var (
	fragmentDef = regexp.MustCompile(`\bvec4\s+fragment\s*\(`)
	surfaceDef  = regexp.MustCompile(`\bvoid\s+surface\s*\(`)
	finishDef   = regexp.MustCompile(`\bvec4\s+finish\s*\(`)
	vertexDef   = regexp.MustCompile(`\bvoid\s+vertex\s*\(`)
	skinCall    = regexp.MustCompile(`SKIN`)
)

// HasVertexHook reports whether a mesh shader source defines vertex().
func HasVertexHook(source string) bool { return vertexDef.MatchString(source) }

// Compose wraps a game's shader source with the prelude and main for its
// kind and stage, filling in optional hooks the source leaves out. The
// result is a complete GLSL program for glslangValidator. Line numbers in
// compiler messages are offset by the prelude; Compose's second result is
// that offset. Sprite shaders have only StageFrag.
func Compose(kind Kind, stage Stage, source string) (glsl string, lineOffset int, err error) {
	switch kind {
	case Sprite:
		if stage != StageFrag {
			return "", 0, fmt.Errorf("sprite shaders have no %s stage", stage)
		}
		if !fragmentDef.MatchString(source) {
			return "", 0, fmt.Errorf("a sprite shader must define vec4 fragment(vec2 uv, vec4 color)")
		}
		return spritePrelude + source + spritePostlude, countLines(spritePrelude), nil
	case Mesh:
		if !surfaceDef.MatchString(source) {
			return "", 0, fmt.Errorf("a mesh shader must define void surface(inout Surface s)")
		}
		body := source
		if !finishDef.MatchString(source) {
			body += defaultFinish
		}
		if !vertexDef.MatchString(source) {
			body += defaultVertex
		}
		switch stage {
		case StageFrag:
			return meshPrelude + body + meshPostlude, countLines(meshPrelude), nil
		case StageVert:
			return vertCommon + body + skinCall.ReplaceAllString(litVertPostlude, ""), countLines(vertCommon), nil
		case StageSkinVert:
			pre := vertCommon + vertSkin
			return pre + body + skinCall.ReplaceAllString(litVertPostlude, " * skinMatrix()"), countLines(pre), nil
		case StageShadowVert:
			return vertCommon + body + skinCall.ReplaceAllString(shadowVertPostlude, ""), countLines(vertCommon), nil
		case StageShadowSkinVert:
			pre := vertCommon + vertSkin
			return pre + body + skinCall.ReplaceAllString(shadowVertPostlude, " * skinMatrix()"), countLines(pre), nil
		}
		return "", 0, fmt.Errorf("unknown stage %d", stage)
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

// A bundle holds every stage of a mesh shader with a vertex hook in one
// file: the magic, a count, then (stage, size, SPIR-V) records. A file
// without the magic is plain SPIR-V for the fragment stage alone.
var bundleMagic = []byte("BYSH")

// Bundle packs stage programs into one blob.
func Bundle(stages map[Stage][]byte) []byte {
	out := append([]byte{}, bundleMagic...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(stages)))
	for st := Stage(0); st < stageCount; st++ {
		spv, ok := stages[st]
		if !ok {
			continue
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(st))
		out = binary.LittleEndian.AppendUint32(out, uint32(len(spv)))
		out = append(out, spv...)
	}
	return out
}

// Unbundle splits a bundle into its stages; plain SPIR-V comes back as
// the fragment stage alone.
func Unbundle(data []byte) (map[Stage][]byte, error) {
	if len(data) < 4 || string(data[:4]) != string(bundleMagic) {
		return map[Stage][]byte{StageFrag: data}, nil
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("shader bundle is truncated")
	}
	n := binary.LittleEndian.Uint32(data[4:8])
	out := map[Stage][]byte{}
	p := 8
	for range n {
		if p+8 > len(data) {
			return nil, fmt.Errorf("shader bundle is truncated")
		}
		st := Stage(binary.LittleEndian.Uint32(data[p:]))
		size := int(binary.LittleEndian.Uint32(data[p+4:]))
		p += 8
		if p+size > len(data) || st >= stageCount {
			return nil, fmt.Errorf("shader bundle is corrupt")
		}
		out[st] = data[p : p+size]
		p += size
	}
	if _, ok := out[StageFrag]; !ok {
		return nil, fmt.Errorf("shader bundle has no fragment stage")
	}
	return out, nil
}
