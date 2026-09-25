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
atmosView: vec4f,
atmosLimb: vec4f,
}

struct LightData {
posRange: vec4f,
color: vec4f,
dir: vec4f,
info: vec4f,
}

// Surface.albedo and finish() use straight color. The engine removes the
// albedo texel's premultiplication before surface(), then premultiplies
// blended output after finish() and fog for sorted and OIT draws alike.
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
struct MeshInput {
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
var<private> gl_FragCoord: vec4f;
var<private> gl_FrontFacing: bool;
@group(1) @binding(1) var<storage, read> probeGrid: ProbeGrid;
struct ProbeGrid { cells: array<vec4f>, }
@group(1) @binding(2) var<storage, read> lights: array<LightData>;
@group(1) @binding(3) var<storage, read> clusterCells: array<vec2u>;
@group(1) @binding(4) var<storage, read> lightIndex: array<u32>;
@group(2) @binding(0) var shadowAtlas: texture_depth_2d;
@group(2) @binding(1) var shadowSampler: sampler_comparison;
// The atmosphere's lookup tables, built by atmoslut.frag.wgsl on frames
// with an atmosphere, and the linear clamping sampler they are read
// through. Without an atmosphere nothing reads them.
@group(2) @binding(2) var atmosTransTable: texture_2d<f32>;
@group(2) @binding(3) var atmosSkyTable: texture_2d<f32>;
@group(2) @binding(4) var atmosAerialTable: texture_2d<f32>;
@group(2) @binding(5) var atmosReflectTable: texture_2d<f32>;
@group(2) @binding(6) var atmosSampler: sampler;
const CLUSTER_X: u32 = 16u;

const CLUSTER_Y: u32 = 9u;

const CLUSTER_Z: u32 = 24u;


const POINT_FACE: f32 = 512.0;

fn texSampler(_slot: i32) -> i32 {
var slot = _slot;
return (((i32(vAtten.w) >> u32(((2 * slot))))) & 3);
}

fn model() -> mat4x4f {

return mat4x4f(vec4f(1.0, 0.0, 0.0, 0.0), vec4f(0.0, 1.0, 0.0, 0.0), vec4f(0.0, 0.0, 1.0, 0.0), vec4f(0.0, 0.0, 0.0, 1.0));
}

fn time() -> f32 {

return frame.params.w;
}

fn uvTransform(_uv: vec2f) -> vec2f {
var uv = _uv;
return vec2f((((vUVT0.x * uv.x) + (vUVT0.y * uv.y)) + vUVT0.z), (((vUVT0.w * uv.x) + (vUVT1.x * uv.y)) + vUVT1.y));
}

const PI: f32 = 3.14159265359;

fn perturbNormal(_n: vec3f, _pos: vec3f, _uv: vec2f) -> vec3f {
var n = _n;
var pos = _pos;
var uv = _uv;
var dp1: vec3f = dpdx(pos);
var dp2: vec3f = dpdy(pos);
var duv1: vec2f = dpdx(uv);
var duv2: vec2f = dpdy(uv);
var dp2perp: vec3f = cross(dp2, n);
var dp1perp: vec3f = cross(n, dp1);
var t: vec3f = ((dp2perp * duv1.x) + (dp1perp * duv2.x));
var b: vec3f = ((dp2perp * duv1.y) + (dp1perp * duv2.y));
var invmax: f32 = inverseSqrt((max(dot(t, t), dot(b, b)) + 1e-8));
var tbn: mat3x3f = mat3x3f((t * invmax), (b * invmax), n);
var nm: vec3f = ((normalTex(uv).xyz * 2.0) - vec3f(1.0));
return normalize((tbn * nm));
}

fn surfaceTangent(_n: vec3f, _uv: vec2f, _dir: vec2f) -> vec3f {
var n = _n;
var uv = _uv;
var dir = _dir;
var dp1: vec3f = dpdx(vWorldPos);
var dp2: vec3f = dpdy(vWorldPos);
var duv1: vec2f = dpdx(uv);
var duv2: vec2f = dpdy(uv);
var dp2perp: vec3f = cross(dp2, n);
var dp1perp: vec3f = cross(n, dp1);
var t: vec3f = ((dp2perp * duv1.x) + (dp1perp * duv2.x));
if (dot(t, t) < 1e-12) { t = select(vec3f(1.0, 0.0, 0.0), cross(vec3f(0.0, 1.0, 0.0), n), (abs(n.y) < 0.99)); }
t = normalize((t - (n * dot(n, t))));
var b: vec3f = cross(n, t);
var turned: vec3f = ((t * dir.x) + (b * dir.y));
if dot(turned, turned) < 1e-12 { return t; }
return normalize(turned);
}

fn sampleAtlas(_origin: vec2f, _size: f32, _uvz: vec3f) -> f32 {
var origin = _origin;
var size = _size;
var uvz = _uvz;
var lit: f32 = 0.0;
var base: vec2f = (((origin + (uvz.xy * size))) / vec2f(4096.0, 6144.0));
var texel: vec2f = (vec2f(1.0) / vec2f(4096.0, 6144.0));
for (var y: i32 = (-1); (y <= 1); y++) { for (var x: i32 = (-1); (x <= 1); x++) {
lit += textureSampleCompareLevel(shadowAtlas, shadowSampler, (base + (vec2f(f32(x), f32(y)) * texel)), uvz.z);
} }
return (lit / 9.0);
}

fn sampleCascade(_c: i32, _uvz: vec3f) -> f32 {
var c = _c;
var uvz = _uvz;
return sampleAtlas((vec2f(f32((c % 2)), f32((c / 2))) * 2048.0), 2048.0, uvz);
}

fn shadowFactor(_n: vec3f, _l: vec3f) -> f32 {
var n = _n;
var l = _l;
if (frame.params.y < 0.5) { return 1.0; }
var c: i32 = select((select(2, 1, (vViewDepth < frame.splits.y))), 0, (vViewDepth < frame.splits.x));
if (vViewDepth > frame.splits.z) { return 1.0; }
var radius: f32 = select((select(frame.radii.z, frame.radii.y, (c == 1))), frame.radii.x, (c == 0));
var texelWorld: f32 = ((2.0 * radius) / frame.params.x);
var NoL: f32 = clamp(dot(n, l), 0.0, 1.0);
var slope: f32 = (sqrt((1.0 - (NoL * NoL))) / max(NoL, 0.05));
var pos: vec3f = (vWorldPos + ((n * texelWorld) * ((1.0 + slope))));
var sp: vec4f = (frame.lightViewProj[c] * vec4f(pos, 1.0));
var p: vec3f = (sp.xyz / vec3f(sp.w));
var uv: vec2f = ((p.xy * 0.5) + vec2f(0.5));
if (((((uv.x < 0.0) || (uv.x > 1.0)) || (uv.y < 0.0)) || (uv.y > 1.0)) || (p.z > 1.0)) { return 1.0; }
var bias: f32 = (texelWorld / ((4.0 * radius)));
return sampleCascade(c, vec3f(uv, (p.z - bias)));
}

fn sampleSpot(_k: i32, _uvz: vec3f) -> f32 {
var k = _k;
var uvz = _uvz;
return sampleAtlas(vec2f((2048.0 + (f32((k % 2)) * 1024.0)), (2048.0 + (f32((k / 2)) * 1024.0))), 1024.0, uvz);
}

fn spotShadowFactor(_k: i32, _n: vec3f, _l: vec3f, _dist: f32, _cosOuter: f32) -> f32 {
var k = _k;
var n = _n;
var l = _l;
var dist = _dist;
var cosOuter = _cosOuter;
var tanHalf: f32 = (sqrt(max((1.0 - (cosOuter * cosOuter)), 0.0)) / max(cosOuter, 0.05));
var texelWorld: f32 = ((((2.0 * dist) * tanHalf) * 1.1) / 1024.0);
var NoL: f32 = clamp(dot(n, l), 0.0, 1.0);
var slope: f32 = (sqrt((1.0 - (NoL * NoL))) / max(NoL, 0.05));
var pos: vec3f = (vWorldPos + ((n * texelWorld) * ((1.0 + slope))));
var sp: vec4f = (frame.spotViewProj[k] * vec4f(pos, 1.0));
if (sp.w <= 0.0) { return 1.0; }
var p: vec3f = (sp.xyz / vec3f(sp.w));
var uv: vec2f = ((p.xy * 0.5) + vec2f(0.5));
if (((((uv.x < 0.0) || (uv.x > 1.0)) || (uv.y < 0.0)) || (uv.y > 1.0)) || (p.z > 1.0)) { return 1.0; }
return sampleSpot(k, vec3f(uv, (p.z - 0.0005)));
}

fn pointFace(_d: vec3f) -> i32 {
var d = _d;
var a: vec3f = abs(d);
if ((a.x >= a.y) && (a.x >= a.z)) { return select(1, 0, (d.x > 0.0)); }
if (a.y >= a.z) { return select(3, 2, (d.y > 0.0)); }
return select(5, 4, (d.z > 0.0));
}

fn samplePoint(_slot: i32, _face: i32, _uvz: vec3f) -> f32 {
var slot = _slot;
var face = _face;
var uvz = _uvz;
var tile: i32 = ((slot * 6) + face);
var origin: vec2f = vec2f((f32((tile % 8)) * POINT_FACE), (4096.0 + (f32((tile / 8)) * POINT_FACE)));
var edge: f32 = (1.5 / POINT_FACE);
return sampleAtlas(origin, POINT_FACE, vec3f(clamp(uvz.xy, vec2f(edge), vec2f((1.0 - edge))), uvz.z));
}

fn pointShadowFactor(_slot: i32, _n: vec3f, _l: vec3f, _dist: f32) -> f32 {
var slot = _slot;
var n = _n;
var l = _l;
var dist = _dist;
var face: i32 = pointFace((-l));
var texelWorld: f32 = ((2.0 * dist) / POINT_FACE);
var NoL: f32 = clamp(dot(n, l), 0.0, 1.0);
var slope: f32 = (sqrt((1.0 - (NoL * NoL))) / max(NoL, 0.05));
var pos: vec3f = (vWorldPos + ((n * texelWorld) * ((1.0 + slope))));
var sp: vec4f = (frame.pointViewProj[((slot * 6) + face)] * vec4f(pos, 1.0));
if (sp.w <= 0.0) { return 1.0; }
var p: vec3f = (sp.xyz / vec3f(sp.w));
var uv: vec2f = ((p.xy * 0.5) + vec2f(0.5));
if (p.z > 1.0) { return 1.0; }
return samplePoint(slot, face, vec3f(uv, (p.z - 0.0015)));
}

fn D_GGX(_NoH: f32, _a2: f32) -> f32 {
var NoH = _NoH;
var a2 = _a2;
var d: f32 = (((NoH * NoH) * ((a2 - 1.0))) + 1.0);
return (a2 / (((PI * d) * d)));
}

fn V_SmithGGX(_NoV: f32, _NoL: f32, _a2: f32) -> f32 {
var NoV = _NoV;
var NoL = _NoL;
var a2 = _a2;
var gv: f32 = (NoL * sqrt((((NoV * NoV) * ((1.0 - a2))) + a2)));
var gl: f32 = (NoV * sqrt((((NoL * NoL) * ((1.0 - a2))) + a2)));
return (0.5 / max((gv + gl), 1e-5));
}

fn F_Schlick(_VoH: f32, _f0: vec3f) -> vec3f {
var VoH = _VoH;
var f0 = _f0;
return (f0 + (((vec3f(1.0) - f0)) * pow((1.0 - VoH), 5.0)));
}

fn D_GGXAniso(_NoH: f32, _ToH: f32, _BoH: f32, _at: f32, _ab: f32) -> f32 {
var NoH = _NoH;
var ToH = _ToH;
var BoH = _BoH;
var at = _at;
var ab = _ab;
var d: vec3f = vec3f((ab * ToH), (at * BoH), ((at * ab) * NoH));
var d2: f32 = max(dot(d, d), 1e-8);
var b2: f32 = ((at * ab) / d2);
return ((((at * ab) * b2) * b2) / PI);
}

fn V_SmithGGXAniso(_at: f32, _ab: f32, _ToV: f32, _BoV: f32, _ToL: f32, _BoL: f32, _NoV: f32, _NoL: f32) -> f32 {
var at = _at;
var ab = _ab;
var ToV = _ToV;
var BoV = _BoV;
var ToL = _ToL;
var BoL = _BoL;
var NoV = _NoV;
var NoL = _NoL;
var lv: f32 = (NoL * length(vec3f((at * ToV), (ab * BoV), NoV)));
var ll: f32 = (NoV * length(vec3f((at * ToL), (ab * BoL), NoL)));
return (0.5 / max((lv + ll), 1e-5));
}

fn thinFilm(_cosTheta: f32, _thickness: f32, _filmIOR: f32) -> vec3f {
var cosTheta = _cosTheta;
var thickness = _thickness;
var filmIOR = _filmIOR;
var eta: f32 = max(filmIOR, 1.0);
var sin2: f32 = (((1.0 - (cosTheta * cosTheta))) / ((eta * eta)));
var cosT: f32 = sqrt(max((1.0 - sin2), 0.0));
var opd: f32 = (((2.0 * eta) * thickness) * cosT);
var phase: vec3f = ((vec3f(((2.0 * PI) * opd)) / vec3f(650.0, 550.0, 450.0)) + vec3f(PI));
return (vec3f(0.5) + (0.5 * cos(phase)));
}

fn baseF0(_s: Surface) -> vec3f {
var s = _s;
return mix(((vec3f(0.04) * s.specularColor) * s.specular), s.albedo, vec3f(s.metallic));
}

fn iridescent(_s: Surface, _F: vec3f, _cosTheta: f32) -> vec3f {
var s = _s;
var F = _F;
var cosTheta = _cosTheta;
if (s.iridescence <= 0.0) { return F; }
return mix(F, ((F * 2.0) * thinFilm(cosTheta, s.iridescenceThickness, s.iridescenceIOR)), vec3f(s.iridescence));
}

fn D_Charlie(_NoH: f32, _roughness: f32) -> f32 {
var NoH = _NoH;
var roughness = _roughness;
var a: f32 = max(roughness, 0.05);
var invA: f32 = (1.0 / a);
var sin2: f32 = (1.0 - (NoH * NoH));
return ((((2.0 + invA)) * pow(sin2, (invA * 0.5))) / ((2.0 * PI)));
}

fn V_Neubelt(_NoV: f32, _NoL: f32) -> f32 {
var NoV = _NoV;
var NoL = _NoL;
return (1.0 / ((4.0 * max(((NoL + NoV) - (NoL * NoV)), 1e-4))));
}

fn shade(_n: vec3f, _v: vec3f, _l: vec3f, _radiance: vec3f, _albedo: vec3f, _metallic: f32, _roughness: f32) -> vec3f {
var n = _n;
var v = _v;
var l = _l;
var radiance = _radiance;
var albedo = _albedo;
var metallic = _metallic;
var roughness = _roughness;
var h: vec3f = normalize((l + v));
var NoL: f32 = max(dot(n, l), 0.0);
var NoV: f32 = max(dot(n, v), 1e-4);
var NoH: f32 = max(dot(n, h), 0.0);
var VoH: f32 = max(dot(v, h), 0.0);
var a: f32 = (roughness * roughness);
var a2: f32 = (a * a);
var f0: vec3f = mix(vec3f(0.04), albedo, vec3f(metallic));
var F: vec3f = F_Schlick(VoH, f0);
var spec: vec3f = ((D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2)) * F);
var kd: vec3f = (((vec3f(1.0) - F)) * ((1.0 - metallic)));
return ((((((kd * albedo) / vec3f(PI)) + spec)) * radiance) * NoL);
}

