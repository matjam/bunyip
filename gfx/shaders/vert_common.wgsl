struct Frame {
viewProj: mat4x4f,
view: mat4x4f,
lightViewProj: array<mat4x4f, 3>,
camPos: vec4f,
lightDir: vec4f,
lightColor: vec4f,
sky: vec4f,
ground: vec4f,
params: vec4f,
splits: vec4f,
radii: vec4f,
sh: array<vec4f, 9>,
env: vec4f,
invViewProj: mat4x4f,
horizon: vec4f,
skyUp: vec4f,
sun: vec4f,
sunColor: vec4f,
fog: vec4f,
fogRange: vec4f,
spotViewProj: array<mat4x4f, 4>,
pointViewProj: array<mat4x4f, 24>,
cluster: vec4f,
probePos: array<vec4f, 8>,
probeMin: array<vec4f, 8>,
probeMax: array<vec4f, 8>,
probeParams: array<vec4f, 8>,
gridOrigin: vec4f,
gridSpacing: vec4f,
gridCounts: vec4f,
reflect: vec4f,
atmos: vec4f,
betaR: vec4f,
betaM: vec4f,
}

struct LightData {
posRange: vec4f,
color: vec4f,
dir: vec4f,
info: vec4f,
}

struct Surface {
albedo: vec3f,
alpha: f32,
normal: vec3f,
metallic: f32,
roughness: f32,
emissive: vec3f,
occlusion: f32,
unlit: bool,
uv: vec2f,
uv2: vec2f,
color: vec4f,
worldPos: vec3f,
viewDir: vec3f,
clearcoat: f32,
clearcoatRoughness: f32,
sheen: vec3f,
sheenRoughness: f32,
subsurface: f32,
thickness: f32,
transmission: f32,
ior: f32,
volume: f32,
attenuation: vec3f,
attenuationDistance: f32,
specularColor: vec3f,
specular: f32,
iridescence: f32,
iridescenceIOR: f32,
iridescenceThickness: f32,
anisotropy: f32,
tangent: vec3f,
shell: f32,
}

struct VertexData {
position: vec3f,
normal: vec3f,
uv: vec2f,
uv2: vec2f,
color: vec4f,
}
@group(0) @binding(0) var tAlbedo: texture_2d<f32>;
@group(0) @binding(1) var tMetalRough: texture_2d<f32>;
@group(0) @binding(2) var tNormal: texture_2d<f32>;
@group(0) @binding(3) var tEmissive: texture_2d<f32>;
@group(0) @binding(4) var tOcclusion: texture_2d<f32>;
@group(0) @binding(5) var tImage0: texture_2d<f32>;
@group(0) @binding(6) var tImage1: texture_2d<f32>;
@group(0) @binding(7) var tImage2: texture_2d<f32>;
@group(0) @binding(8) var tImage3: texture_2d<f32>;
@group(0) @binding(9) var tEnv: texture_cube<f32>;
@group(0) @binding(10) var tThickness: texture_2d<f32>;
@group(0) @binding(11) var tScene: texture_2d<f32>;
@group(0) @binding(12) var tTransmission: texture_2d<f32>;
@group(0) @binding(13) var tIridescence: texture_2d<f32>;
@group(0) @binding(14) var tAnisotropy: texture_2d<f32>;
@group(0) @binding(15) var tSpecular: texture_2d<f32>;
@group(0) @binding(16) var tFur: texture_2d<f32>;
@group(0) @binding(17) var materialSampler0: sampler;
@group(0) @binding(18) var materialSampler1: sampler;
@group(0) @binding(19) var materialSampler2: sampler;
@group(0) @binding(20) var materialSampler3: sampler;
@group(1) @binding(0) var<uniform> frame: Frame;
struct VertexInput {
@location(0) iPos: vec3f,
@location(1) iNormal: vec3f,
@location(2) iUV: vec2f,
@location(3) iUV2: vec2f,
@location(4) iColor: vec4f,
@location(5) iModel0: vec4f,
@location(6) iModel1: vec4f,
@location(7) iModel2: vec4f,
@location(8) iBaseColor: vec4f,
@location(9) iMaterial: vec4f,
@location(10) iExtra: vec4f,
@location(11) iUVT0: vec4f,
@location(12) iUVT1: vec4f,
@location(13) iSheen: vec4f,
@location(14) iVolume: vec4f,
@location(15) iAtten: vec4f,
@location(16) iGI: vec4f,
@location(19) iSpec: vec4f,
@location(20) iIrid: vec4f,
@location(21) iFur: vec4f,
@location(22) iMorph: vec4f,
@location(23) iMorphW0: vec4f,
@location(24) iMorphW1: vec4f,
@location(25) iMorphIdx: vec2u,
}
var<private> iPos: vec3f;
var<private> iNormal: vec3f;
var<private> iUV: vec2f;
var<private> iUV2: vec2f;
var<private> iColor: vec4f;
var<private> iModel0: vec4f;
var<private> iModel1: vec4f;
var<private> iModel2: vec4f;
var<private> iBaseColor: vec4f;
var<private> iMaterial: vec4f;
var<private> iExtra: vec4f;
var<private> iUVT0: vec4f;
var<private> iUVT1: vec4f;
var<private> iSheen: vec4f;
var<private> iVolume: vec4f;
var<private> iAtten: vec4f;
var<private> iGI: vec4f;
var<private> iSpec: vec4f;
var<private> iIrid: vec4f;
var<private> iFur: vec4f;
var<private> iMorph: vec4f;
var<private> iMorphW0: vec4f;
var<private> iMorphW1: vec4f;
var<private> iMorphIdx: vec2u;
var<private> gl_VertexIndex: u32;
@group(5) @binding(0) var<storage, read> morphDeltas: array<f32>;
const MORPH_STRIDE: u32 = 6u;

