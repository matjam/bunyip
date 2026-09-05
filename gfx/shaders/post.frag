#version 450

// Final composite: lens distortion and chromatic aberration on the way
// in, then exposure, bloom, light shafts, ACES tone mapping, the grade,
// grain and vignette. The swapchain is sRGB, so the output stays linear
// here. In 2D mode (pc.d.y) exposure and tone mapping are skipped, so a
// 2D game with no post settings on gets the colours it drew.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D bloom;
layout(set = 0, binding = 2) uniform sampler2D ao;
layout(set = 0, binding = 3) uniform sampler2D rays;
layout(set = 1, binding = 0) uniform sampler2D lut; // colour grading strip: n slices of n by n
layout(push_constant) uniform PC {
    vec4 a; // x exposure, y bloom strength, z vignette, w saturation
    vec4 b; // x contrast, y ambient occlusion strength, z show occlusion, w LUT strength
    vec4 c; // x aberration, y distortion, z grain, w grain seed
    vec4 d; // x ghost strength, y 2D mode
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

// noise is a cheap per-pixel hash, moved each frame by the seed.
float noise(vec2 p, float seed) {
    return fract(sin(dot(p + seed, vec2(12.9898, 78.233))) * 43758.5453);
}

void main() {
    if (pc.b.z > 0.5) { // debug view: occlusion only
        outColor = vec4(vec3(texture(ao, vUV).r), 1.0);
        return;
    }
    bool flat2D = pc.d.y > 0.5;
    // The lens bends the image before anything samples it: a positive
    // distortion pushes pixels out (barrel), a negative one pulls them in.
    vec2 off = vUV - 0.5;
    vec2 uv = vUV;
    if (pc.c.y != 0.0) uv = 0.5 + off * (1.0 + pc.c.y * dot(off, off));
    vec3 c;
    if (pc.c.x > 0.0) {
        // Chromatic aberration: red and blue sampled either side of green,
        // further apart towards the edge of the frame.
        vec2 shift = off * pc.c.x * 0.02;
        c = vec3(texture(scene, uv + shift).r, texture(scene, uv).g, texture(scene, uv - shift).b);
    } else {
        c = texture(scene, uv).rgb;
    }
    if (!flat2D) c *= pc.a.x;
    c *= mix(1.0, pow(texture(ao, uv).r, 4.0), pc.b.y); // shaped occlusion, blended by strength
    vec3 glow = texture(bloom, uv).rgb;
    c += glow * pc.a.y;
    c += texture(rays, uv).rgb;
    if (pc.d.x > 0.0) {
        // Lens ghosts: the bright pass mirrored through the centre a few
        // times over, each copy dimmer than the last.
        vec2 toCentre = vec2(0.5) - uv;
        vec3 ghost = vec3(0.0);
        for (int k = 1; k <= 3; k++) {
            ghost += texture(bloom, uv + toCentre * (float(k) * 0.55)).rgb / float(k);
        }
        c += ghost * pc.d.x;
    }
    if (!flat2D) c = aces(c);
    float lum = dot(c, vec3(0.2126, 0.7152, 0.0722));
    c = mix(vec3(lum), c, pc.a.w);
    c = (c - 0.5) * pc.b.x + 0.5;
    if (pc.b.w > 0.0) c = mix(c, grade(c), pc.b.w);
    if (pc.c.z > 0.0) c += (noise(vUV * 1024.0, pc.c.w) - 0.5) * pc.c.z;
    c *= 1.0 - pc.a.z * dot(off, off) * 2.0;
    outColor = vec4(clamp(c, 0.0, 1.0), 1.0);
}