fn shadeVolume(_n: vec3f, _v: vec3f, _l: vec3f, _radiance: vec3f, _albedo: vec3f, _metallic: f32, _roughness: f32, _transmission: f32) -> vec3f {
var n = _n;
var v = _v;
var l = _l;
var radiance = _radiance;
var albedo = _albedo;
var metallic = _metallic;
var roughness = _roughness;
var transmission = _transmission;
var h: vec3f = normalize((l + v));
var NoL: f32 = max(dot(n, l), 0.0);
var NoV: f32 = max(dot(n, v), 1e-4);
var NoH: f32 = max(dot(n, h), 0.0);
var VoH: f32 = max(dot(v, h), 0.0);
var a: f32 = (roughness * roughness);
var a2: f32 = (a * a);
var f0: vec3f = mix(vec3f(0.04), albedo, vec3f(metallic));
var F: vec3f = F_Schlick(VoH, f0);
var spec: vec3f = ((D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2)) * F);
var kd: vec3f = ((((vec3f(1.0) - F)) * ((1.0 - metallic))) * ((1.0 - transmission)));
return ((((((kd * albedo) / vec3f(PI)) + spec)) * radiance) * NoL);
}

fn baseLayer(_s: Surface, _n: vec3f, _v: vec3f, _l: vec3f, _radiance: vec3f) -> vec3f {
var s = _s;
var n = _n;
var v = _v;
var l = _l;
var radiance = _radiance;
var h: vec3f = normalize((l + v));
var NoL: f32 = max(dot(n, l), 0.0);
var NoV: f32 = max(dot(n, v), 1e-4);
var NoH: f32 = max(dot(n, h), 0.0);
var VoH: f32 = max(dot(v, h), 0.0);
var a: f32 = (s.roughness * s.roughness);
var a2: f32 = (a * a);
var F: vec3f = iridescent(s, F_Schlick(VoH, baseF0(s)), VoH);
var D: f32 = D_GGX(NoH, a2);
var V: f32 = V_SmithGGX(NoV, NoL, a2);
if (s.anisotropy != 0.0) {
var at: f32 = max((a * ((1.0 + s.anisotropy))), 1e-3);
var ab: f32 = max((a * ((1.0 - s.anisotropy))), 1e-3);
var t: vec3f = normalize((s.tangent - (n * dot(n, s.tangent))));
var b: vec3f = cross(n, t);
D = D_GGXAniso(NoH, dot(t, h), dot(b, h), at, ab);
V = V_SmithGGXAniso(at, ab, dot(t, v), dot(b, v), dot(t, l), dot(b, l), NoV, NoL);
}
var spec: vec3f = ((D * V) * F);
var kd: vec3f = ((((vec3f(1.0) - F)) * ((1.0 - s.metallic))) * ((1.0 - s.transmission)));
return ((((((kd * s.albedo) / vec3f(PI)) + spec)) * radiance) * NoL);
}

