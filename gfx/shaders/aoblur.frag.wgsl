// 4x4 box blur that removes the SSAO kernel's rotation noise.
@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var srcSampler: sampler;
struct PC {
    a: vec4f,
    b: vec4f,
}
var<push_constant> pc: PC; // a.xy = texel size

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var sum: f32 = 0.0;
    for (var y: i32 = -2; y < 2; y++) {
        for (var x: i32 = -2; x < 2; x++) {
            sum += textureSampleLevel(src, srcSampler, vUV + (vec2f(f32(x), f32(y)) + vec2f(0.5)) * pc.a.xy, 0.0).r;
        }
    }
    outColor = vec4f(vec3f(sum / 16.0), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
