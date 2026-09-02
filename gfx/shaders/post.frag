#version 450

// Final composite: exposure, bloom, ACES tone mapping, vignette. The
// swapchain is sRGB, so the output stays linear here.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D bloom;
layout(set = 0, binding = 2) uniform sampler2D ao;
layout(set = 1, binding = 0) uniform sampler2D lut; // colour grading strip: n slices of n by n
layout(push_constant) uniform PC {
    vec4 a; // x exposure, y bloom strength, z vignette, w saturation
    vec4 b; // x contrast, y ambient occlusion strength, z show occlusion, w LUT strength
} pc;

// grade looks a colour up in the LUT strip, in gamma space where LUTs
// are authored, blending between the two nearest blue slices.
vec3 grade(vec3 c) {
    float n = float(textureSize(lut, 0).y);
    vec3 s = pow(clamp(c, 0.0, 1.0), vec3(1.0 / 2.2));
    float b = s.b * (n - 1.0);
    float s0 = floor(b);
    float s1 = min(s0 + 1.0, n - 1.0);
    vec2 uv = vec2((s.r * (n - 1.0) + 0.5) / (n * n), (s.g * (n - 1.0) + 0.5) / n);
    vec3 lo = texture(lut, uv + vec2(s0 / n, 0.0)).rgb;
    vec3 hi = texture(lut, uv + vec2(s1 / n, 0.0)).rgb;
    return pow(mix(lo, hi, b - s0), vec3(2.2));
}

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

vec3 aces(vec3 x) {
    const float a = 2.51, b = 0.03, c = 2.43, d = 0.59, e = 0.14;
    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), 0.0, 1.0);
}

void main() {
    if (pc.b.z > 0.5) { // debug view: occlusion only
        outColor = vec4(vec3(texture(ao, vUV).r), 1.0);
        return;
    }
    vec3 c = texture(scene, vUV).rgb * pc.a.x;
    c *= mix(1.0, pow(texture(ao, vUV).r, 4.0), pc.b.y); // shaped occlusion, blended by strength
    c += texture(bloom, vUV).rgb * pc.a.y;
    c = aces(c);
    float lum = dot(c, vec3(0.2126, 0.7152, 0.0722));
    c = mix(vec3(lum), c, pc.a.w);
    c = (c - 0.5) * pc.b.x + 0.5;
    if (pc.b.w > 0.0) c = mix(c, grade(c), pc.b.w);
    vec2 d = vUV - 0.5;
    c *= 1.0 - pc.a.z * dot(d, d) * 2.0;
    outColor = vec4(clamp(c, 0.0, 1.0), 1.0);
}