fn transmitted(_s: Surface, _n: vec3f, _v: vec3f) -> vec3f {
var s = _s;
var n = _n;
var v = _v;
var dir: vec3f = refract((-v), n, (1.0 / max(s.ior, 1.0)));
if (dot(dir, dir) < 1e-6) { dir = (-v); }
var clip: vec4f = (frame.viewProj * vec4f((s.worldPos + (dir * s.volume)), 1.0));
var uv: vec2f = (((clip.xy / vec2f(max(clip.w, 1e-4))) * 0.5) + vec2f(0.5));
var levels: f32 = f32((textureNumLevels(tScene) - 1));
var color: vec3f = textureSampleLevel(tScene, materialSampler1, clamp(uv, vec2f(0.0), vec2f(1.0)), ((s.roughness * levels) * 0.7)).rgb;
if (s.attenuationDistance > 0.0) {
var sigma: vec3f = ((-log(max(s.attenuation, vec3f(1e-3)))) / vec3f(s.attenuationDistance));
color *= exp(((-sigma) * s.volume));
}
return (color * s.albedo);
}

fn lobes(_s: Surface, _n: vec3f, _v: vec3f, _l: vec3f, _radiance: vec3f) -> vec3f {
var s = _s;
var n = _n;
var v = _v;
var l = _l;
var radiance = _radiance;
var color: vec3f = baseLayer(s, n, v, l, radiance);
var h: vec3f = normalize((l + v));
var NoL: f32 = max(dot(n, l), 0.0);
var NoV: f32 = max(dot(n, v), 1e-4);
var NoH: f32 = max(dot(n, h), 0.0);
var VoH: f32 = max(dot(v, h), 0.0);
if (dot(s.sheen, s.sheen) > 0.0) {
color += ((((s.sheen * D_Charlie(NoH, s.sheenRoughness)) * V_Neubelt(NoV, NoL)) * radiance) * NoL);
}
if (s.clearcoat > 0.0) {
var a: f32 = max(s.clearcoatRoughness, 0.03);
var a2: f32 = (((a * a) * a) * a);
var Fc: vec3f = (F_Schlick(VoH, vec3f(0.04)) * s.clearcoat);
var coat: vec3f = ((D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2)) * Fc);
color = ((color * ((vec3f(1.0) - Fc))) + ((coat * radiance) * NoL));
}
if (s.subsurface > 0.0) {
var through: vec3f = normalize((l + (n * 0.3)));
var back: f32 = pow(max(dot(v, (-through)), 0.0), 3.0);
color += ((((s.albedo * radiance) * ((back + 0.15))) * ((1.0 - s.thickness))) * s.subsurface);
}
return color;
}

fn clusterAt() -> u32 {

var tile: vec2u = vec2u((max(gl_FragCoord.xy, vec2f(0.0)) / max(frame.cluster.xy, vec2f(1.0))));
tile = min(tile, vec2u((CLUSTER_X - 1u), (CLUSTER_Y - 1u)));
var slice: f32 = ((log2(max(vViewDepth, 1e-4)) * frame.cluster.z) + frame.cluster.w);
var z: u32 = u32(clamp(slice, 0.0, f32((CLUSTER_Z - 1u))));
return ((tile.x + (tile.y * CLUSTER_X)) + ((z * CLUSTER_X) * CLUSTER_Y));
}

fn light(_s: Surface) -> vec3f {
var s = _s;
var n: vec3f = normalize(s.normal);
var v: vec3f = s.viewDir;
var l: vec3f = normalize((-frame.lightDir.xyz));
var shadow: f32 = mix(1.0, shadowFactor(n, l), frame.lightColor.w);
var color: vec3f = lobes(s, n, v, l, (frame.lightColor.rgb * shadow));
var cell: vec2u = clusterCells[clusterAt()];
for (var c: u32 = 0u; (c < cell.y); c++) {
var ld: LightData = lights[lightIndex[(cell.x + c)]];
var d: vec3f = (ld.posRange.xyz - s.worldPos);
var dist: f32 = length(d);
var range: f32 = max(ld.posRange.w, 1e-3);
var att: f32 = clamp((1.0 - (((dist * dist)) / ((range * range)))), 0.0, 1.0);
att *= (att / max((dist * dist), 1e-3));
var cone: f32 = ld.dir.w;
if (cone > (-1.5)) {
var cd: f32 = dot(((-d) / vec3f(dist)), ld.dir.xyz);
att *= smoothstep(cone, max(ld.color.w, (cone + 1e-3)), cd);
var k: i32 = i32(ld.info.x);
if ((k >= 0) && (att > 0.0)) { att *= spotShadowFactor(k, n, (d / vec3f(dist)), dist, cone); }
} else {
var slot: i32 = i32(ld.info.y);
if ((slot >= 0) && (att > 0.0)) { att *= pointShadowFactor(slot, n, (d / vec3f(dist)), dist); }
}
color += lobes(s, n, v, (d / vec3f(dist)), (ld.color.rgb * att));
}
return (color + vec3f((ambient(s, n, v) * s.occlusion)));
}

