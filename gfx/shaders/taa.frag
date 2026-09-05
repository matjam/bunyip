#version 450

// Temporal resolve: reproject the last frame's resolved image into this
// one, clip it to the neighbourhood of the pixels around it so a
// disocclusion or a badly reprojected sample cannot ghost, and blend.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D history;
layout(set = 0, binding = 2) uniform sampler2D velocity;
layout(set = 0, binding = 3) uniform sampler2D depthTex;

layout(push_constant) uniform PC {
    mat4 reproject; // this frame's clip space back to the previous frame's
    vec4 a;         // xy = 1 / size, z = blend towards the new frame, w = history is valid
    vec4 b;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

float luma(vec3 c) { return dot(c, vec3(0.2126, 0.7152, 0.0722)); }

void main() {
    vec3 cur = texture(scene, vUV).rgb;
    if (pc.a.w < 0.5) {
        outColor = vec4(cur, 1.0);
        return;
    }
    float depth = texture(depthTex, vUV).r;
    vec4 clip = pc.reproject * vec4(vUV * 2.0 - 1.0, depth, 1.0);
    vec2 prevUV = (clip.xy / clip.w) * 0.5 + 0.5 - texture(velocity, vUV).rg;
    if (prevUV.x < 0.0 || prevUV.x > 1.0 || prevUV.y < 0.0 || prevUV.y > 1.0) {
        outColor = vec4(cur, 1.0);
        return;
    }
    // Variance clipping: the mean and deviation of the nine pixels around
    // this one bound what the history is allowed to say.
    vec2 px = pc.a.xy;
    vec3 m1 = vec3(0.0), m2 = vec3(0.0);
    for (int y = -1; y <= 1; y++) {
        for (int x = -1; x <= 1; x++) {
            vec3 s = texture(scene, vUV + vec2(x, y) * px).rgb;
            m1 += s;
            m2 += s * s;
        }
    }
    vec3 mu = m1 / 9.0;
    vec3 sigma = sqrt(max(m2 / 9.0 - mu * mu, vec3(0.0)));
    vec3 old = clamp(texture(history, prevUV).rgb, mu - sigma, mu + sigma);
    // Weight both samples by the inverse of their brightness so one very
    // bright pixel cannot dominate the average and flicker.
    float blend = clamp(pc.a.z, 0.02, 1.0);
    float wn = blend / (1.0 + luma(cur));
    float wo = (1.0 - blend) / (1.0 + luma(old));
    outColor = vec4((cur * wn + old * wo) / max(wn + wo, 1e-5), 1.0);
}
