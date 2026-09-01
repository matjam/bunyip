#version 450

// Final composite: exposure, bloom, ACES tone mapping, vignette. The
// swapchain is sRGB, so the output stays linear here.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D bloom;
layout(push_constant) uniform PC {
    vec4 a; // x exposure, y bloom strength, z vignette, w saturation
    vec4 b; // x contrast, y gamma unused
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

vec3 aces(vec3 x) {
    const float a = 2.51, b = 0.03, c = 2.43, d = 0.59, e = 0.14;
    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), 0.0, 1.0);
}

void main() {
    vec3 c = texture(scene, vUV).rgb * pc.a.x;
    c += texture(bloom, vUV).rgb * pc.a.y;
    c = aces(c);
    float lum = dot(c, vec3(0.2126, 0.7152, 0.0722));
    c = mix(vec3(lum), c, pc.a.w);
    c = (c - 0.5) * pc.b.x + 0.5;
    vec2 d = vUV - 0.5;
    c *= 1.0 - pc.a.z * dot(d, d) * 2.0;
    outColor = vec4(clamp(c, 0.0, 1.0), 1.0);
}
