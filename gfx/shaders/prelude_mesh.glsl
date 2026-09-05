#version 450

// Prelude for mesh (surface) shaders. bunyip-shader puts this before the
// game's code and the lighting main after it; the game defines
//
//     void surface(inout Surface s)          // adjust inputs before lighting
//     vec4 finish(vec4 lit, Surface s)       // optional: post-process the lit colour
//     void vertex(inout VertexData v)        // optional: move vertices first
//
// Lighting is metallic-roughness PBR: Cook-Torrance with GGX
// distribution, Schlick Fresnel and Smith-GGX visibility; one shadowed
// directional light, the point and spot lights of the fragment's own
// cluster, image-based or hemisphere ambient, plus clearcoat, sheen and
// subsurface lobes when a material asks for them.
// Set 0 keeps images and samplers apart: thirteen sampled images and one
// array of four samplers shared by every material (linear repeat, linear
// clamp, nearest repeat, nearest clamp). A texture's own filtering and
// edge handling pick its sampler, and the index rides in the instance
// stream, so a new material texture costs an image and no sampler. The
// names below are macros that pair the two, so texture(albedoTex, uv)
// and texture(image0, uv) read as they always have.
layout(set = 0, binding = 0) uniform texture2D tAlbedo;
layout(set = 0, binding = 1) uniform texture2D tMetalRough; // G roughness, B metallic (glTF)
layout(set = 0, binding = 2) uniform texture2D tNormal;
layout(set = 0, binding = 3) uniform texture2D tEmissive;
layout(set = 0, binding = 4) uniform texture2D tOcclusion; // R: baked ambient occlusion
layout(set = 0, binding = 5) uniform texture2D tImage0;
layout(set = 0, binding = 6) uniform texture2D tImage1;
layout(set = 0, binding = 7) uniform texture2D tImage2;
layout(set = 0, binding = 8) uniform texture2D tImage3;
layout(set = 0, binding = 9) uniform textureCube tEnv;     // prefiltered environment, one level per roughness
layout(set = 0, binding = 10) uniform texture2D tThickness; // R: 1 thick, 0 thin, for subsurface
layout(set = 0, binding = 11) uniform texture2D tScene;     // the opaque scene, blurred down its mips, for transmission
layout(set = 0, binding = 12) uniform texture2D tTransmission; // R scales the transmission factor
layout(set = 0, binding = 13) uniform sampler samplers[4];

// LINEAR_CLAMP is the sampler the engine's own images always use.
const int LINEAR_CLAMP = 1;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3]; // shadow cascades
    vec4 camPos;
    vec4 lightDir;     // direction the light travels
    vec4 lightColor;   // rgb, w = shadow strength
    vec4 sky;          // rgb ambient from above
    vec4 ground;       // rgb ambient from below
    vec4 params;       // x = shadow map size, y = shadows enabled, z = point light count, w = time
    vec4 splits;       // view-space distances where cascades end
    vec4 radii;        // half-size of each cascade's orthographic box
    vec4 sh[9];        // environment irradiance as spherical harmonics
    vec4 env;          // x intensity, y mip count, z = 1 when an environment is set
    mat4 invViewProj;
    vec4 horizon;      // rgb the sky at the horizon, w = air (1 - vacuum)
    vec4 skyUp;        // xyz up, w = stars
    vec4 sun;          // xyz towards the sun, w = angular radius
    vec4 sunColor;     // rgb the drawn disc's radiance
    vec4 fog;          // rgb the fog colour, w = exponential density
    vec4 fogRange;     // x start, y end of linear fog; z height, w falloff of ground fog
    mat4 spotViewProj[4];  // shadowed spot lights' projections
    mat4 pointViewProj[24]; // four shadowed point lights, six faces each
    vec4 cluster;      // xy tile size in pixels, zw the depth slice's scale and bias
} frame;