// ATMOSPHERE MAPPING. Everything between this line and END ATMOSPHERE
// MAPPING is the same text in atmoslut.frag.wgsl, which builds the
// atmosphere's lookup tables, and in prelude_mesh.wgsl and
// skyparam.frag.wgsl, which read them: where a table keeps a ray has to
// be the same on both sides. TestAtmosphereBlocksMatch compares the
// three, and TestAtmosphereTablesMatchGo checks the tables against
// Sky.scatter in gfx/sky.go.
const ATMOS_PI: f32 = 3.14159265359;
// The transmittance table: the optical depth from a point to the top of
// the air, by the ray's angle across and the point's height down, both
// on Bruneton's mapping, which spends the texels near the ground and the
// horizon. Red and green are the four-step integrals of air and haze,
// blue and alpha the eight-step ones, each over its falloff height.
const ATMOS_TRANS_W: f32 = 256.0;
const ATMOS_TRANS_H: f32 = 64.0;
// The sky view: the light scattered towards the camera from every
// direction, without the phase functions, which the reader applies. The
// azimuth from the sun runs across, from towards it to away from it,
// since the sky is the same either side of the sun. Down each block
// runs the angle from the zenith, the sky above the ground's horizon in
// the top half and the ground below it in the bottom half, each on a
// square-root mapping that spends the texels near the horizon. Air is
// the top block and haze the bottom one.
const ATMOS_SKY_W: f32 = 128.0;
const ATMOS_SKY_H: f32 = 128.0;
// The aerial perspective: the same view mapping at ATMOS_AERIAL_D
// distances side by side, each a fraction of the way along the ray's
// run through the air (atmosSpan), closer together near the camera and
// near the ray's end, with the optical depth back to the camera in
// alpha.
const ATMOS_AERIAL_W: f32 = 32.0;
const ATMOS_AERIAL_H: f32 = 64.0;
const ATMOS_AERIAL_D: f32 = 32.0;

// raySphere returns where a ray from o along d crosses a sphere of
// radius r about the origin, as two distances along the ray. x is
// greater than y when the ray misses.
fn raySphere(o: vec3f, d: vec3f, r: f32) -> vec2f {
    var b: f32 = dot(o, d);
    var c: f32 = dot(o, o) - r * r;
    var h: f32 = b * b - c;
    if (h < 0.0) { return vec2f(1.0, -1.0); }
    h = sqrt(h);
    return vec2f(-b - h, -b + h);
}

// phaseRayleigh is how much air scatters towards an angle whose cosine
// is mu: nearly even, a little more forwards and backwards.
fn phaseRayleigh(mu: f32) -> f32 { return 3.0 / (16.0 * ATMOS_PI) * (1.0 + mu * mu); }

// phaseMie is the Henyey-Greenstein lobe haze scatters into, forwards
// by g, which is the glare around the sun.
fn phaseMie(mu: f32, g: f32) -> f32 {
    var g2: f32 = g * g;
    var ik: f32 = inverseSqrt(1.0 + g2 - 2.0 * g * mu);
    return 3.0 / (8.0 * ATMOS_PI) * (1.0 - g2) / (2.0 + g2) * (1.0 + mu * mu) * ik * ik * ik;
}

// atmosTexel places x, from 0 to 1, between the centres of the first
// and last of n texels, in texels.
fn atmosTexel(x: f32, n: f32) -> f32 { return clamp(x, 0.0, 1.0) * (n - 1.0) + 0.5; }

// atmosUnit is the inverse of atmosTexel.
fn atmosUnit(t: f32, n: f32) -> f32 { return clamp((t - 0.5) / (n - 1.0), 0.0, 1.0); }

// atmosTransCoord is where the transmittance table keeps a ray from
// radius r whose cosine with the zenith is mu, in texels. The ray must
// not meet the ground.
fn atmosTransCoord(r: f32, mu: f32, radius: f32, top: f32) -> vec2f {
    var span: f32 = sqrt(top * top - radius * radius);
    var rho: f32 = sqrt(max(r * r - radius * radius, 0.0));
    var d: f32 = max(-r * mu + sqrt(max(r * r * (mu * mu - 1.0) + top * top, 0.0)), 0.0);
    var dMin: f32 = top - r;
    var dMax: f32 = rho + span;
    return vec2f(atmosTexel((d - dMin) / max(dMax - dMin, 1e-6), ATMOS_TRANS_W), atmosTexel(rho / span, ATMOS_TRANS_H));
}

// atmosTransRay is the inverse of atmosTransCoord: the radius and the
// cosine with the zenith a texel stands for.
fn atmosTransRay(t: vec2f, radius: f32, top: f32) -> vec2f {
    var span: f32 = sqrt(top * top - radius * radius);
    var rho: f32 = span * atmosUnit(t.y, ATMOS_TRANS_H);
    var r: f32 = sqrt(rho * rho + radius * radius);
    var dMin: f32 = top - r;
    var dMax: f32 = rho + span;
    var d: f32 = dMin + atmosUnit(t.x, ATMOS_TRANS_W) * (dMax - dMin);
    var mu: f32 = 1.0;
    if (d > 0.0) { mu = clamp((span * span - rho * rho - d * d) / (2.0 * r * d), -1.0, 1.0); }
    return vec2f(r, mu);
}

// The view tables measure a direction's angle from the zenith by the
// tangent of half of it, which needs no trigonometry to find or invert:
// it is 0 at the zenith, 1 at the horizontal and grows without bound
// towards the nadir. Below the horizon they use its reciprocal, which
// is 0 at the nadir. Across, they measure the cosine of the azimuth
// from the sun: the light the tables hold leaves out the phase
// functions, so it changes slowly towards the sun and away from it.

// atmosHorizon describes the view from radius r for the view tables,
// as tangents of half the angle from the zenith: x of the first ray
// that reaches the air, 0 inside it, and y of the ray that grazes the
// ground. z is 1 / (y - x). From space the sky half of a table spans
// only the limb, between x and y.
fn atmosHorizon(r: f32, radius: f32, top: f32) -> vec3f {
    var rr: f32 = max(r, radius);
    var first: f32 = 0.0;
    if (rr > top) { first = (rr + sqrt(rr * rr - top * top)) / top; }
    var grazing: f32 = (rr + sqrt(max(rr * rr - radius * radius, 0.0))) / radius;
    return vec3f(first, grazing, 1.0 / (grazing - first));
}

// atmosSunSide is the horizontal direction towards the sun, which the
// view tables measure azimuth from. With the sun at the zenith every
// azimuth is the same and any horizontal direction does.
fn atmosSunSide(up: vec3f, sun: vec3f) -> vec3f {
    var side: vec3f = sun - up * dot(sun, up);
    if (dot(side, side) < 1e-12) {
        side = cross(up, select(vec3f(1.0, 0.0, 0.0), vec3f(0.0, 0.0, 1.0), abs(up.x) > 0.9));
    }
    return normalize(side);
}

