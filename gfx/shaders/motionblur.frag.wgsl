// Motion blur: each pixel is smeared back along the way it travelled
// since the last frame. The camera's part of that comes from reprojecting
// the pixel's depth, the object's part from the velocity buffer.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
@group(0) @binding(2) var velocity: texture_2d<f32>;
@group(0) @binding(3) var velocitySampler: sampler;
@group(0) @binding(4) var depthTex: texture_2d<f32>;
@group(0) @binding(5) var depthTexSampler: sampler;

struct PC {
    matrix: mat4x4f, // this frame's clip space back to the previous frame's
    a: vec4f, // xy = 1 / size, z = strength, w = sample count
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var depth: f32 = textureSampleLevel(depthTex, depthTexSampler, vUV, 0.0).r;
    var clip: vec4f = pc.matrix * vec4f(vUV * 2.0 - 1.0, depth, 1.0);
    var prevUV: vec2f = (clip.xy / clip.w) * 0.5 + 0.5 - textureSampleLevel(velocity, velocitySampler, vUV, 0.0).rg;
    var motion: vec2f = (vUV - prevUV) * pc.a.z;
    // Below a tenth of a pixel there is nothing to smear.
    if (dot(motion, motion) < dot(pc.a.xy * 0.1, pc.a.xy * 0.1)) {
        outColor = vec4f(textureSampleLevel(scene, sceneSampler, vUV, 0.0).rgb, 1.0);
        return;
    }
    var taps: i32 = max(i32(pc.a.w), 2);
    var sum: vec3f = vec3f(0.0);
    for (var i: i32 = 0; i < taps; i++) {
        var uv: vec2f = clamp(vUV - motion * (f32(i) / f32(taps - 1)), vec2f(0.0), vec2f(1.0));
        sum += textureSampleLevel(scene, sceneSampler, uv, 0.0).rgb;
    }
    outColor = vec4f(sum / f32(taps), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