// The frame's point and spot lights, and the cluster grid that says
// which of them reach a fragment: sixteen tiles across the view, nine
// down and twenty-four slices into the distance, the slices spaced
// exponentially. cells holds each cluster's first index and count in
// lightIndex, which holds indices into lights. gfx/cluster.go fills all
// three each frame.
struct LightData {
    vec4 posRange; // xyz position, w range
    vec4 color;    // rgb colour, w = cos of a spot's inner cone (2 for a point light)
    vec4 dir;      // xyz a spot's direction, w = cos of its outer cone (-2 for a point light)
    vec4 info;     // x = spot map index or -1, y = cube map slot or -1
};
layout(std430, set = 1, binding = 1) readonly buffer Lights { LightData lights[]; };
layout(std430, set = 1, binding = 2) readonly buffer Clusters { uvec2 cells[]; };
layout(std430, set = 1, binding = 3) readonly buffer LightIndex { uint lightIndex[]; };
const uint CLUSTER_X = 16u;
const uint CLUSTER_Y = 9u;
const uint CLUSTER_Z = 24u;

// One atlas holds every shadow map: the three cascades of 2048 in three
// quadrants of the square top, four spot maps of 1024 in the fourth, and
// the cube faces of 512 in the strip below, eight to a row. shadowRegion
// in gfx/mesh_draw.go lays out the same rectangles.
layout(set = 2, binding = 0) uniform sampler2DShadow shadowAtlas;
const vec2 SHADOW_ATLAS = vec2(4096.0, 6144.0);
const float POINT_FACE = 512.0;

#define UNIFORMS layout(set = 4, binding = 0)

layout(location = 0) in vec3 vWorldPos;
layout(location = 1) in vec3 vNormal;
layout(location = 2) in vec2 vUV;
layout(location = 3) in float vViewDepth;
layout(location = 4) flat in vec4 vBaseColor;
layout(location = 5) flat in vec4 vMaterial; // x metallic, y roughness, z emissive, w flags (1 normal map, 2 unlit, 4 occlusion on uv2)
layout(location = 6) flat in vec4 vExtra;    // y alpha cutoff, z occlusion strength, w subsurface
layout(location = 7) in vec2 vUV2;
layout(location = 8) in vec4 vColor;
layout(location = 9) flat in vec4 vUVT0;     // texture transform a, b, c, d
layout(location = 10) flat in vec4 vUVT1;    // texture transform e, f; z clearcoat, w clearcoat roughness
layout(location = 11) flat in vec4 vSheen;   // sheen colour, w sheen roughness
layout(location = 12) flat in vec4 vVolume;  // x transmission, y ior, z thickness, w attenuation distance
layout(location = 13) flat in vec4 vAtten;   // attenuation colour, w = packed sampler indices
layout(location = 0) out vec4 outColor;

// texSampler is the sampler for one of the material set's texture slots,
// two bits apiece in the packed index the instance stream carries. Every
// instance of a draw shares set 0, so the value is the same across the
// draw, which is what indexing the sampler array needs.
int texSampler(int slot) { return (int(vAtten.w) >> (2 * slot)) & 3; }

#define albedoTex sampler2D(tAlbedo, samplers[texSampler(0)])
#define metalRoughTex sampler2D(tMetalRough, samplers[texSampler(1)])
#define normalTex sampler2D(tNormal, samplers[texSampler(2)])
#define emissiveTex sampler2D(tEmissive, samplers[texSampler(3)])
#define occlusionTex sampler2D(tOcclusion, samplers[texSampler(4)])
#define image0 sampler2D(tImage0, samplers[texSampler(5)])
#define image1 sampler2D(tImage1, samplers[texSampler(6)])
#define image2 sampler2D(tImage2, samplers[texSampler(7)])
#define image3 sampler2D(tImage3, samplers[texSampler(8)])
#define thicknessTex sampler2D(tThickness, samplers[texSampler(9)])
#define transmissionTex sampler2D(tTransmission, samplers[texSampler(10)])
#define envMap samplerCube(tEnv, samplers[LINEAR_CLAMP])
#define sceneTex sampler2D(tScene, samplers[LINEAR_CLAMP])