// atmosViewCoord is where a view table w texels across and h down keeps
// direction d, in texels. It is in the bottom half, and meets the
// ground, when y is more than h / 2.
fn atmosViewCoord(d: vec3f, up: vec3f, side: vec3f, horizon: vec3f, w: f32, h: f32) -> vec2f {
    var cosZ: f32 = clamp(dot(d, up), -1.0, 1.0);
    // One over the sine of the angle from the zenith gives the tangent
    // of half of it, (1 - cos) / sin, and its reciprocal, (1 + cos) / sin.
    // side is level, so its dot with d is its dot with d's level part.
    var inv: f32 = inverseSqrt(max(1.0 - cosZ * cosZ, 1e-12));
    var cosA: f32 = clamp(dot(d, side) * inv, -1.0, 1.0);
    var t: f32 = (1.0 - cosZ) * inv;
    var rows: f32 = h * 0.5;
    var y: f32;
    if (t < horizon.y) {
        y = atmosTexel(1.0 - sqrt(clamp((horizon.y - t) * horizon.z, 0.0, 1.0)), rows);
    } else {
        y = rows + atmosTexel(sqrt(clamp(1.0 - (1.0 + cosZ) * inv * horizon.y, 0.0, 1.0)), rows);
    }
    return vec2f(atmosTexel(0.5 - 0.5 * cosA, w), y);
}

// atmosViewDir is the inverse of atmosViewCoord: the direction a texel
// stands for.
fn atmosViewDir(t: vec2f, up: vec3f, side: vec3f, horizon: vec3f, w: f32, h: f32) -> vec3f {
    var rows: f32 = h * 0.5;
    var cosZ: f32;
    var sinZ: f32;
    if (t.y < rows) {
        var c: f32 = 1.0 - atmosUnit(t.y, rows);
        var p: f32 = horizon.y - (horizon.y - horizon.x) * c * c;
        cosZ = (1.0 - p * p) / (1.0 + p * p);
        sinZ = 2.0 * p / (1.0 + p * p);
    } else {
        var c: f32 = atmosUnit(t.y - rows, rows);
        var q: f32 = (1.0 - c * c) / horizon.y;
        cosZ = -(1.0 - q * q) / (1.0 + q * q);
        sinZ = 2.0 * q / (1.0 + q * q);
    }
    var cosA: f32 = 1.0 - 2.0 * atmosUnit(t.x, w);
    var sinA: f32 = sqrt(max(1.0 - cosA * cosA, 0.0));
    return up * cosZ + (side * cosA + cross(up, side) * sinA) * sinZ;
}

// atmosSpan is where a view ray from radius r whose cosine with the
// zenith is cosZ enters the air, and how far it runs through it before
// it leaves or, with ground set, meets the ground: the stretch the
// aerial perspective table's distances divide. A ray meets the ground
// when it starts above it and lies in the bottom half of a view table,
// so a table and its reader agree about a ray that grazes the ground.
fn atmosSpan(r: f32, cosZ: f32, radius: f32, top: f32, ground: bool) -> vec2f {
    var b: f32 = r * cosZ;
    var h: f32 = b * b - (r * r - top * top);
    if (h < 0.0) { return vec2f(0.0); }
    h = sqrt(h);
    var t0: f32 = max(-b - h, 0.0);
    var t1: f32 = -b + h;
    if (ground && r > radius) { t1 = min(t1, -b - sqrt(max(b * b - (r * r - radius * radius), 0.0))); }
    return vec2f(t0, max(t1 - t0, 0.0));
}

// atmosSliceFraction is how far along the span the aerial perspective
// table's slice u, from 0 to 1, lies: quadratic from each end, so the
// slices crowd near the camera and near the ray's end. atmosSliceOf is
// its inverse.
fn atmosSliceFraction(u: f32) -> f32 {
    if (u < 0.5) { return 2.0 * u * u; }
    return 1.0 - 2.0 * (1.0 - u) * (1.0 - u);
}
fn atmosSliceOf(x: f32) -> f32 {
    if (x < 0.5) { return sqrt(0.5 * x); }
    return 1.0 - sqrt(0.5 - 0.5 * x);
}

// The reflection table: skyColor for every direction, ATMOS_REFLECT_N
// texels square on an octahedral mapping about the zenith with the
// sun's side along its first axis. The height above or below the
// horizon is warped to its square root first, which crowds the texels
// towards the horizon, where the sky changes fastest. The lit meshes'
// reflections read it in one lookup and no trigonometry.
const ATMOS_REFLECT_N: f32 = 256.0;

// atmosOctCoord is where the reflection table keeps direction d, as a
// texture coordinate. The warped direction is still of unit length: its
// level part shrinks by 1 / sqrt(1 + |z|) as its height z grows to
// sqrt(|z|).
fn atmosOctCoord(d: vec3f, up: vec3f, side: vec3f) -> vec2f {
    var z: f32 = dot(d, up);
    var a: f32 = abs(z);
    var level: vec2f = vec2f(dot(d, side), dot(d, cross(up, side))) * inverseSqrt(1.0 + a);
    var l: vec3f = vec3f(level, sign(z) * sqrt(a));
    var p: vec2f = l.xy / (abs(l.x) + abs(l.y) + abs(l.z));
    if (l.z < 0.0) {
        p = (vec2f(1.0) - abs(p.yx)) * select(vec2f(-1.0), vec2f(1.0), p >= vec2f(0.0));
    }
    return p * 0.5 + vec2f(0.5);
}

// atmosOctDir is the inverse of atmosOctCoord: the direction a texture
// coordinate stands for. The fold below the horizon is its own inverse.
fn atmosOctDir(uv: vec2f, up: vec3f, side: vec3f) -> vec3f {
    var p: vec2f = uv * 2.0 - vec2f(1.0);
    var w: f32 = 1.0 - abs(p.x) - abs(p.y);
    if (w < 0.0) {
        p = (vec2f(1.0) - abs(p.yx)) * select(vec2f(-1.0), vec2f(1.0), p >= vec2f(0.0));
    }
    var l: vec3f = normalize(vec3f(p, w));
    var z: f32 = sign(l.z) * l.z * l.z;
    var level: vec2f = l.xy * sqrt(1.0 + abs(z));
    return side * level.x + cross(up, side) * level.y + up * z;
}
// END ATMOSPHERE MAPPING.

// ATMOSPHERE LOOKUP. Everything between this line and END ATMOSPHERE
// LOOKUP is the same text in prelude_mesh.wgsl and skyparam.frag.wgsl:
// the sky and the lit meshes read the atmosphere from the tables
// atmoslut.frag.wgsl builds, and Sky.radiance in gfx/sky.go projects
// the ambient harmonics from the same model, so they have to agree.
// TestAtmosphereBlocksMatch compares the two shaders and
// TestAtmosphereMatchesGo the drawn sky with the Go side.

// ScatterResult is the light a stretch of air scatters towards the
// camera, and how much of the light from beyond it survives.
struct ScatterResult { radiance: vec3f, transmittance: vec3f }

// atmosTransmittance is how much light survives from radius r along a
// ray whose cosine with the zenith is mu to the top of the air, from
// the transmittance table's eight-step integral: how the air dims the
// view of space.
fn atmosTransmittance(r: f32, mu: f32) -> vec3f {
    var c: vec2f = atmosTransCoord(r, mu, frame.atmos.x, frame.atmos.x + frame.atmos.y);
    var od: vec4f = textureSampleLevel(atmosTransTable, atmosSampler, c / vec2f(ATMOS_TRANS_W, ATMOS_TRANS_H), 0.0);
    var depth: vec2f = od.zw * frame.atmos.zw;
    return exp(-(frame.betaR.rgb * depth.x + frame.betaM.x * 1.1 * depth.y));
}

// atmosSkyLight is the sunlight the whole air scatters towards the
// camera from direction d, from the sky view table. frame.atmosView and
// frame.atmosLimb hold what atmosSunSide and atmosHorizon give for this
// frame's camera, found once on the processor.
fn atmosSkyLight(d: vec3f) -> vec3f {
    var c: vec2f = atmosViewCoord(d, frame.skyUp.xyz, frame.atmosView.xyz, frame.atmosLimb.xyz, ATMOS_SKY_W, ATMOS_SKY_H);
    var scale: vec2f = vec2f(1.0 / ATMOS_SKY_W, 0.5 / ATMOS_SKY_H);
    var ray: vec3f = textureSampleLevel(atmosSkyTable, atmosSampler, c * scale, 0.0).rgb;
    var mie: vec3f = textureSampleLevel(atmosSkyTable, atmosSampler, (c + vec2f(0.0, ATMOS_SKY_H)) * scale, 0.0).rgb;
    var mu: f32 = dot(d, frame.sun.xyz);
    return frame.betaR.w * (ray * phaseRayleigh(mu) + mie * phaseMie(mu, frame.betaM.y));
}

