package shaders

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed prelude_mesh.wgsl
var meshWGSL string

//go:embed vert_common.wgsl
var meshVertexWGSL string

//go:embed vert_skin.wgsl
var meshSkinWGSL string

// composeMesh preserves the six mesh stages with one shared material contract.
func composeMesh(stage Stage, source string) (string, int, error) {
	if !hookPresent(source, "surface") {
		return "", 0, fmt.Errorf("a mesh shader must define fn surface(s: Surface) -> Surface")
	}
	body := source
	if !hookPresent(source, "finish") {
		body += "\nfn finish(lit: vec4f, s: Surface) -> vec4f { return lit; }\n"
	}
	if !hookPresent(source, "vertex") {
		body += "\nfn vertex(v: VertexData) -> VertexData { return v; }\n"
	}
	switch stage {
	case StageFrag, StageOITFrag:
		post := meshFragmentWGSL
		if stage == StageFrag {
			post = strings.Replace(post, "return lit;", "if vGI.y > 0.5 && frame.reflect.x > 0.0 { lit.a = reflectWeight(s); }\nreturn lit;", 1)
		}
		entry := meshFragmentEntryWGSL
		if stage == StageOITFrag {
			entry = meshOITEntryWGSL
		}
		return meshWGSL + body + post + entry, strings.Count(meshWGSL, "\n"), nil
	case StageVert, StageSkinVert, StageShadowVert, StageShadowSkinVert:
		pre := meshVertexWGSL
		skin := stage == StageSkinVert || stage == StageShadowSkinVert
		if skin {
			pre = strings.Replace(pre, "struct VertexInput {", "struct VertexInput {\n@location(17) iJoints: vec4u,\n@location(18) iWeights: vec4f,", 1)
			pre += meshSkinWGSL + "\nvar<private> inputJoints: vec4u;\nvar<private> inputWeights: vec4f;\n"
		}
		post, entry := meshLitVertexWGSL, meshLitVertexEntryWGSL
		if stage == StageShadowVert || stage == StageShadowSkinVert {
			post, entry = meshShadowVertexWGSL, meshShadowVertexEntryWGSL
		}
		if skin {
			post = strings.Replace(post, "model()", "(model() * skinMatrix(inputJoints, inputWeights))", 1)
			entry = strings.Replace(entry, "meshVertex();", "inputJoints = input.iJoints; inputWeights = input.iWeights; meshVertex();", 1)
		}
		return pre + body + post + entry, strings.Count(pre, "\n"), nil
	default:
		return "", 0, fmt.Errorf("unknown mesh shader stage %d", stage)
	}
}

