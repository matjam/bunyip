// Resolving weighted blended order-independent transparency: the colour
// every translucent fragment accumulated, divided by the weight it
// accumulated with, and the revealage those fragments left. The pipeline
// blends it over the scene, source times one minus the alpha written
// here plus the destination times it, so a pixel no fragment touched
// keeps the scene exactly.
@group(0) @binding(0) var accum: texture_2d<f32>;
@group(0) @binding(1) var accumSampler: sampler;
@group(0) @binding(2) var reveal: texture_2d<f32>;
@group(0) @binding(3) var revealSampler: sampler;

struct PC {
    a: vec4f,
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var a: vec4f = textureSample(accum, accumSampler, vUV);
    outColor = vec4f(a.rgb / max(a.a, 1e-5), textureSample(reveal, revealSampler, vUV).r);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