// atmosphereScatter is the light the first dist world units of air
// along direction d scatter towards the camera, and how much of the
// light from beyond them survives: the aerial perspective, from the
// table's two nearest distances.
fn atmosphereScatter(d: vec3f, dist: f32) -> ScatterResult {
    var radius: f32 = frame.atmos.x;
    var up: vec3f = frame.skyUp.xyz;
    var c: vec2f = atmosViewCoord(d, up, frame.atmosView.xyz, frame.atmosLimb.xyz, ATMOS_AERIAL_W, ATMOS_AERIAL_H);
    var span: vec2f = atmosSpan(frame.atmosView.w, dot(d, up), radius, radius + frame.atmos.y, c.y > ATMOS_AERIAL_H * 0.5);
    if (span.y <= 0.0) { return ScatterResult(vec3f(0.0), vec3f(1.0)); }
    // The two slices either side, blended by distance rather than by
    // slice, since the light a short stretch of air scatters grows with
    // its length.
    var x: f32 = clamp(dist / span.y, 0.0, 1.0);
    var k0: f32 = min(floor(atmosSliceOf(x) * (ATMOS_AERIAL_D - 1.0)), ATMOS_AERIAL_D - 2.0);
    var x0: f32 = atmosSliceFraction(k0 / (ATMOS_AERIAL_D - 1.0));
    var x1: f32 = atmosSliceFraction((k0 + 1.0) / (ATMOS_AERIAL_D - 1.0));
    var f: f32 = clamp((x - x0) / (x1 - x0), 0.0, 1.0);
    var scale: vec2f = vec2f(1.0 / (ATMOS_AERIAL_W * ATMOS_AERIAL_D), 0.5 / ATMOS_AERIAL_H);
    var a: vec2f = (c + vec2f(k0 * ATMOS_AERIAL_W, 0.0)) * scale;
    var b: vec2f = a + vec2f(ATMOS_AERIAL_W, 0.0) * scale;
    var m: vec2f = vec2f(0.0, 0.5);
    var ray: vec4f = mix(textureSampleLevel(atmosAerialTable, atmosSampler, a, 0.0),
        textureSampleLevel(atmosAerialTable, atmosSampler, b, 0.0), f);
    var mie: vec4f = mix(textureSampleLevel(atmosAerialTable, atmosSampler, a + m, 0.0),
        textureSampleLevel(atmosAerialTable, atmosSampler, b + m, 0.0), f);
    var mu: f32 = dot(d, frame.sun.xyz);
    var light: vec3f = frame.betaR.w * (ray.rgb * phaseRayleigh(mu) + mie.rgb * phaseMie(mu, frame.betaM.y));
    var through: vec3f = exp(-(frame.betaR.rgb * ray.a * frame.atmos.z + frame.betaM.x * 1.1 * mie.a * frame.atmos.w));
    return ScatterResult(light, through);
}

// skyColor is the sky's light from a direction without the sun's disc:
// the atmosphere when the light's Sky has one, otherwise the gradient
// above the horizon, and the ground or planet below. Below the horizon a
// camera inside the air looks the colour up along the horizon instead,
// because its own ray meets the ground at once and would leave a dark
// band; from above the air the ray itself is looked up, so a planet
// seen from orbit keeps the glow around its limb.
fn skyColor(d: vec3f) -> vec3f {
    var up: f32 = dot(d, frame.skyUp.xyz);
    var air: f32 = frame.horizon.w;
    if (frame.betaM.w > 0.5) {
        var dir: vec3f = d;
        if (up < 0.0 && frame.betaM.z < frame.atmos.y) {
            var side: vec3f = d - frame.skyUp.xyz * up;
            var len: f32 = length(side);
            if (len > 1e-4) { dir = side / len; }
        }
        var c: vec3f = atmosSkyLight(dir) * air;
        if (up < 0.0) { c = mix(c, frame.ground.rgb, pow(-up, 0.5)); }
        return c;
    }
    return skyGradient(d);
}

// skyGradient is skyColor without an atmosphere: the sky's gradient
// above the horizon and the ground or planet below.
fn skyGradient(d: vec3f) -> vec3f {
    var up: f32 = dot(d, frame.skyUp.xyz);
    var air: f32 = frame.horizon.w;
    var above: vec3f = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    var below: vec3f = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    return select(below, above, up >= 0.0);
}

// atmosSkyReflection is skyColor with an atmosphere, from the reflection
// table, for the lit meshes' reflections. The table leaves out the
// haze's phase and the ground's blend below the horizon, the two things
// that change too fast for its texels, and this puts them back.
fn atmosSkyReflection(d: vec3f) -> vec3f {
    var uv: vec2f = atmosOctCoord(d, frame.skyUp.xyz, frame.atmosView.xyz);
    var c: vec3f = textureSampleLevel(atmosReflectTable, atmosSampler, uv, 0.0).rgb * phaseMie(dot(d, frame.sun.xyz), frame.betaM.y);
    var up: f32 = dot(d, frame.skyUp.xyz);
    if (up < 0.0) { c = mix(c, frame.ground.rgb, sqrt(-up)); }
    return c;
}

// spaceTransmittance attenuates distant radiance with the atmosphere and
// hides it behind the solid planet. It matches Sky.spaceTransmittance in Go.
fn spaceTransmittance(d: vec3f) -> vec3f {
    if (frame.betaM.w < 0.5) {
        return vec3f(select(1.0 - frame.horizon.w, 1.0, dot(d, frame.skyUp.xyz) >= 0.0));
    }
    let origin = frame.skyUp.xyz * (frame.atmos.x + frame.betaM.z);
    let ground = raySphere(origin, d, frame.atmos.x);
    if (ground.y > 0.0 && ground.x >= 0.0) { return vec3f(0.0); }
    let shell = raySphere(origin, d, frame.atmos.x + frame.atmos.y);
    let start = max(shell.x, 0.0);
    if (shell.y <= start) { return vec3f(1.0); }
    let p = origin + d * start;
    let r = max(length(p), frame.atmos.x);
    return mix(vec3f(1.0), atmosTransmittance(r, dot(p, d) / r), vec3f(frame.horizon.w));
}
// END ATMOSPHERE LOOKUP.

fn aerialPerspective(_c: vec3f, _worldPos: vec3f) -> vec3f {
var c = _c;
var worldPos = _worldPos;
var d: vec3f = (worldPos - frame.camPos.xyz);
var dist: f32 = length(d);
if (dist < 1e-4) { return c; }
let scatter = atmosphereScatter(d / dist, dist);
return mix(c, c * scatter.transmittance + scatter.radiance, vec3f(frame.horizon.w));
}

fn applyFog(_c: vec3f, _worldPos: vec3f, _depth: f32) -> vec3f {
var c = _c;
var worldPos = _worldPos;
var depth = _depth;
var f: f32 = 0.0;
if (frame.fogRange.y > frame.fogRange.x) {
f = clamp((((depth - frame.fogRange.x)) / ((frame.fogRange.y - frame.fogRange.x))), 0.0, 1.0);
}
if (frame.fog.w > 0.0) {
var e: f32 = (depth * frame.fog.w);
f = max(f, (1.0 - exp(((-e) * e))));
}
if (frame.fogRange.w > 0.0) {
f *= clamp(exp(((-((worldPos.y - frame.fogRange.z))) * frame.fogRange.w)), 0.0, 1.0);
}
c = mix(c, frame.fog.rgb, vec3f(f));
if (frame.betaM.w > 0.5) { c = aerialPerspective(c, worldPos); }
return c;
}