fn morphWeight(_k: i32) -> f32 {
var k = _k;
if k < 4 { return iMorphW0[k]; }
return iMorphW1[k - 4];
}

fn morphTarget(_k: i32) -> u32 {
var k = _k;
return ((((select(iMorphIdx.y, iMorphIdx.x, (k < 4))) >> u32(u32((8 * ((k & 3))))))) & 0xFFu);
}

fn time() -> f32 {

return frame.params.w;
}

fn model() -> mat4x4f {

return mat4x4f(vec4f(iModel0.x, iModel1.x, iModel2.x, 0.0), vec4f(iModel0.y, iModel1.y, iModel2.y, 0.0), vec4f(iModel0.z, iModel1.z, iModel2.z, 0.0), vec4f(iModel0.w, iModel1.w, iModel2.w, 1.0));
}

fn uvTransform(_uv: vec2f) -> vec2f {
var uv = _uv;
return vec2f((((iUVT0.x * uv.x) + (iUVT0.y * uv.y)) + iUVT0.z), (((iUVT0.w * uv.x) + (iUVT1.x * uv.y)) + iUVT1.y));
}

fn morph(v: ptr<function, VertexData>) {

var n: i32 = i32(iMorph.z);
if (n == 0) { return; }
var base: u32 = u32(iMorph.x);
var count: u32 = u32(iMorph.y);
for (var k: i32 = 0; (k < n); k++) {
var w: f32 = morphWeight(k);
var at: u32 = (base + ((((morphTarget(k) * count) + u32(gl_VertexIndex))) * MORPH_STRIDE));
(*v).position += (w * vec3f(morphDeltas[at], morphDeltas[(at + 1u)], morphDeltas[(at + 2u)]));
(*v).normal += (w * vec3f(morphDeltas[(at + 3u)], morphDeltas[(at + 4u)], morphDeltas[(at + 5u)]));
}
(*v).normal = normalize((*v).normal);
}

fn perturbNormal(_n: vec3f, _pos: vec3f, _uv: vec2f) -> vec3f {
var n = _n;
var pos = _pos;
var uv = _uv;
return n;
}

fn surfaceTangent(_n: vec3f, _uv: vec2f, _dir: vec2f) -> vec3f {
var n = _n;
var uv = _uv;
var dir = _dir;
return n;
}

fn shadowFactor(_n: vec3f, _l: vec3f) -> f32 {
var n = _n;
var l = _l;
return 1.0;
}

fn shade(_n: vec3f, _v: vec3f, _l: vec3f, _radiance: vec3f, _albedo: vec3f, _metallic: f32, _roughness: f32) -> vec3f {
var n = _n;
var v = _v;
var l = _l;
var radiance = _radiance;
var albedo = _albedo;
var metallic = _metallic;
var roughness = _roughness;
return vec3f(0.0);
}

