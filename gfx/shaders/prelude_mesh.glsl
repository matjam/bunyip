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
// directional light, up to eight point lights, image-based or hemisphere
// ambient, plus clearcoat, sheen and subsurface lobes when a material
// asks for them.
layout(set = 0, binding = 0) uniform sampler2D albedoTex;
layout(set = 0, binding = 1) uniform sampler2D metalRoughTex; // G roughness, B metallic (glTF)
layout(set = 0, binding = 2) uniform sampler2D normalTex;
layout(set = 0, binding = 3) uniform sampler2D emissiveTex;
layout(set = 0, binding = 4) uniform sampler2D occlusionTex; // R: baked ambient occlusion
layout(set = 0, binding = 5) uniform sampler2D image0;
layout(set = 0, binding = 6) uniform sampler2D image1;
layout(set = 0, binding = 7) uniform sampler2D image2;
layout(set = 0, binding = 8) uniform sampler2D image3;
layout(set = 0, binding = 9) uniform samplerCube envMap;     // prefiltered environment, one level per roughness
layout(set = 0, binding = 10) uniform sampler2D thicknessTex; // R: 1 thick, 0 thin, for subsurface
layout(set = 0, binding = 11) uniform sampler2D sceneTex;     // the opaque scene, blurred down its mips, for transmission

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
    vec4 pointPos[8];  // xyz, w = range
    vec4 pointColor[8];
    vec4 sh[9];        // environment irradiance as spherical harmonics
    vec4 env;          // x intensity, y mip count, z = 1 when an environment is set
    mat4 invViewProj;
    vec4 horizon;      // rgb the sky at the horizon, w = air (1 - vacuum)
    vec4 skyUp;        // xyz up, w = stars
    vec4 sun;          // xyz towards the sun, w = angular radius
    vec4 sunColor;     // rgb the drawn disc's radiance
} frame;

layout(set = 2, binding = 0) uniform sampler2DShadow shadowMap0;
layout(set = 2, binding = 1) uniform sampler2DShadow shadowMap1;
layout(set = 2, binding = 2) uniform sampler2DShadow shadowMap2;

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
layout(location = 13) flat in vec4 vAtten;   // attenuation colour
layout(location = 0) out vec4 outColor;

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

float sampleCascade(int c, vec3 uvz, vec2 texel) {
    float lit = 0.0;
    for (int y = -1; y <= 1; y++)
        for (int x = -1; x <= 1; x++) {
            vec3 p = vec3(uvz.xy + vec2(x, y) * texel, uvz.z);
            lit += c == 0 ? texture(shadowMap0, p) : (c == 1 ? texture(shadowMap1, p) : texture(shadowMap2, p));
        }
    return lit / 9.0;
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
    float texel = 1.0 / frame.params.x;
    return sampleCascade(c, vec3(uv, p.z - bias), vec2(texel));
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

vec3 ambient(Surface s, vec3 n, vec3 v);

// light runs the engine's lighting over a surface: the shadowed
// directional light, the point lights and the ambient term.
vec3 light(Surface s) {
    vec3 n = normalize(s.normal);
    vec3 v = s.viewDir;
    vec3 l = normalize(-frame.lightDir.xyz);
    float shadow = mix(1.0, shadowFactor(n, l), frame.lightColor.w);
    vec3 color = lobes(s, n, v, l, frame.lightColor.rgb * shadow);

    int count = int(frame.params.z);
    for (int i = 0; i < count; i++) {
        vec3 d = frame.pointPos[i].xyz - s.worldPos;
        float dist = length(d);
        float range = max(frame.pointPos[i].w, 1e-3);
        float att = clamp(1.0 - (dist * dist) / (range * range), 0.0, 1.0);
        att *= att / max(dist * dist, 1e-3);
        color += lobes(s, n, v, d / dist, frame.pointColor[i].rgb * att);
    }
    return color + ambient(s, n, v) * s.occlusion;
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
