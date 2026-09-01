#version 450

// Metallic-roughness PBR: Cook-Torrance with GGX distribution, Schlick
// Fresnel and Smith-GGX visibility; one shadowed directional light, up to
// eight point lights, and a roughness-aware ambient term.
layout(set = 0, binding = 0) uniform sampler2D albedoTex;
layout(set = 0, binding = 1) uniform sampler2D metalRoughTex; // G roughness, B metallic (glTF)
layout(set = 0, binding = 2) uniform sampler2D normalTex;
layout(set = 0, binding = 3) uniform sampler2D emissiveTex;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3]; // shadow cascades
    vec4 camPos;
    vec4 lightDir;     // direction the light travels
    vec4 lightColor;   // rgb, w = shadow strength
    vec4 sky;          // rgb ambient from above
    vec4 ground;       // rgb ambient from below
    vec4 params;       // x = shadow map size, y = shadows enabled, z = point light count
    vec4 splits;       // view-space distances where cascades end
    vec4 radii;        // half-size of each cascade's orthographic box
    vec4 pointPos[8];  // xyz, w = range
    vec4 pointColor[8];
} frame;

layout(set = 2, binding = 0) uniform sampler2DShadow shadowMap0;
layout(set = 2, binding = 1) uniform sampler2DShadow shadowMap1;
layout(set = 2, binding = 2) uniform sampler2DShadow shadowMap2;


layout(location = 0) in vec3 vWorldPos;
layout(location = 1) in vec3 vNormal;
layout(location = 2) in vec2 vUV;
layout(location = 3) in float vViewDepth;
layout(location = 4) flat in vec4 vBaseColor;
layout(location = 5) flat in vec4 vMaterial;
layout(location = 0) out vec4 outColor;

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

void main() {
    vec4 albedoSample = texture(albedoTex, vUV) * vBaseColor;
    vec3 albedo = albedoSample.rgb;
    vec3 mr = texture(metalRoughTex, vUV).rgb;
    float metallic = clamp(vMaterial.x * mr.b, 0.0, 1.0);
    float roughness = clamp(vMaterial.y * mr.g, 0.04, 1.0);
    vec3 n = normalize(vNormal);
    if (!gl_FrontFacing) n = -n;
    if (vMaterial.w > 0.5) n = perturbNormal(n, vWorldPos, vUV);
    vec3 v = normalize(frame.camPos.xyz - vWorldPos);

    vec3 l = normalize(-frame.lightDir.xyz);
    float shadow = mix(1.0, shadowFactor(n, l), frame.lightColor.w);
    vec3 color = shade(n, v, l, frame.lightColor.rgb * shadow, albedo, metallic, roughness);

    int count = int(frame.params.z);
    for (int i = 0; i < count; i++) {
        vec3 d = frame.pointPos[i].xyz - vWorldPos;
        float dist = length(d);
        float range = max(frame.pointPos[i].w, 1e-3);
        float att = clamp(1.0 - (dist * dist) / (range * range), 0.0, 1.0);
        att *= att / max(dist * dist, 1e-3);
        color += shade(n, v, d / dist, frame.pointColor[i].rgb * att, albedo, metallic, roughness);
    }

    // Hemisphere ambient: sky from above, ground from below, plus a
    // Fresnel-weighted specular from the reflected direction's sky.
    float NoV = max(dot(n, v), 1e-4);
    vec3 f0 = mix(vec3(0.04), albedo, metallic);
    vec3 kS = f0 + (max(vec3(1.0 - roughness), f0) - f0) * pow(1.0 - NoV, 5.0);
    vec3 ambientDiffuse = (1.0 - kS) * (1.0 - metallic) * albedo * mix(frame.ground.rgb, frame.sky.rgb, n.y * 0.5 + 0.5);
    vec3 r = reflect(-v, n);
    vec3 env = mix(frame.ground.rgb, frame.sky.rgb, r.y * 0.5 + 0.5);
    vec3 ambientSpec = kS * env * (1.0 - roughness * 0.8);
    color += ambientDiffuse + ambientSpec;

    color += texture(emissiveTex, vUV).rgb * vMaterial.z;
    outColor = vec4(color, albedoSample.a);
}
