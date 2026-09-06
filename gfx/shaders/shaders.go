// Package shaders holds the gfx package's GLSL sources and committed SPIR-V,
// and the preludes that game shaders are compiled against. Compose builds
// GLSL without invoking a compiler; bunyip-shader invokes glslangValidator
// offline. Mesh bundles contain regular and order-independent fragment
// programs and optionally static/skinned vertex programs for lit and
// shadow passes. Embedded byte slices are shared and must not be modified.
package shaders

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
)

// The 2D and mesh programs, including the engine's own, are game-style
// shader bodies compiled against the preludes by bunyip-shader.

//go:generate glslangValidator -V -o sprite.vert.spv sprite.vert
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sprite.frag.spv sprite_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sdf.frag.spv sdf_default.glsl
//go:generate glslangValidator -V -S frag -o text_outline.frag.spv text_outline.glsl
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
	//go:embed text_outline.frag.spv
	TextOutlineFrag []byte
)

//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage frag -o pbr.frag.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage oitfrag -o pbr_oit.frag.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage vert -o pbr.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage skinvert -o pbr_skin.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowvert -o shadow.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowskinvert -o shadow_skin.vert.spv pbr_default.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage frag -o terrain.frag.spv terrain_default.glsl
//go:generate glslangValidator -V -o shadow.frag.spv shadow.frag
//go:generate glslangValidator -V -o post.vert.spv post.vert
//go:generate glslangValidator -V -o post.frag.spv post.frag
//go:generate glslangValidator -V -o bright.frag.spv bright.frag
//go:generate glslangValidator -V -o blur.frag.spv blur.frag
//go:generate glslangValidator -V -o fxaa.frag.spv fxaa.frag
//go:generate glslangValidator -V -o ssao.frag.spv ssao.frag
//go:generate glslangValidator -V -o ssr.frag.spv ssr.frag
//go:generate glslangValidator -V -o aoblur.frag.spv aoblur.frag
//go:generate glslangValidator -V -o velocity.vert.spv velocity.vert
//go:generate glslangValidator -V -o velocity_skin.vert.spv velocity_skin.vert
//go:generate glslangValidator -V -o velocity.frag.spv velocity.frag
//go:generate glslangValidator -V -o taa.frag.spv taa.frag
//go:generate glslangValidator -V -o dof.frag.spv dof.frag
//go:generate glslangValidator -V -o motionblur.frag.spv motionblur.frag
//go:generate glslangValidator -V -o godrays.frag.spv godrays.frag
//go:generate glslangValidator -V -o oit.frag.spv oit.frag
//go:generate glslangValidator -V -o sky.frag.spv sky.frag
//go:generate glslangValidator -V -o skyparam.frag.spv skyparam.frag
//go:generate glslangValidator -V -o line.vert.spv line.vert
//go:generate glslangValidator -V -o line.frag.spv line.frag
//go:generate glslangValidator -V -o particle.vert.spv particle.vert
//go:generate glslangValidator -V -o particle.frag.spv particle.frag
//go:generate glslangValidator -V -o particle3d.vert.spv particle3d.vert
//go:generate glslangValidator -V -o particle3d.frag.spv particle3d.frag
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
	//go:embed particle.vert.spv
	ParticleVert []byte
	//go:embed particle.frag.spv
	ParticleFrag []byte
	//go:embed particle3d.vert.spv
	Particle3DVert []byte
	//go:embed particle3d.frag.spv
	Particle3DFrag []byte
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
	//go:embed pbr_oit.frag.spv
	PBROITFrag []byte
	// TerrainFrag blends four tiling layers by a splat map, for gfx.Terrain.
	//go:embed terrain.frag.spv
	TerrainFrag []byte
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
	//go:embed ssr.frag.spv
	SSRFrag []byte
	//go:embed aoblur.frag.spv
	AOBlurFrag []byte
	//go:embed velocity.vert.spv
	VelocityVert []byte
	//go:embed velocity_skin.vert.spv
	VelocitySkinVert []byte
	//go:embed velocity.frag.spv
	VelocityFrag []byte
	//go:embed taa.frag.spv
	TAAFrag []byte
	//go:embed dof.frag.spv
	DOFFrag []byte
	//go:embed motionblur.frag.spv
	MotionBlurFrag []byte
	//go:embed godrays.frag.spv
	GodRaysFrag []byte
	//go:embed oit.frag.spv
	OITFrag []byte
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
	StageOITFrag                     // the fragment program of the order-independent transparency pass
	stageCount
)