// Surface is what the lighting sees. surface() may change any field:
// albedo and alpha (linear, not premultiplied), normal (world space),
// metallic, roughness, emissive (linear radiance added after lighting),
// occlusion (0..1, scales ambient light), unlit (skip lighting),
// clearcoat and clearcoatRoughness (a varnish layer), sheen and
// sheenRoughness (cloth), subsurface and thickness (light through thin
// parts), transmission, ior, volume (thickness in world units),
// attenuation and attenuationDistance (light through glass and liquids).
// color is the vertex colour, already multiplied into albedo.
struct Surface {
    vec3 albedo;
    float alpha;
    vec3 normal;
    float metallic;
    float roughness;
    vec3 emissive;
    float occlusion;
    bool unlit;
    vec2 uv;
    vec2 uv2;
    vec4 color;
    vec3 worldPos;
    vec3 viewDir;   // towards the camera
    float clearcoat;
    float clearcoatRoughness;
    vec3 sheen;
    float sheenRoughness;
    float subsurface;
    float thickness;
    float transmission;
    float ior;
    float volume;
    vec3 attenuation;
    float attenuationDistance;
};

// VertexData and model are the vertex stage's; they are here so a
// shader's vertex() compiles in this stage too, where it is never called.
struct VertexData {
    vec3 position;
    vec3 normal;
    vec2 uv;
    vec2 uv2;
    vec4 color;
};
mat4 model() { return mat4(1.0); }

// time is seconds since the game started.
float time() { return frame.params.w; }

// uvTransform maps a texture coordinate through the material's transform.
vec2 uvTransform(vec2 uv) {
    return vec2(vUVT0.x * uv.x + vUVT0.y * uv.y + vUVT0.z, vUVT0.w * uv.x + vUVT1.x * uv.y + vUVT1.y);
}

const float PI = 3.14159265359;

// Tangent frame from screen-space derivatives, so meshes need no tangents.
vec3 perturbNormal(vec3 n, vec3 pos, vec2 uv) {
    vec3 dp1 = dFdx(pos), dp2 = dFdy(pos);
    vec2 duv1 = dFdx(uv), duv2 = dFdy(uv);
    vec3 dp2perp = cross(dp2, n), dp1perp = cross(n, dp1);
    vec3 t = dp2perp * duv1.x + dp1perp * duv2.x;
    vec3 b = dp2perp * duv1.y + dp1perp * duv2.y;
    float invmax = inversesqrt(max(dot(t, t), dot(b, b)) + 1e-8);
    mat3 tbn = mat3(t * invmax, b * invmax, n);
    vec3 nm = texture(normalTex, uv).xyz * 2.0 - 1.0;
    return normalize(tbn * nm);
}

// sampleAtlas takes nine comparisons around a point of one map in the
// atlas: origin and size are the map's rectangle in atlas pixels.
float sampleAtlas(vec2 origin, float size, vec3 uvz) {
    float lit = 0.0;
    vec2 base = (origin + uvz.xy * size) / SHADOW_ATLAS;
    vec2 texel = 1.0 / SHADOW_ATLAS;
    for (int y = -1; y <= 1; y++)
        for (int x = -1; x <= 1; x++) {
            lit += texture(shadowAtlas, vec3(base + vec2(x, y) * texel, uvz.z));
        }
    return lit / 9.0;
}

float sampleCascade(int c, vec3 uvz) {
    return sampleAtlas(vec2(float(c % 2), float(c / 2)) * 2048.0, 2048.0, uvz);
}

// shadowFactor is 1 where the directional light reaches, 0 in shadow.
float shadowFactor(vec3 n, vec3 l) {
    if (frame.params.y < 0.5) return 1.0;
    int c = vViewDepth < frame.splits.x ? 0 : (vViewDepth < frame.splits.y ? 1 : 2);
    if (vViewDepth > frame.splits.z) return 1.0;
    // Normal-offset shadows: push the lookup point off the surface by
    // about one shadow texel of this cascade, more at grazing angles, so
    // the map's quantisation cannot self-shadow.
    float radius = c == 0 ? frame.radii.x : (c == 1 ? frame.radii.y : frame.radii.z);
    float texelWorld = 2.0 * radius / frame.params.x;
    float NoL = clamp(dot(n, l), 0.0, 1.0);
    float slope = sqrt(1.0 - NoL * NoL) / max(NoL, 0.05);
    vec3 pos = vWorldPos + n * texelWorld * (1.0 + slope);
    vec4 sp = frame.lightViewProj[c] * vec4(pos, 1.0);
    vec3 p = sp.xyz / sp.w;
    vec2 uv = p.xy * 0.5 + 0.5;
    if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || p.z > 1.0) return 1.0;
    float bias = texelWorld / (4.0 * radius); // one texel in depth units
    return sampleCascade(c, vec3(uv, p.z - bias));
}