const meshFragmentWGSL = `fn meshShade() -> vec4f {

var s: Surface;
var uv: vec2f = uvTransform(vUV);
// Color textures store premultiplied texels; mesh hooks use straight color.
let texel = albedoTex(uv);
let straightTexel = vec4f(texel.rgb / max(texel.a, 1e-4), texel.a);
var albedoSample: vec4f = ((straightTexel * vBaseColor) * vColor);
s.albedo = albedoSample.rgb;
s.alpha = albedoSample.a;
var mr: vec3f = metalRoughTex(uv).rgb;
s.metallic = clamp((vMaterial.x * mr.b), 0.0, 1.0);
s.roughness = clamp((vMaterial.y * mr.g), 0.04, 1.0);
var n: vec3f = normalize(vNormal);
if (!gl_FrontFacing) { n = (-n); }
var flags: f32 = vMaterial.w;
var mappedNormal: vec3f = perturbNormal(n, vWorldPos, uv);
if ((flags - 2.0 * floor(flags / 2.0)) >= 1.0) { n = mappedNormal; }
s.normal = n;
s.uv = uv;
s.uv2 = vUV2;
s.color = vColor;
s.worldPos = vWorldPos;
s.viewDir = normalize((frame.camPos.xyz - vWorldPos));
var glow: vec3f = select(emissiveTex(uv).rgb, albedoSample.rgb, ((flags - 16.0 * floor(flags / 16.0)) >= 8.0));
s.emissive = (glow * vMaterial.z);
var aoUV: vec2f = select(uv, vUV2, ((flags - 8.0 * floor(flags / 8.0)) >= 4.0));
s.occlusion = mix(1.0, occlusionTex(aoUV).r, vExtra.z);
s.unlit = ((flags - 4.0 * floor(flags / 4.0)) >= 2.0);
s.clearcoat = vUVT1.z;
s.clearcoatRoughness = vUVT1.w;
s.sheen = vSheen.rgb;
s.sheenRoughness = vSheen.w;
s.subsurface = vExtra.w;
s.thickness = thicknessTex(uv).r;
s.transmission = (vVolume.x * transmissionTex(uv).r);
s.ior = vVolume.y;
s.volume = (vVolume.z * s.thickness);
s.attenuation = vAtten.rgb;
s.attenuationDistance = vVolume.w;
var specSample: vec4f = specularTex(uv);
s.specularColor = (vSpec.rgb * specSample.rgb);
s.specular = (vSpec.w * specSample.a);
var iridSample: vec2f = iridescenceTex(uv).rg;
s.iridescence = (vIrid.x * iridSample.r);
s.iridescenceIOR = vIrid.y;
s.iridescenceThickness = mix(vIrid.z, vIrid.w, iridSample.g);
var anisoSample: vec3f = anisotropyTex(uv).rgb;
s.anisotropy = (vFur.x * anisoSample.b);
s.tangent = n;
{
var dir: vec2f = ((anisoSample.rg * 2.0) - vec2f(1.0));
var c: f32 = cos(vFur.y);
var sn: f32 = sin(vFur.y);
s.tangent = surfaceTangent(n, uv, vec2f(((dir.x * c) - (dir.y * sn)), ((dir.x * sn) + (dir.y * c))));
}
s.shell = vFur.w;
s = surface(s);
var strand: f32 = furTex(uv).r;
if ((vExtra.y > 0.0) && (s.alpha < vExtra.y)) { discard; }
if (s.shell > 0.0) {
if (strand < s.shell) { discard; }
s.alpha *= (1.0 - (s.shell * 0.5));
}
var color = s.albedo;
if !s.unlit { color = light(s); }
var lit: vec4f = finish(vec4f((color + s.emissive), s.alpha), s);
lit = vec4f(applyFog(lit.rgb, vWorldPos, vViewDepth), lit.a);
// vGI.y marks opaque draws. Both sorted source-over and OIT accumulation
// need premultiplied color, after lighting, finish and fog have run.
if vGI.y < 0.5 { lit = vec4f(lit.rgb * lit.a, lit.a); }
return lit;
}
`
const meshFragmentEntryWGSL = `
@fragment fn main(input: MeshInput, @builtin(position) coord: vec4f, @builtin(front_facing) facing: bool) -> @location(0) vec4f {
vWorldPos = input.vWorldPos;
vNormal = input.vNormal;
vUV = input.vUV;
vViewDepth = input.vViewDepth;
vBaseColor = input.vBaseColor;
vMaterial = input.vMaterial;
vExtra = input.vExtra;
vUV2 = input.vUV2;
vColor = input.vColor;
vUVT0 = input.vUVT0;
vUVT1 = input.vUVT1;
vSheen = input.vSheen;
vVolume = input.vVolume;
vAtten = input.vAtten;
vGI = input.vGI;
vSpec = input.vSpec;
vIrid = input.vIrid;
vFur = input.vFur;
gl_FragCoord = coord; gl_FrontFacing = facing;
return meshShade();
}
`
const meshOITEntryWGSL = `
struct OITOutput { @location(0) color: vec4f, @location(1) reveal: f32, }
@fragment fn main(input: MeshInput, @builtin(position) coord: vec4f, @builtin(front_facing) facing: bool) -> OITOutput {
vWorldPos = input.vWorldPos;
vNormal = input.vNormal;
vUV = input.vUV;
vViewDepth = input.vViewDepth;
vBaseColor = input.vBaseColor;
vMaterial = input.vMaterial;
vExtra = input.vExtra;
vUV2 = input.vUV2;
vColor = input.vColor;
vUVT0 = input.vUVT0;
vUVT1 = input.vUVT1;
vSheen = input.vSheen;
vVolume = input.vVolume;
vAtten = input.vAtten;
vGI = input.vGI;
vSpec = input.vSpec;
vIrid = input.vIrid;
vFur = input.vFur;
gl_FragCoord = coord; gl_FrontFacing = facing;
let lit = meshShade();
// meshShade already premultiplies RGB; alpha below is also part of the
// depth weight. The resolve divides by accumulated alpha times weight.
let weight = lit.a * clamp(3e3 * pow(1.0 - coord.z, 3.0), 1e-2, 1e3);
return OITOutput(lit * weight, lit.a);
}
`
const meshLitVertexWGSL = `
var<private> vWorldPos: vec3f;
var<private> vNormal: vec3f;
var<private> vUV: vec2f;
var<private> vViewDepth: f32;
var<private> vBaseColor: vec4f;
var<private> vMaterial: vec4f;
var<private> vExtra: vec4f;
var<private> vUV2: vec2f;
var<private> vColor: vec4f;
var<private> vUVT0: vec4f;
var<private> vUVT1: vec4f;
var<private> vSheen: vec4f;
var<private> vVolume: vec4f;
var<private> vAtten: vec4f;
var<private> vGI: vec4f;
var<private> vSpec: vec4f;
var<private> vIrid: vec4f;
var<private> vFur: vec4f;
var<private> gl_Position: vec4f;
fn meshVertex() {

var v: VertexData = VertexData(iPos, iNormal, iUV, iUV2, iColor);
morph(&v);
v = vertex(v);
var m: mat4x4f = model();
var world: vec4f = (m * vec4f(v.position, 1.0));
vNormal = normalize((mat3x3f(m[0].xyz, m[1].xyz, m[2].xyz) * v.normal));
world = vec4f(world.xyz + (vNormal * iFur.z), world.w);
gl_Position = (frame.viewProj * world);
vWorldPos = world.xyz;
vUV = v.uv;
vUV2 = v.uv2;
vColor = v.color;
vViewDepth = (-((frame.view * world)).z);
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
const meshLitVertexEntryWGSL = `
struct VertexOutput {
@builtin(position) position: vec4f,
@location(0) vWorldPos: vec3f,
@location(1) vNormal: vec3f,
@location(2) vUV: vec2f,
@location(3) vViewDepth: f32,
@location(4) @interpolate(flat) vBaseColor: vec4f,
@location(5) @interpolate(flat) vMaterial: vec4f,
@location(6) @interpolate(flat) vExtra: vec4f,
@location(7) vUV2: vec2f,
@location(8) vColor: vec4f,
@location(9) @interpolate(flat) vUVT0: vec4f,
@location(10) @interpolate(flat) vUVT1: vec4f,
@location(11) @interpolate(flat) vSheen: vec4f,
@location(12) @interpolate(flat) vVolume: vec4f,
@location(13) @interpolate(flat) vAtten: vec4f,
@location(14) @interpolate(flat) vGI: vec4f,
@location(15) @interpolate(flat) vSpec: vec4f,
@location(16) @interpolate(flat) vIrid: vec4f,
@location(17) @interpolate(flat) vFur: vec4f,
}
@vertex fn main(input: VertexInput, @builtin(vertex_index) index: u32) -> VertexOutput {
iPos = input.iPos;
iNormal = input.iNormal;
iUV = input.iUV;
iUV2 = input.iUV2;
iColor = input.iColor;
iModel0 = input.iModel0;
iModel1 = input.iModel1;
iModel2 = input.iModel2;
iBaseColor = input.iBaseColor;
iMaterial = input.iMaterial;
iExtra = input.iExtra;
iUVT0 = input.iUVT0;
iUVT1 = input.iUVT1;
iSheen = input.iSheen;
iVolume = input.iVolume;
iAtten = input.iAtten;
iGI = input.iGI;
iSpec = input.iSpec;
iIrid = input.iIrid;
iFur = input.iFur;
iMorph = input.iMorph;
iMorphW0 = input.iMorphW0;
iMorphW1 = input.iMorphW1;
iMorphIdx = input.iMorphIdx;
gl_VertexIndex = index;
meshVertex();
return VertexOutput(gl_Position, vWorldPos, vNormal, vUV, vViewDepth, vBaseColor, vMaterial, vExtra, vUV2, vColor, vUVT0, vUVT1, vSheen, vVolume, vAtten, vGI, vSpec, vIrid, vFur);
}
`
const meshShadowVertexWGSL = `
struct PC { map: i32, }
var<push_constant> pc: PC;
var<private> gl_Position: vec4f;
var<private> vUV: vec2f;
var<private> vCutout: vec3f;
fn meshVertex() {

var v: VertexData = VertexData(iPos, iNormal, iUV, iUV2, iColor);
morph(&v);
v = vertex(v);
var m: mat4x4f = model();
var lightProj: mat4x4f;
if (pc.map < 3) { lightProj = frame.lightViewProj[pc.map]; } else if (pc.map < 7) { lightProj = frame.spotViewProj[(pc.map - 3)]; } else { lightProj = frame.pointViewProj[(pc.map - 7)]; }
gl_Position = ((lightProj * m) * vec4f(v.position, 1.0));
vUV = uvTransform(v.uv);
vCutout = vec3f((iBaseColor.a * v.color.a), iExtra.y, f32(texSampler(0)));
}

`
const meshShadowVertexEntryWGSL = `
struct ShadowOutput { @builtin(position) position: vec4f, @location(0) vUV: vec2f, @location(1) @interpolate(flat) vCutout: vec3f, }
@vertex fn main(input: VertexInput, @builtin(vertex_index) index: u32) -> ShadowOutput {
iPos = input.iPos;
iNormal = input.iNormal;
iUV = input.iUV;
iUV2 = input.iUV2;
iColor = input.iColor;
iModel0 = input.iModel0;
iModel1 = input.iModel1;
iModel2 = input.iModel2;
iBaseColor = input.iBaseColor;
iMaterial = input.iMaterial;
iExtra = input.iExtra;
iUVT0 = input.iUVT0;
iUVT1 = input.iUVT1;
iSheen = input.iSheen;
iVolume = input.iVolume;
iAtten = input.iAtten;
iGI = input.iGI;
iSpec = input.iSpec;
iIrid = input.iIrid;
iFur = input.iFur;
iMorph = input.iMorph;
iMorphW0 = input.iMorphW0;
iMorphW1 = input.iMorphW1;
iMorphIdx = input.iMorphIdx;
gl_VertexIndex = index;
meshVertex();
return ShadowOutput(gl_Position, vUV, vCutout);
}
`
