#version 450

// FXAA (Lottes), console-quality variant: blends along the local edge
// direction where neighbouring luma contrast is high.
layout(set = 0, binding = 0) uniform sampler2D src;
layout(push_constant) uniform PC { vec4 a; vec4 b; } pc; // a.xy = 1/size

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

float luma(vec3 c) { return sqrt(dot(c, vec3(0.299, 0.587, 0.114))); }

void main() {
    vec2 px = pc.a.xy;
    vec3 rgbM = texture(src, vUV).rgb;
    float lM = luma(rgbM);
    float lNW = luma(texture(src, vUV + vec2(-1, -1) * px).rgb);
    float lNE = luma(texture(src, vUV + vec2(1, -1) * px).rgb);
    float lSW = luma(texture(src, vUV + vec2(-1, 1) * px).rgb);
    float lSE = luma(texture(src, vUV + vec2(1, 1) * px).rgb);
    float lMin = min(lM, min(min(lNW, lNE), min(lSW, lSE)));
    float lMax = max(lM, max(max(lNW, lNE), max(lSW, lSE)));
    if (lMax - lMin < max(0.0312, lMax * 0.125)) {
        outColor = vec4(rgbM, 1.0);
        return;
    }
    vec2 dir = vec2(-((lNW + lNE) - (lSW + lSE)), (lNW + lSW) - (lNE + lSE));
    float dirReduce = max((lNW + lNE + lSW + lSE) * 0.03125, 0.0078125);
    float rcp = 1.0 / (min(abs(dir.x), abs(dir.y)) + dirReduce);
    dir = clamp(dir * rcp, -8.0, 8.0) * px;
    vec3 rgbA = 0.5 * (texture(src, vUV + dir * (1.0 / 3.0 - 0.5)).rgb + texture(src, vUV + dir * (2.0 / 3.0 - 0.5)).rgb);
    vec3 rgbB = rgbA * 0.5 + 0.25 * (texture(src, vUV + dir * -0.5).rgb + texture(src, vUV + dir * 0.5).rgb);
    float lB = luma(rgbB);
    outColor = vec4((lB < lMin || lB > lMax) ? rgbA : rgbB, 1.0);
}