fn irradiance(_n: vec3f) -> vec3f {
var n = _n;
var x: f32 = n.x;
var y: f32 = n.y;
var z: f32 = n.z;
return (((((((((frame.sh[0].rgb * 0.282095) + (frame.sh[1].rgb * ((0.488603 * y)))) + (frame.sh[2].rgb * ((0.488603 * z)))) + (frame.sh[3].rgb * ((0.488603 * x)))) + (frame.sh[4].rgb * (((1.092548 * x) * y)))) + (frame.sh[5].rgb * (((1.092548 * y) * z)))) + (frame.sh[6].rgb * ((0.315392 * ((((3.0 * z) * z) - 1.0)))))) + (frame.sh[7].rgb * (((1.092548 * x) * z)))) + (frame.sh[8].rgb * ((0.546274 * (((x * x) - (y * y)))))));
}

fn envBRDF(_f0: vec3f, _roughness: f32, _NoV: f32) -> vec3f {
var f0 = _f0;
var roughness = _roughness;
var NoV = _NoV;
let c0 = vec4f(-1.0, -0.0275, -0.572, 0.022);
let c1 = vec4f(1.0, 0.0425, 1.04, -0.04);
var r: vec4f = ((roughness * c0) + c1);
var a004: f32 = ((min((r.x * r.x), exp2(((-9.28) * NoV))) * r.x) + r.y);
var ab: vec2f = ((vec2f((-1.04), 1.04) * a004) + r.zw);
return ((f0 * ab.x) + vec3f(ab.y));
}

fn skyRadiance(_d: vec3f, _roughness: f32) -> vec3f {
var d = _d;
var roughness = _roughness;
// The atmosphere's reflection comes from its reflection table, which
// holds skyColor for every direction; otherwise the gradient.
var radiance: vec3f;
if (frame.betaM.w > 0.5) {
    radiance = atmosSkyReflection(d);
} else {
    radiance = skyGradient(d);
}
if (frame.env.w != 0.0) {
    radiance += textureSampleLevel(tEnv, materialSampler1, d, roughness * (frame.env.y - 1.0)).rgb * frame.env.w * spaceTransmittance(d);
}
return mix(radiance, (frame.sh[0].rgb * 0.282095), vec3f((roughness * 0.8)));
}

fn probeIndex() -> i32 {

return (i32(vGI.x) - 1);
}

fn boxProject(_dir: vec3f, _pos: vec3f, _i: i32) -> vec3f {
var dir = _dir;
var pos = _pos;
var i = _i;
var inv: vec3f = (vec3f(1.0) / dir);
var t1: vec3f = (((frame.probeMax[i].xyz - pos)) * inv);
var t2: vec3f = (((frame.probeMin[i].xyz - pos)) * inv);
var tmax: vec3f = max(t1, t2);
var t: f32 = min(min(tmax.x, tmax.y), tmax.z);
return (((pos + (dir * max(t, 0.0)))) - frame.probePos[i].xyz);
}

fn sphereProject(_dir: vec3f, _pos: vec3f, _i: i32) -> vec3f {
var dir = _dir;
var pos = _pos;
var i = _i;
var c: vec3f = frame.probePos[i].xyz;
var radius: f32 = frame.probeMin[i].w;
var d: vec3f = (pos - c);
var b: f32 = dot(d, dir);
var q: f32 = (dot(d, d) - (radius * radius));
var t: f32 = ((-b) + sqrt(max(((b * b) - q), 0.0)));
return (((pos + (dir * max(t, 0.0)))) - c);
}

fn probeFade(_pos: vec3f, _i: i32) -> f32 {
var pos = _pos;
var i = _i;
var margin: f32 = frame.probeMax[i].w;
if (margin <= 0.0) { return 1.0; }
var depth: f32;
if (frame.probePos[i].w > 1.5) {
depth = (frame.probeMin[i].w - distance(pos, frame.probePos[i].xyz));
} else {
var d: vec3f = min((pos - frame.probeMin[i].xyz), (frame.probeMax[i].xyz - pos));
depth = min(min(d.x, d.y), d.z);
}
return clamp((depth / margin), 0.0, 1.0);
}

fn probeSpecular(_r: vec3f, _roughness: f32, _pos: vec3f, _i: i32) -> vec3f {
var r = _r;
var roughness = _roughness;
var pos = _pos;
var i = _i;
var dir: vec3f = r;
if (frame.probePos[i].w > 1.5) {
dir = sphereProject(r, pos, i);
} else if (frame.probeParams[i].z > 0.5) {
dir = boxProject(r, pos, i);
}
return (textureSampleLevel(tEnv, materialSampler1, dir, (roughness * ((frame.probeParams[i].y - 1.0)))).rgb * frame.probeParams[i].x);
}

fn envSpecular(_r: vec3f, _roughness: f32) -> vec3f {
var r = _r;
var roughness = _roughness;
var i: i32 = probeIndex();
if (i >= 0) {
var probe: vec3f = probeSpecular(r, roughness, vWorldPos, i);
var fade: f32 = probeFade(vWorldPos, i);
if (fade >= 1.0) { return probe; }
return mix(((frame.sh[0].rgb * 0.282095) * frame.env.x), probe, vec3f(fade));
}
if (frame.env.z > 1.5) { return skyRadiance(r, roughness); }
return (textureSampleLevel(tEnv, materialSampler1, r, (roughness * ((frame.env.y - 1.0)))).rgb * frame.env.x);
}

fn cellIrradiance(_base: i32, _n: vec3f) -> vec3f {
var base = _base;
var n = _n;
var x: f32 = n.x;
var y: f32 = n.y;
var z: f32 = n.z;
return (((((((((probeGrid.cells[(base + 0)].rgb * 0.282095) + (probeGrid.cells[(base + 1)].rgb * ((0.488603 * y)))) + (probeGrid.cells[(base + 2)].rgb * ((0.488603 * z)))) + (probeGrid.cells[(base + 3)].rgb * ((0.488603 * x)))) + (probeGrid.cells[(base + 4)].rgb * (((1.092548 * x) * y)))) + (probeGrid.cells[(base + 5)].rgb * (((1.092548 * y) * z)))) + (probeGrid.cells[(base + 6)].rgb * ((0.315392 * ((((3.0 * z) * z) - 1.0)))))) + (probeGrid.cells[(base + 7)].rgb * (((1.092548 * x) * z)))) + (probeGrid.cells[(base + 8)].rgb * ((0.546274 * (((x * x) - (y * y)))))));
}

fn gridIrradiance(_n: vec3f, _pos: vec3f, cover: ptr<function, f32>) -> vec3f {
var n = _n;
var pos = _pos;
(*cover) = 0.0;
var last: vec3f = max((frame.gridCounts.xyz - vec3f(1.0)), vec3f(0.0));
var g: vec3f = (((pos - frame.gridOrigin.xyz)) / max(frame.gridSpacing.xyz, vec3f(1e-4)));
if g.x < -0.5 || g.y < -0.5 || g.z < -0.5 || g.x > last.x + 0.5 || g.y > last.y + 0.5 || g.z > last.z + 0.5 { return vec3f(0.0); }
var c: vec3f = clamp(g, vec3f(0.0), last);
var f: vec3f = fract(c);
var i0: vec3i = vec3i(floor(c));
var i1: vec3i = vec3i(min((vec3f(i0) + vec3f(1.0)), last));
var nx: i32 = i32(frame.gridCounts.x);
var ny: i32 = i32(frame.gridCounts.y);
var sum: vec3f = vec3f(0.0);
for (var k: i32 = 0; (k < 8); k++) {
var o: vec3i = vec3i((k & 1), (((k >> u32(1))) & 1), (((k >> u32(2))) & 1));
var idx: vec3i = vec3i(select(i1.x, i0.x, (o.x == 0)), select(i1.y, i0.y, (o.y == 0)), select(i1.z, i0.z, (o.z == 0)));
var w3: vec3f = vec3f(select(f.x, (1.0 - f.x), (o.x == 0)), select(f.y, (1.0 - f.y), (o.y == 0)), select(f.z, (1.0 - f.z), (o.z == 0)));
var w: f32 = ((w3.x * w3.y) * w3.z);
if (w <= 0.0) { continue; }
sum += (cellIrradiance((((((((idx.z * ny) + idx.y)) * nx) + idx.x)) * 9), n) * w);
}
var edge: vec3f = min((g + vec3f(0.5)), ((last + vec3f(0.5)) - g));
(*cover) = clamp((min(min(edge.x, edge.y), edge.z) * 2.0), 0.0, 1.0);
return sum;
}

