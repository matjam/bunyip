// Bright pass: keeps HDR energy above the threshold for bloom, at reduced resolution.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
struct PC {
    a: vec4f,
    b: vec4f,
}
var<push_constant> pc: PC; // a.x threshold, a.y knee

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var c: vec3f = textureSample(scene, sceneSampler, vUV).rgb;
    var l: f32 = max(c.r, max(c.g, c.b));
    var t: f32 = pc.a.x;
    var k: f32 = max(pc.a.y, 1e-4);
    var soft: f32 = clamp(l - t + k, 0.0, 2.0 * k);
    soft = soft * soft / (4.0 * k);
    var contribution: f32 = max(soft, l - t) / max(l, 1e-4);
    outColor = vec4f(c * contribution, 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
