#version 450

// Bright pass: keeps HDR energy above the threshold for bloom, at reduced resolution.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(push_constant) uniform PC { vec4 a; vec4 b; } pc; // a.x threshold, a.y knee

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    vec3 c = texture(scene, vUV).rgb;
    float l = max(c.r, max(c.g, c.b));
    float t = pc.a.x;
    float k = max(pc.a.y, 1e-4);
    float soft = clamp(l - t + k, 0.0, 2.0 * k);
    soft = soft * soft / (4.0 * k);
    float contribution = max(soft, l - t) / max(l, 1e-4);
    outColor = vec4(c * contribution, 1.0);
}