// String names a stage as bunyip-shader's -stage flag spells it.
func (s Stage) String() string {
	names := [...]string{"frag", "vert", "skinvert", "shadowvert", "shadowskinvert", "oitfrag"}
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
    vec4 specSample = texture(specularTex, uv);
    s.specularColor = vSpec.rgb * specSample.rgb;
    s.specular = vSpec.w * specSample.a;
    // glTF packs the thin film's strength in red and its thickness in
    // green, the thickness running from the minimum to the maximum.
    vec2 iridSample = texture(iridescenceTex, uv).rg;
    s.iridescence = vIrid.x * iridSample.r;
    s.iridescenceIOR = vIrid.y;
    s.iridescenceThickness = mix(vIrid.z, vIrid.w, iridSample.g);
    // An anisotropy map holds the direction the highlight stretches in
    // as red and green around a half, and the strength in blue.
    vec3 anisoSample = texture(anisotropyTex, uv).rgb;
    s.anisotropy = vFur.x * anisoSample.b;
    s.tangent = n;
    if (s.anisotropy != 0.0) {
        vec2 dir = anisoSample.rg * 2.0 - 1.0;
        float c = cos(vFur.y), sn = sin(vFur.y);
        s.tangent = surfaceTangent(n, uv, vec2(dir.x * c - dir.y * sn, dir.x * sn + dir.y * c));
    }
    s.shell = vFur.w;
    surface(s);
    if (vExtra.y > 0.0 && s.alpha < vExtra.y) discard;
    if (s.shell > 0.0) {
        // A fur shell keeps only the fragments where a strand reaches
        // this far out, and fades as it goes.
        if (texture(furTex, uv).r < s.shell) discard;
        s.alpha *= 1.0 - s.shell * 0.5;
    }
    vec3 color = s.unlit ? s.albedo : light(s);
    vec4 lit = finish(vec4(color + s.emissive, s.alpha), s);
    lit.rgb = applyFog(lit.rgb, vWorldPos, vViewDepth);
OUTPUT}
`

// meshOutput and meshOITOutput are the two ways the fragment main ends,
// substituted for OUTPUT in meshPostlude. The order-independent pass
// writes two attachments and every other pass writes one, so the two are
// separate programs rather than one with a branch: a program that writes
// an attachment its pass does not have is a validation warning on every
// draw.
const meshOutput = `    // An opaque draw's alpha is never read as coverage, so the frame
    // carries the screen-space reflection weight there instead and the
    // reflection pass reads it back from the scene copy.
    if (vGI.y > 0.5 && frame.reflect.x > 0.0) lit.a = reflectWeight(s);
    outColor = lit;
`

const meshOITOutput = `    // The colour goes into the accumulation attachment scaled by the
    // depth weight, and the alpha multiplies the revealage attachment
    // down. The composite divides one by the other, which gives back
    // exactly this fragment's colour where it is the only layer.
    outColor = lit * oitWeight(lit.a, gl_FragCoord.z);
    outReveal = lit.a;
`

// meshOITDecls is what the order-independent fragment program has that
// the others do not. It goes after the game's own code, so the lines a
// compiler reports still line up with the file.
const meshOITDecls = `
layout(location = 1) out float outReveal;