float sampleSpot(int k, vec3 uvz) {
    return sampleAtlas(vec2(2048.0 + float(k % 2) * 1024.0, 2048.0 + float(k / 2) * 1024.0), 1024.0, uvz);
}

// spotShadowFactor is 1 where spot light i reaches the surface, 0 in
// its shadow; k is the light's shadow map, dist its distance and
// cosOuter its cone.
float spotShadowFactor(int k, vec3 n, vec3 l, float dist, float cosOuter) {
    // Normal-offset by one shadow texel at this distance.
    float tanHalf = sqrt(max(1.0 - cosOuter * cosOuter, 0.0)) / max(cosOuter, 0.05);
    float texelWorld = 2.0 * dist * tanHalf * 1.1 / 1024.0;
    float NoL = clamp(dot(n, l), 0.0, 1.0);
    float slope = sqrt(1.0 - NoL * NoL) / max(NoL, 0.05);
    vec3 pos = vWorldPos + n * texelWorld * (1.0 + slope);
    vec4 sp = frame.spotViewProj[k] * vec4(pos, 1.0);
    if (sp.w <= 0.0) return 1.0;
    vec3 p = sp.xyz / sp.w;
    vec2 uv = p.xy * 0.5 + 0.5;
    if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0 || p.z > 1.0) return 1.0;
    return sampleSpot(k, vec3(uv, p.z - 0.0005));
}

// pointFace picks which face of a cube map a direction falls on, in the
// order pointFaces lists them in gfx/mesh_draw.go: +x, -x, +y, -y, +z, -z.
int pointFace(vec3 d) {
    vec3 a = abs(d);
    if (a.x >= a.y && a.x >= a.z) return d.x > 0.0 ? 0 : 1;
    if (a.y >= a.z) return d.y > 0.0 ? 2 : 3;
    return d.z > 0.0 ? 4 : 5;
}

// samplePoint reads one face of point light slot's cube map. The face is
// rendered a little wider than ninety degrees, so clamping the lookup
// half a filter kernel inside the face keeps the nine comparisons off
// the neighbouring face's pixels.
float samplePoint(int slot, int face, vec3 uvz) {
    int tile = slot * 6 + face;
    vec2 origin = vec2(float(tile % 8) * POINT_FACE, 4096.0 + float(tile / 8) * POINT_FACE);
    float edge = 1.5 / POINT_FACE;
    return sampleAtlas(origin, POINT_FACE, vec3(clamp(uvz.xy, edge, 1.0 - edge), uvz.z));
}

// pointShadowFactor is 1 where the point light in cube map slot reaches
// the surface, 0 in its shadow; l points from the surface to the light
// and dist is how far away it is. The bias is a texel of the face at
// this distance, which grows with it.
float pointShadowFactor(int slot, vec3 n, vec3 l, float dist) {
    int face = pointFace(-l);
    // Normal-offset by one texel of the face at this distance: a face
    // covers ninety degrees across POINT_FACE pixels.
    float texelWorld = 2.0 * dist / POINT_FACE;
    float NoL = clamp(dot(n, l), 0.0, 1.0);
    float slope = sqrt(1.0 - NoL * NoL) / max(NoL, 0.05);
    vec3 pos = vWorldPos + n * texelWorld * (1.0 + slope);
    vec4 sp = frame.pointViewProj[slot * 6 + face] * vec4(pos, 1.0);
    if (sp.w <= 0.0) return 1.0;
    vec3 p = sp.xyz / sp.w;
    vec2 uv = p.xy * 0.5 + 0.5;
    if (p.z > 1.0) return 1.0;
    return samplePoint(slot, face, vec3(uv, p.z - 0.0015));
}

float D_GGX(float NoH, float a2) {
    float d = NoH * NoH * (a2 - 1.0) + 1.0;
    return a2 / (PI * d * d);
}

float V_SmithGGX(float NoV, float NoL, float a2) {
    float gv = NoL * sqrt(NoV * NoV * (1.0 - a2) + a2);
    float gl = NoV * sqrt(NoL * NoL * (1.0 - a2) + a2);
    return 0.5 / max(gv + gl, 1e-5);
}

