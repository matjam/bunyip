#version 450

layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 lightViewProj;
    vec4 camPos;
    vec4 lightDir;     // direction the light travels
    vec4 lightColor;   // rgb, w = shadow strength
    vec4 ambient;      // rgb, w = point light count
    vec4 params;       // x = shadow map size, y = shadows enabled
    vec4 pointPos[8];  // xyz, w = range
    vec4 pointColor[8];
} frame;

layout(push_constant) uniform PC {
    mat4 model;
    vec4 baseColor;
    vec4 material;     // x metallic, y roughness, z emissive strength, w normal map on
} pc;

layout(location = 0) out vec3 vWorldPos;
layout(location = 1) out vec3 vNormal;
layout(location = 2) out vec2 vUV;
layout(location = 3) out vec4 vShadowPos;

void main() {
    vec4 world = pc.model * vec4(iPos, 1.0);
    gl_Position = frame.viewProj * world;
    vWorldPos = world.xyz;
    vNormal = normalize(mat3(pc.model) * iNormal);
    vUV = iUV;
    vShadowPos = frame.lightViewProj * world;
}
