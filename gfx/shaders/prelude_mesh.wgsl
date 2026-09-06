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

// ATMOSPHERE. Everything between this line and END ATMOSPHERE is the
// same text in prelude_mesh.wgsl and skyparam.frag.wgsl, and Sky.scatter and
// Sky.radiance in gfx/sky.go are the same functions in Go: the ambient
// harmonics are projected from the Go side and the pixels come from
// here, so the three must stay in step. TestAtmosphereBlocksMatch
// compares the two shaders and TestAtmosphereMatchesGo the Go side.
const ATMOS_VIEW_STEPS: i32 = 8;
const ATMOS_SUN_STEPS: i32 = 4;
const ATMOS_PI: f32 = 3.14159265359;

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
    var d: f32 = (2.0 + g2) * pow(1.0 + g2 - 2.0 * g * mu, 1.5);
    return 3.0 / (8.0 * ATMOS_PI) * ((1.0 - g2) * (1.0 + mu * mu)) / d;
}

// atmosphereScatter integrates single scattering along a ray leaving the
// camera in direction d, for at most dist world units: air and haze
// thinning with height, each sample lit by what is left of the sunlight
// that reached it and dimmed by the air back to the camera. Samples the
// planet shadows are dark, which is what makes dusk fall. transmittance
// comes back as how much of the light from beyond the segment survives
// it, for aerial perspective and the sun's disc.
struct ScatterResult { radiance: vec3f, transmittance: vec3f }
fn atmosphereScatter(d: vec3f, dist: f32, steps: i32, sunSteps: i32) -> ScatterResult {
    var radius: f32 = frame.atmos.x;
    var height: f32 = frame.atmos.y;
    var hR: f32 = frame.atmos.z;
    var hM: f32 = frame.atmos.w;
    var betaR: vec3f = frame.betaR.rgb;
    var betaM: f32 = frame.betaM.x;
    var sun: vec3f = frame.sun.xyz;
    var origin: vec3f = frame.skyUp.xyz * (radius + frame.betaM.z);
    var transmittance = vec3f(1.0);
    var shell: vec2f = raySphere(origin, d, radius + height);
    var t0: f32 = max(shell.x, 0.0);
    var t1: f32 = shell.y;
    if (t1 <= t0) { return ScatterResult(vec3f(0.0), transmittance); } // outside the air, looking away from the planet
    var gnd: vec2f = raySphere(origin, d, radius);
    if (gnd.y > 0.0 && gnd.x > 0.0) { t1 = min(t1, gnd.x); } // the ray meets the ground first
    t1 = min(t1, t0 + dist);
    if (t1 <= t0) { return ScatterResult(vec3f(0.0), transmittance); }
    var ds: f32 = (t1 - t0) / f32(steps);
    var mu: f32 = dot(d, sun);
    var odR: f32 = 0.0;
    var odM: f32 = 0.0;
    var sumR: vec3f = vec3f(0.0);
    var sumM: vec3f = vec3f(0.0);
    for (var i: i32 = 0; i < steps; i++) {
        var p: vec3f = origin + d * (t0 + (f32(i) + 0.5) * ds);
        var h: f32 = max(length(p) - radius, 0.0);
        var dR: f32 = exp(-h / hR) * ds;
        var dM: f32 = exp(-h / hM) * ds;
        odR += dR;
        odM += dM;
        var shadow: vec2f = raySphere(p, sun, radius);
        if (shadow.y > 0.0 && shadow.x > 0.0) { continue; } // the planet stands in the way
        var lightStep: f32 = max(raySphere(p, sun, radius + height).y, 0.0) / f32(sunSteps);
        var lodR: f32 = 0.0;
        var lodM: f32 = 0.0;
        for (var j: i32 = 0; j < sunSteps; j++) {
            var q: vec3f = p + sun * ((f32(j) + 0.5) * lightStep);
            var hj: f32 = max(length(q) - radius, 0.0);
            lodR += exp(-hj / hR) * lightStep;
            lodM += exp(-hj / hM) * lightStep;
        }
        var att: vec3f = exp(-(betaR * (odR + lodR) + betaM * 1.1 * (odM + lodM)));
        sumR += att * dR;
        sumM += att * dM;
    }
    transmittance = exp(-(betaR * odR + betaM * 1.1 * odM));
    return ScatterResult(frame.betaR.w * (sumR * betaR * phaseRayleigh(mu) + sumM * betaM * phaseMie(mu, frame.betaM.y)), transmittance);
}

// skyColor is the sky's light from a direction without the sun's disc:
// the atmosphere when the light's Sky has one, otherwise the gradient
// above the horizon, and the ground or planet below. Below the horizon a
// camera inside the air looks the colour up along the horizon instead,
// because its own ray meets the ground at once and would leave a dark
// band; from above the air the ray itself is integrated, so a planet
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
        var tr: vec3f;
        var c: vec3f = atmosphereScatter(dir, 1e9, ATMOS_VIEW_STEPS, ATMOS_SUN_STEPS).radiance * air;
        if (up < 0.0) { c = mix(c, frame.ground.rgb, pow(-up, 0.5)); }
        return c;
    }
    var above: vec3f = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    var below: vec3f = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    return select(below, above, up >= 0.0);
}
// END ATMOSPHERE.

fn aerialPerspective(_c: vec3f, _worldPos: vec3f) -> vec3f {
var c = _c;
var worldPos = _worldPos;
var d: vec3f = (worldPos - frame.camPos.xyz);
var dist: f32 = length(d);
if (dist < 1e-4) { return c; }
let scatter = atmosphereScatter(d / dist, dist, ATMOS_VIEW_STEPS / 2, ATMOS_SUN_STEPS / 2);
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
return mix(skyColor(d), (frame.sh[0].rgb * 0.282095), vec3f((roughness * 0.8)));
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