vec3 F_Schlick(float VoH, vec3 f0) {
    return f0 + (1.0 - f0) * pow(1.0 - VoH, 5.0);
}

// D_Charlie is the sheen distribution (Estevez and Kulla).
float D_Charlie(float NoH, float roughness) {
    float a = max(roughness, 0.05);
    float invA = 1.0 / a;
    float sin2 = 1.0 - NoH * NoH;
    return (2.0 + invA) * pow(sin2, invA * 0.5) / (2.0 * PI);
}

// V_Neubelt is the visibility term paired with D_Charlie.
float V_Neubelt(float NoV, float NoL) {
    return 1.0 / (4.0 * max(NoL + NoV - NoL * NoV, 1e-4));
}

// shade is one light's contribution to a surface's base layer.
vec3 shade(vec3 n, vec3 v, vec3 l, vec3 radiance, vec3 albedo, float metallic, float roughness) {
    vec3 h = normalize(l + v);
    float NoL = max(dot(n, l), 0.0);
    float NoV = max(dot(n, v), 1e-4);
    float NoH = max(dot(n, h), 0.0);
    float VoH = max(dot(v, h), 0.0);
    float a = roughness * roughness;
    float a2 = a * a;
    vec3 f0 = mix(vec3(0.04), albedo, metallic);
    vec3 F = F_Schlick(VoH, f0);
    vec3 spec = D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2) * F;
    vec3 kd = (1.0 - F) * (1.0 - metallic);
    return (kd * albedo / PI + spec) * radiance * NoL;
}

// shadeVolume is shade with the diffuse lobe scaled down by how much of
// the light passes through instead.
vec3 shadeVolume(vec3 n, vec3 v, vec3 l, vec3 radiance, vec3 albedo, float metallic, float roughness, float transmission) {
    vec3 h = normalize(l + v);
    float NoL = max(dot(n, l), 0.0);
    float NoV = max(dot(n, v), 1e-4);
    float NoH = max(dot(n, h), 0.0);
    float VoH = max(dot(v, h), 0.0);
    float a = roughness * roughness;
    float a2 = a * a;
    vec3 f0 = mix(vec3(0.04), albedo, metallic);
    vec3 F = F_Schlick(VoH, f0);
    vec3 spec = D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2) * F;
    vec3 kd = (1.0 - F) * (1.0 - metallic) * (1.0 - transmission);
    return (kd * albedo / PI + spec) * radiance * NoL;
}

// transmitted is the light arriving through a transmissive surface: the
// opaque scene behind it, refracted across the volume, blurred by the
// roughness and absorbed by the attenuation colour over the distance,
// then tinted by the albedo.
vec3 transmitted(Surface s, vec3 n, vec3 v) {
    vec3 dir = refract(-v, n, 1.0 / max(s.ior, 1.0));
    if (dot(dir, dir) < 1e-6) dir = -v;
    vec4 clip = frame.viewProj * vec4(s.worldPos + dir * s.volume, 1.0);
    vec2 uv = clip.xy / max(clip.w, 1e-4) * 0.5 + 0.5;
    float levels = float(textureQueryLevels(sceneTex) - 1);
    vec3 color = textureLod(sceneTex, clamp(uv, 0.0, 1.0), s.roughness * levels * 0.7).rgb;
    if (s.attenuationDistance > 0.0) {
        vec3 sigma = -log(max(s.attenuation, vec3(1e-3))) / s.attenuationDistance;
        color *= exp(-sigma * s.volume);
    }
    return color * s.albedo;
}