// oitWeight is how much a translucent fragment counts in the
// order-independent pass: nearer fragments weigh far more, so a surface
// in front of another still looks like it is in front (McGuire and
// Bavoil, equation 10). It reads the depth buffer's own scale, so it
// holds whatever size a game's world is, and stops at a thousand so a
// stack of layers cannot overflow the half-float target.
float oitWeight(float alpha, float z) {
    return alpha * clamp(3e3 * pow(1.0 - z, 3.0), 1e-2, 1e3);
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
layout(location = 14) flat out vec4 vGI;
layout(location = 15) flat out vec4 vSpec;
layout(location = 16) flat out vec4 vIrid;
layout(location = 17) flat out vec4 vFur;

void main() {
    VertexData v = VertexData(iPos, iNormal, iUV, iUV2, iColor);
    morph(v);
    vertex(v);
    mat4 m = model()SKIN;
    vec4 world = m * vec4(v.position, 1.0);
    vNormal = normalize(mat3(m) * v.normal);
    // A fur shell is the same mesh pushed out along its normals.
    world.xyz += vNormal * iFur.z;
    gl_Position = frame.viewProj * world;
    vWorldPos = world.xyz;
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
    vGI = iGI;
    vSpec = iSpec;
    vIrid = iIrid;
    vFur = iFur;
}
`

// Shadow-pass vertex main: position from the light, plus what the
// depth fragment needs for alpha cutouts.
const shadowVertPostlude = `
layout(push_constant) uniform PC { int map; } pc; // the shadow map being drawn

layout(location = 0) out vec2 vUV;
layout(location = 1) flat out vec3 vCutout; // x base alpha, y cutoff, z albedo sampler

void main() {
    VertexData v = VertexData(iPos, iNormal, iUV, iUV2, iColor);
    morph(v);
    vertex(v);
    mat4 m = model()SKIN;
    // The maps run cascades, then spot maps, then cube faces.
    mat4 lightProj;
    if (pc.map < 3) lightProj = frame.lightViewProj[pc.map];
    else if (pc.map < 7) lightProj = frame.spotViewProj[pc.map - 3];
    else lightProj = frame.pointViewProj[pc.map - 7];
    gl_Position = lightProj * m * vec4(v.position, 1.0);
    vUV = uvTransform(v.uv);
    vCutout = vec3(iBaseColor.a * v.color.a, iExtra.y, float(texSampler(0)));
}
`

var (
	fragmentDef = regexp.MustCompile(`\bvec4\s+fragment\s*\(`)
	surfaceDef  = regexp.MustCompile(`\bvoid\s+surface\s*\(`)
	finishDef   = regexp.MustCompile(`\bvec4\s+finish\s*\(`)
	vertexDef   = regexp.MustCompile(`\bvoid\s+vertex\s*\(`)
	skinCall    = regexp.MustCompile(`SKIN`)
)

// HasVertexHook reports whether source matches a void vertex declaration.
// It uses a textual pattern, not a GLSL parser; comments are not stripped.
func HasVertexHook(source string) bool { return vertexDef.MatchString(source) }

// Compose wraps a game's shader source with the prelude and main for its
// kind and stage, filling in optional hooks the source leaves out. The
// result is a complete GLSL program for glslangValidator. Line numbers in
// compiler messages are offset by the prelude; Compose's second result is
// that offset. Sprite shaders have only StageFrag. Mesh vertex hooks run
// after morph blending and before skinning in both lit and shadow passes.
// Hook detection is textual; GLSL syntax is checked by the compiler later.
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
			return meshPrelude + body + strings.Replace(meshPostlude, "OUTPUT", meshOutput, 1), countLines(meshPrelude), nil
		case StageOITFrag:
			return meshPrelude + body + meshOITDecls + strings.Replace(meshPostlude, "OUTPUT", meshOITOutput, 1), countLines(meshPrelude), nil
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

// A bundle holds the programs of a mesh shader in one
// file: the magic, a count, then (stage, size, SPIR-V) records. A file
// without the magic is plain SPIR-V for the fragment stage alone.
var bundleMagic = []byte("BYSH")

// Bundle copies stage programs into a new blob in stage-number order.
// Supply only defined Stage values and include StageFrag for Unbundle.
// It does not validate SPIR-V program bytes.
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
// the fragment stage alone. Returned slices alias data. It validates bundle
// record bounds and stage numbers, but not the SPIR-V bytes themselves;
// NewMeshShader validates those when loading the programs.
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
