#version 450

// 4x4 box blur that removes the SSAO kernel's rotation noise.
layout(set = 0, binding = 0) uniform sampler2D src;
layout(push_constant) uniform PC { vec4 a; vec4 b; } pc; // a.xy = texel size

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    float sum = 0.0;
    for (int y = -2; y < 2; y++)
        for (int x = -2; x < 2; x++)
            sum += texture(src, vUV + (vec2(x, y) + 0.5) * pc.a.xy).r;
    outColor = vec4(vec3(sum / 16.0), 1.0);
}