fn envDiffuse(_n: vec3f) -> vec3f {
var n = _n;
return (irradiance(n) * frame.env.x);
}

fn envDiffuseAt(_n: vec3f, _worldPos: vec3f) -> vec3f {
var n = _n;
var worldPos = _worldPos;
if (frame.gridOrigin.w > 0.0) {
var cover: f32;
var e: vec3f = gridIrradiance(n, worldPos, &cover);
if (cover > 0.0) { return mix(envDiffuse(n), (e * frame.gridOrigin.w), vec3f(cover)); }
}
return envDiffuse(n);
}

fn reflectWeight(_s: Surface) -> f32 {
var s = _s;
var maxRough: f32 = max(frame.reflect.y, 1e-3);
var gloss: f32 = (1.0 - smoothstep((maxRough * 0.5), maxRough, s.roughness));
if (gloss <= 0.0) { return 0.0; }
var n: vec3f = normalize(s.normal);
var NoV: f32 = max(dot(n, s.viewDir), 1e-4);
var f0: vec3f = baseF0(s);
var F: vec3f = (f0 + (((max(vec3f((1.0 - s.roughness)), f0) - f0)) * pow((1.0 - NoV), 5.0)));
return clamp(((max(max(F.r, F.g), F.b) * gloss) * frame.reflect.x), 0.0, 1.0);
}

fn ambient(_s: Surface, _n: vec3f, _v: vec3f) -> vec3f {
var s = _s;
var n = _n;
var v = _v;
var NoV: f32 = max(dot(n, v), 1e-4);
var f0: vec3f = baseF0(s);
var kS: vec3f = (f0 + (((max(vec3f((1.0 - s.roughness)), f0) - f0)) * pow((1.0 - NoV), 5.0)));
var kD: vec3f = (((vec3f(1.0) - kS)) * ((1.0 - s.metallic)));
var r: vec3f = reflect((-v), n);
if (s.anisotropy != 0.0) {
var t: vec3f = normalize((s.tangent - (n * dot(n, s.tangent))));
var dir: vec3f = select(t, cross(n, t), (s.anisotropy >= 0.0));
var bent: vec3f = normalize(mix(n, cross(cross(dir, v), dir), vec3f(abs(s.anisotropy))));
r = reflect((-v), bent);
}
var diffuse: vec3f = envDiffuseAt(n, s.worldPos);
var color: vec3f = (((kD * s.albedo) * diffuse) * ((1.0 - s.transmission)));
if (s.transmission > 0.0) {
color += ((kD * s.transmission) * transmitted(s, n, v));
}
color += (envSpecular(r, s.roughness) * iridescent(s, envBRDF(f0, s.roughness, NoV), NoV));
if (dot(s.sheen, s.sheen) > 0.0) {
color += (((s.sheen * diffuse) * 0.25) * ((1.0 - (s.sheenRoughness * 0.5))));
}
if (s.clearcoat > 0.0) {
var Fc: vec3f = (F_Schlick(NoV, vec3f(0.04)) * s.clearcoat);
color = ((color * ((vec3f(1.0) - Fc))) + (envSpecular(r, s.clearcoatRoughness) * Fc));
}
if (s.subsurface > 0.0) {
color += ((((s.albedo * envDiffuseAt((-n), s.worldPos)) * 0.5) * ((1.0 - s.thickness))) * s.subsurface);
}
return color;
}
fn albedoTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(0) {
case 0: { return textureSampleGrad(tAlbedo, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tAlbedo, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tAlbedo, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tAlbedo, materialSampler3, uv, dx, dy); }
}
}
fn metalRoughTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(1) {
case 0: { return textureSampleGrad(tMetalRough, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tMetalRough, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tMetalRough, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tMetalRough, materialSampler3, uv, dx, dy); }
}
}
fn normalTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(2) {
case 0: { return textureSampleGrad(tNormal, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tNormal, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tNormal, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tNormal, materialSampler3, uv, dx, dy); }
}
}
fn emissiveTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(3) {
case 0: { return textureSampleGrad(tEmissive, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tEmissive, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tEmissive, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tEmissive, materialSampler3, uv, dx, dy); }
}
}
fn occlusionTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(4) {
case 0: { return textureSampleGrad(tOcclusion, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tOcclusion, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tOcclusion, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tOcclusion, materialSampler3, uv, dx, dy); }
}
}
fn image0(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(5) {
case 0: { return textureSampleGrad(tImage0, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tImage0, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tImage0, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tImage0, materialSampler3, uv, dx, dy); }
}
}
fn image1(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(6) {
case 0: { return textureSampleGrad(tImage1, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tImage1, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tImage1, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tImage1, materialSampler3, uv, dx, dy); }
}
}
fn image2(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(7) {
case 0: { return textureSampleGrad(tImage2, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tImage2, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tImage2, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tImage2, materialSampler3, uv, dx, dy); }
}
}
fn image3(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(8) {
case 0: { return textureSampleGrad(tImage3, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tImage3, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tImage3, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tImage3, materialSampler3, uv, dx, dy); }
}
}
fn thicknessTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(9) {
case 0: { return textureSampleGrad(tThickness, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tThickness, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tThickness, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tThickness, materialSampler3, uv, dx, dy); }
}
}
fn transmissionTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
switch texSampler(10) {
case 0: { return textureSampleGrad(tTransmission, materialSampler0, uv, dx, dy); }
case 1: { return textureSampleGrad(tTransmission, materialSampler1, uv, dx, dy); }
case 2: { return textureSampleGrad(tTransmission, materialSampler2, uv, dx, dy); }
default: { return textureSampleGrad(tTransmission, materialSampler3, uv, dx, dy); }
}
}
fn iridescenceTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
return textureSampleGrad(tIridescence, materialSampler0, uv, dx, dy);
}
fn anisotropyTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
return textureSampleGrad(tAnisotropy, materialSampler0, uv, dx, dy);
}
fn specularTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
return textureSampleGrad(tSpecular, materialSampler0, uv, dx, dy);
}
fn furTex(uv: vec2f) -> vec4f {
let dx = dpdx(uv); let dy = dpdy(uv);
return textureSampleGrad(tFur, materialSampler0, uv, dx, dy);
}

// sampleImage0 samples the shader's image0 slot using its texture settings.
fn sampleImage0(uv: vec2f) -> vec4f { return image0(uv); }

// sampleImage1 samples the shader's image1 slot using its texture settings.
fn sampleImage1(uv: vec2f) -> vec4f { return image1(uv); }

// sampleImage2 samples the shader's image2 slot using its texture settings.
fn sampleImage2(uv: vec2f) -> vec4f { return image2(uv); }

// sampleImage3 samples the shader's image3 slot using its texture settings.
fn sampleImage3(uv: vec2f) -> vec4f { return image3(uv); }
