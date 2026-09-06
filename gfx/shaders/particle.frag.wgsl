// The texture times the particle's tint. Textures are premultiplied on
// upload and the instance tint is not, so the tint is premultiplied
// here to match the pipeline's ONE, ONE_MINUS_SRC_ALPHA blending.
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var texSampler: sampler;

var<private> vUV: vec2f;
var<private> vColor: vec4f;
var<private> outColor: vec4f;

fn effect() {
    outColor = textureSample(tex, texSampler, vUV) * vec4f(vColor.rgb * vColor.a, vColor.a);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f, @location(1) vColorIn: vec4f) -> EffectOutput {
    vUV = vUVIn;
    vColor = vColorIn;
    effect();
    return EffectOutput(outColor);
}
