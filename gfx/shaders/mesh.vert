#version 450

layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    vec4 camPos;
    vec4 lightDir;   // direction the light travels, world space
    vec4 lightColor;
    vec4 ambient;
} frame;

layout(push_constant) uniform PC {
    mat4 model;
    vec4 baseColor;
} pc;

layout(location = 0) out vec3 vWorldPos;
layout(location = 1) out vec3 vNormal;
layout(location = 2) out vec2 vUV;

void main() {
    vec4 world = pc.model * vec4(iPos, 1.0);
    gl_Position = frame.viewProj * world;
    vWorldPos = world.xyz;
    vNormal = mat3(pc.model) * iNormal;
    vUV = iUV;
}