// lobes is one light's contribution to a surface: the base layer plus
// sheen, clearcoat and subsurface transmission.
vec3 lobes(Surface s, vec3 n, vec3 v, vec3 l, vec3 radiance) {
    vec3 color = shadeVolume(n, v, l, radiance, s.albedo, s.metallic, s.roughness, s.transmission);
    vec3 h = normalize(l + v);
    float NoL = max(dot(n, l), 0.0);
    float NoV = max(dot(n, v), 1e-4);
    float NoH = max(dot(n, h), 0.0);
    float VoH = max(dot(v, h), 0.0);
    if (dot(s.sheen, s.sheen) > 0.0) {
        color += s.sheen * D_Charlie(NoH, s.sheenRoughness) * V_Neubelt(NoV, NoL) * radiance * NoL;
    }
    if (s.clearcoat > 0.0) {
        float a = max(s.clearcoatRoughness, 0.03);
        float a2 = a * a * a * a;
        vec3 Fc = F_Schlick(VoH, vec3(0.04)) * s.clearcoat;
        vec3 coat = D_GGX(NoH, a2) * V_SmithGGX(NoV, NoL, a2) * Fc;
        color = color * (1.0 - Fc) + coat * radiance * NoL;
    }
    if (s.subsurface > 0.0) {
        // Light through the surface: the view looking towards the light
        // through the body, bent by the normal, scaled by thinness.
        vec3 through = normalize(l + n * 0.3);
        float back = pow(max(dot(v, -through), 0.0), 3.0);
        color += s.albedo * radiance * (back + 0.15) * (1.0 - s.thickness) * s.subsurface;
    }
    return color;
}

// clusterAt is the cluster this fragment sits in: its tile across the
// view and its slice into the distance.
uint clusterAt() {
    uvec2 tile = uvec2(max(gl_FragCoord.xy, vec2(0.0)) / max(frame.cluster.xy, vec2(1.0)));
    tile = min(tile, uvec2(CLUSTER_X - 1u, CLUSTER_Y - 1u));
    float slice = log2(max(vViewDepth, 1e-4)) * frame.cluster.z + frame.cluster.w;
    uint z = uint(clamp(slice, 0.0, float(CLUSTER_Z - 1u)));
    return tile.x + tile.y * CLUSTER_X + z * CLUSTER_X * CLUSTER_Y;
}

vec3 ambient(Surface s, vec3 n, vec3 v);

// light runs the engine's lighting over a surface: the shadowed
// directional light, the lights of the fragment's own cluster, and the
// ambient term.
vec3 light(Surface s) {
    vec3 n = normalize(s.normal);
    vec3 v = s.viewDir;
    vec3 l = normalize(-frame.lightDir.xyz);
    float shadow = mix(1.0, shadowFactor(n, l), frame.lightColor.w);
    vec3 color = lobes(s, n, v, l, frame.lightColor.rgb * shadow);

    uvec2 cell = cells[clusterAt()];
    for (uint c = 0u; c < cell.y; c++) {
        LightData ld = lights[lightIndex[cell.x + c]];
        vec3 d = ld.posRange.xyz - s.worldPos;
        float dist = length(d);
        float range = max(ld.posRange.w, 1e-3);
        float att = clamp(1.0 - (dist * dist) / (range * range), 0.0, 1.0);
        att *= att / max(dist * dist, 1e-3);
        float cone = ld.dir.w;
        if (cone > -1.5) {
            // A spot light: full inside the inner cone, fading to nothing
            // at the outer one.
            float cd = dot(-d / dist, ld.dir.xyz);
            att *= smoothstep(cone, max(ld.color.w, cone + 1e-3), cd);
            int k = int(ld.info.x);
            if (k >= 0 && att > 0.0) att *= spotShadowFactor(k, n, d / dist, dist, cone);
        } else {
            int slot = int(ld.info.y);
            if (slot >= 0 && att > 0.0) att *= pointShadowFactor(slot, n, d / dist, dist);
        }
        color += lobes(s, n, v, d / dist, ld.color.rgb * att);
    }
    return color + ambient(s, n, v) * s.occlusion;
}

// applyFog fades a lit colour towards the frame's fog colour by the
// distance from the camera: linear between the range's start and end,
// exponential-squared by the density, whichever is denser, thinned
// above the ground fog's height. Every mesh shader's output passes
// through it; call it yourself in finish() only to fog something the
// engine does not.
vec3 applyFog(vec3 c, vec3 worldPos, float depth) {
    float f = 0.0;
    if (frame.fogRange.y > frame.fogRange.x) {
        f = clamp((depth - frame.fogRange.x) / (frame.fogRange.y - frame.fogRange.x), 0.0, 1.0);
    }
    if (frame.fog.w > 0.0) {
        float e = depth * frame.fog.w;
        f = max(f, 1.0 - exp(-e * e));
    }
    if (frame.fogRange.w > 0.0) {
        f *= clamp(exp(-(worldPos.y - frame.fogRange.z) * frame.fogRange.w), 0.0, 1.0);
    }
    return mix(c, frame.fog.rgb, f);
}

