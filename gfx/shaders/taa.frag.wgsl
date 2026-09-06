// Temporal resolve: reproject the last frame's resolved image into this
// one, clip it to the neighbourhood of the pixels around it so a
// disocclusion or a badly reprojected sample cannot ghost, and blend.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
@group(0) @binding(2) var history: texture_2d<f32>;
@group(0) @binding(3) var historySampler: sampler;
@group(0) @binding(4) var velocity: texture_2d<f32>;
@group(0) @binding(5) var velocitySampler: sampler;
@group(0) @binding(6) var depthTex: texture_2d<f32>;
@group(0) @binding(7) var depthTexSampler: sampler;

struct PC {
    reproject: mat4x4f, // this frame's clip space back to the previous frame's
    a: vec4f, // xy = 1 / size, z = blend towards the new frame, w = history is valid
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn luma(c: vec3f) -> f32 { return dot(c, vec3f(0.2126, 0.7152, 0.0722)); }

fn effect() {
    var cur: vec3f = textureSampleLevel(scene, sceneSampler, vUV, 0.0).rgb;
    if (pc.a.w < 0.5) {
        outColor = vec4f(cur, 1.0);
        return;
    }
    var depth: f32 = textureSampleLevel(depthTex, depthTexSampler, vUV, 0.0).r;
    var clip: vec4f = pc.reproject * vec4f(vUV * 2.0 - 1.0, depth, 1.0);
    var prevUV: vec2f = (clip.xy / clip.w) * 0.5 + 0.5 - textureSampleLevel(velocity, velocitySampler, vUV, 0.0).rg;
    if (prevUV.x < 0.0 || prevUV.x > 1.0 || prevUV.y < 0.0 || prevUV.y > 1.0) {
        outColor = vec4f(cur, 1.0);
        return;
    }
    // Variance clipping: the mean and deviation of the nine pixels around
    // this one bound what the history is allowed to say.
    var px: vec2f = pc.a.xy;
    var m1: vec3f = vec3f(0.0);
    var m2: vec3f = vec3f(0.0);
    for (var y: i32 = -1; y <= 1; y++) {
        for (var x: i32 = -1; x <= 1; x++) {
            var s: vec3f = textureSampleLevel(scene, sceneSampler, vUV + vec2f(f32(x), f32(y)) * px, 0.0).rgb;
            m1 += s;
            m2 += s * s;
        }
    }
    var mu: vec3f = m1 / 9.0;
    var sigma: vec3f = sqrt(max(m2 / 9.0 - mu * mu, vec3f(0.0)));
    var old: vec3f = clamp(textureSampleLevel(history, historySampler, prevUV, 0.0).rgb, mu - sigma, mu + sigma);
    // Weight both samples by the inverse of their brightness so one very
    // bright pixel cannot dominate the average and flicker.
    var blend: f32 = clamp(pc.a.z, 0.02, 1.0);
    var wn: f32 = blend / (1.0 + luma(cur));
    var wo: f32 = (1.0 - blend) / (1.0 + luma(old));
    outColor = vec4f((cur * wn + old * wo) / max(wn + wo, 1e-5), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
