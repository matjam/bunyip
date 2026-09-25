var<private> fragCoordValue: vec4f;

// Half-resolution depth: each texel keeps the nearest of the four scene
// depth texels under it, as a single float. Ambient occlusion, the
// screen-space reflection trace, the light shafts and the depth of field
// gather read this image rather than the full-size depth-stencil one, at
// a quarter of the texels and without its packed format.
@group(0) @binding(0) var depthTex: texture_2d<f32>;
@group(0) @binding(1) var depthTexSampler: sampler;

var<private> outColor: vec4f;

fn effect() {
    var size: vec2i = vec2i(textureDimensions(depthTex, 0));
    var c: vec2i = vec2i(fragCoordValue.xy) * 2;
    var hi: vec2i = size - vec2i(1);
    var a: f32 = textureLoad(depthTex, min(c, hi), 0).r;
    var b: f32 = textureLoad(depthTex, min(c + vec2i(1, 0), hi), 0).r;
    var d: f32 = textureLoad(depthTex, min(c + vec2i(0, 1), hi), 0).r;
    var e: f32 = textureLoad(depthTex, min(c + vec2i(1, 1), hi), 0).r;
    outColor = vec4f(min(min(a, b), min(d, e)), 0.0, 0.0, 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@builtin(position) fragCoord: vec4f) -> EffectOutput {
    fragCoordValue = fragCoord;
    effect();
    return EffectOutput(outColor);
}