// irradiance evaluates the environment's spherical harmonics for a
// normal: the diffuse radiance an albedo of 1 would reflect.
vec3 irradiance(vec3 n) {
    float x = n.x, y = n.y, z = n.z;
    return frame.sh[0].rgb * 0.282095
         + frame.sh[1].rgb * (0.488603 * y) + frame.sh[2].rgb * (0.488603 * z) + frame.sh[3].rgb * (0.488603 * x)
         + frame.sh[4].rgb * (1.092548 * x * y) + frame.sh[5].rgb * (1.092548 * y * z)
         + frame.sh[6].rgb * (0.315392 * (3.0 * z * z - 1.0)) + frame.sh[7].rgb * (1.092548 * x * z)
         + frame.sh[8].rgb * (0.546274 * (x * x - y * y));
}

// envBRDF approximates the split-sum specular scale and bias for a
// Fresnel colour, roughness and view angle (Karis, mobile form).
vec3 envBRDF(vec3 f0, float roughness, float NoV) {
    const vec4 c0 = vec4(-1.0, -0.0275, -0.572, 0.022);
    const vec4 c1 = vec4(1.0, 0.0425, 1.04, -0.04);
    vec4 r = roughness * c0 + c1;
    float a004 = min(r.x * r.x, exp2(-9.28 * NoV)) * r.x + r.y;
    vec2 ab = vec2(-1.04, 1.04) * a004 + r.zw;
    return f0 * ab.x + ab.y;
}

// skyRadiance is the procedural sky's light from a direction, without
// the sun: the atmosphere's gradient above the horizon and the ground or
// planet below, blurred towards the mean for rough reflections. The
// background shader draws the same gradient.
vec3 skyRadiance(vec3 d, float roughness) {
    float up = dot(d, frame.skyUp.xyz);
    float air = frame.horizon.w;
    vec3 above = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    vec3 below = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    vec3 color = up >= 0.0 ? above : below;
    return mix(color, frame.sh[0].rgb * 0.282095, roughness * 0.8);
}

// envSpecular is the environment's reflection along r for a roughness,
// from the prefiltered map or the procedural sky.
vec3 envSpecular(vec3 r, float roughness) {
    if (frame.env.z > 1.5) return skyRadiance(r, roughness);
    return textureLod(envMap, r, roughness * (frame.env.y - 1.0)).rgb * frame.env.x;
}

// envDiffuse is the ambient irradiance for a normal.
vec3 envDiffuse(vec3 n) {
    return irradiance(n) * frame.env.x;
}

// ambient is light from everywhere: the environment map when one is set
// (image-based lighting), otherwise the sky and ground hemisphere, over
// every lobe.
vec3 ambient(Surface s, vec3 n, vec3 v) {
    float NoV = max(dot(n, v), 1e-4);
    vec3 f0 = mix(vec3(0.04), s.albedo, s.metallic);
    vec3 kS = f0 + (max(vec3(1.0 - s.roughness), f0) - f0) * pow(1.0 - NoV, 5.0);
    vec3 kD = (1.0 - kS) * (1.0 - s.metallic);
    vec3 r = reflect(-v, n);
    vec3 color = kD * s.albedo * envDiffuse(n) * (1.0 - s.transmission);
    if (s.transmission > 0.0) {
        color += kD * s.transmission * transmitted(s, n, v);
    }
    color += envSpecular(r, s.roughness) * envBRDF(f0, s.roughness, NoV);
    if (dot(s.sheen, s.sheen) > 0.0) {
        color += s.sheen * envDiffuse(n) * 0.25 * (1.0 - s.sheenRoughness * 0.5);
    }
    if (s.clearcoat > 0.0) {
        vec3 Fc = F_Schlick(NoV, vec3(0.04)) * s.clearcoat;
        color = color * (1.0 - Fc) + envSpecular(r, s.clearcoatRoughness) * Fc;
    }
    if (s.subsurface > 0.0) {
        color += s.albedo * envDiffuse(-n) * 0.5 * (1.0 - s.thickness) * s.subsurface;
    }
    return color;
}

void surface(inout Surface s);
vec4 finish(vec4 lit, Surface s);
