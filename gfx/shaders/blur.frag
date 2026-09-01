#version 450

// Separable 9-tap Gaussian; a.xy is the step in UV per tap.
layout(set = 0, binding = 0) uniform sampler2D src;
layout(push_constant) uniform PC { vec4 a; vec4 b; } pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    const float w[5] = float[](0.227027, 0.1945946, 0.1216216, 0.054054, 0.016216);
    vec3 c = texture(src, vUV).rgb * w[0];
    for (int i = 1; i < 5; i++) {
        c += texture(src, vUV + pc.a.xy * float(i)).rgb * w[i];
        c += texture(src, vUV - pc.a.xy * float(i)).rgb * w[i];
    }
    outColor = vec4(c, 1.0);
}
