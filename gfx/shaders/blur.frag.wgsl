// Separable 9-tap Gaussian; a.xy is the step in UV per tap.
@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var srcSampler: sampler;
struct PC {
    a: vec4f,
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    let w: array<f32, 5> = array<f32, 5>(0.227027, 0.1945946, 0.1216216, 0.054054, 0.016216);
    var c: vec3f = textureSample(src, srcSampler, vUV).rgb * w[0];
    for (var i: i32 = 1; i < 5; i++) {
        c += textureSample(src, srcSampler, vUV + pc.a.xy * f32(i)).rgb * w[i];
        c += textureSample(src, srcSampler, vUV - pc.a.xy * f32(i)).rgb * w[i];
    }
    outColor = vec4f(c, 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