fn light(_s: Surface) -> vec3f {
var s = _s;
return vec3f(0.0);
}
fn texSampler(slot: i32) -> i32 { return (i32(iAtten.w) >> u32(2 * slot)) & 3; }
fn albedoTex(uv: vec2f) -> vec4f {
switch texSampler(0) {
case 0: { return textureSampleLevel(tAlbedo, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tAlbedo, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tAlbedo, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tAlbedo, materialSampler3, uv, 0.0); }
}
}
fn metalRoughTex(uv: vec2f) -> vec4f {
switch texSampler(1) {
case 0: { return textureSampleLevel(tMetalRough, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tMetalRough, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tMetalRough, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tMetalRough, materialSampler3, uv, 0.0); }
}
}
fn normalTex(uv: vec2f) -> vec4f {
switch texSampler(2) {
case 0: { return textureSampleLevel(tNormal, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tNormal, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tNormal, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tNormal, materialSampler3, uv, 0.0); }
}
}
fn emissiveTex(uv: vec2f) -> vec4f {
switch texSampler(3) {
case 0: { return textureSampleLevel(tEmissive, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tEmissive, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tEmissive, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tEmissive, materialSampler3, uv, 0.0); }
}
}
fn occlusionTex(uv: vec2f) -> vec4f {
switch texSampler(4) {
case 0: { return textureSampleLevel(tOcclusion, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tOcclusion, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tOcclusion, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tOcclusion, materialSampler3, uv, 0.0); }
}
}
fn image0(uv: vec2f) -> vec4f {
switch texSampler(5) {
case 0: { return textureSampleLevel(tImage0, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tImage0, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tImage0, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tImage0, materialSampler3, uv, 0.0); }
}
}
fn image1(uv: vec2f) -> vec4f {
switch texSampler(6) {
case 0: { return textureSampleLevel(tImage1, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tImage1, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tImage1, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tImage1, materialSampler3, uv, 0.0); }
}
}
fn image2(uv: vec2f) -> vec4f {
switch texSampler(7) {
case 0: { return textureSampleLevel(tImage2, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tImage2, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tImage2, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tImage2, materialSampler3, uv, 0.0); }
}
}
fn image3(uv: vec2f) -> vec4f {
switch texSampler(8) {
case 0: { return textureSampleLevel(tImage3, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tImage3, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tImage3, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tImage3, materialSampler3, uv, 0.0); }
}
}
fn thicknessTex(uv: vec2f) -> vec4f {
switch texSampler(9) {
case 0: { return textureSampleLevel(tThickness, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tThickness, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tThickness, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tThickness, materialSampler3, uv, 0.0); }
}
}
fn transmissionTex(uv: vec2f) -> vec4f {
switch texSampler(10) {
case 0: { return textureSampleLevel(tTransmission, materialSampler0, uv, 0.0); }
case 1: { return textureSampleLevel(tTransmission, materialSampler1, uv, 0.0); }
case 2: { return textureSampleLevel(tTransmission, materialSampler2, uv, 0.0); }
default: { return textureSampleLevel(tTransmission, materialSampler3, uv, 0.0); }
}
}
fn iridescenceTex(uv: vec2f) -> vec4f {
return textureSampleLevel(tIridescence, materialSampler0, uv, 0.0);
}
fn anisotropyTex(uv: vec2f) -> vec4f {
return textureSampleLevel(tAnisotropy, materialSampler0, uv, 0.0);
}
fn specularTex(uv: vec2f) -> vec4f {
return textureSampleLevel(tSpecular, materialSampler0, uv, 0.0);
}
fn furTex(uv: vec2f) -> vec4f {
return textureSampleLevel(tFur, materialSampler0, uv, 0.0);
}

// Vertex image sampling uses mip zero because derivatives are unavailable.
fn sampleImage0(uv: vec2f) -> vec4f { return image0(uv); }

// Vertex image sampling uses mip zero because derivatives are unavailable.
fn sampleImage1(uv: vec2f) -> vec4f { return image1(uv); }

// Vertex image sampling uses mip zero because derivatives are unavailable.
fn sampleImage2(uv: vec2f) -> vec4f { return image2(uv); }

// Vertex image sampling uses mip zero because derivatives are unavailable.
fn sampleImage3(uv: vec2f) -> vec4f { return image3(uv); }
