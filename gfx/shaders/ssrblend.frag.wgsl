// Blends the half-resolution reflection trace over the scene pass's own
// colour attachment, for a frame whose translucent draws follow the
// reflections in the same pass. The trace already carries each surface's
// weight, so this is a premultiplied source-over of its filtered texel.
@group(0) @binding(0) var refl: texture_2d<f32>;
@group(0) @binding(1) var reflSampler: sampler;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    outColor = textureSampleLevel(refl, reflSampler, vUV, 0.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
