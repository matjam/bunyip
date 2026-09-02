#version 450

// The environment as the background: each pixel looks up the cube map
// along its view ray.
layout(set = 0, binding = 0) uniform samplerCube envMap;

layout(push_constant) uniform PC {
    mat4 invViewProj;
    vec4 params; // x intensity
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    vec2 ndc = vUV * 2.0 - 1.0;
    vec4 near = pc.invViewProj * vec4(ndc, 0.0, 1.0);
    vec4 far = pc.invViewProj * vec4(ndc, 1.0, 1.0);
    vec3 dir = normalize(far.xyz / far.w - near.xyz / near.w);
    outColor = vec4(textureLod(envMap, dir, 0.0).rgb * pc.params.x, 1.0);
}
