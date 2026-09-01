#version 450

layout(set = 0, binding = 0) uniform sampler2D albedoTex;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 ambient;
} frame;

layout(push_constant) uniform PC {
    mat4 model;
    vec4 baseColor;
} pc;

layout(location = 0) in vec3 vWorldPos;
layout(location = 1) in vec3 vNormal;
layout(location = 2) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    vec4 albedo = texture(albedoTex, vUV) * pc.baseColor;
    vec3 n = normalize(vNormal);
    vec3 l = normalize(-frame.lightDir.xyz);
    vec3 v = normalize(frame.camPos.xyz - vWorldPos);
    vec3 h = normalize(l + v);
    float diffuse = max(dot(n, l), 0.0);
    float spec = pow(max(dot(n, h), 0.0), 32.0) * 0.25 * step(0.0, dot(n, l));
    vec3 color = albedo.rgb * (frame.ambient.rgb + frame.lightColor.rgb * diffuse) + frame.lightColor.rgb * spec;
    outColor = vec4(color, albedo.a);
}
