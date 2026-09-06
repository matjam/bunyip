// FXAA (Lottes), console-quality variant: blends along the local edge
// direction where neighbouring luma contrast is high.
@group(0) @binding(0) var src: texture_2d<f32>;
@group(0) @binding(1) var srcSampler: sampler;
struct PC {
    a: vec4f,
    b: vec4f,
}
var<push_constant> pc: PC; // a.xy = 1/size

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn luma(c: vec3f) -> f32 { return sqrt(dot(c, vec3f(0.299, 0.587, 0.114))); }

fn effect() {
    var px: vec2f = pc.a.xy;
    var rgbM: vec3f = textureSampleLevel(src, srcSampler, vUV, 0.0).rgb;
    var lM: f32 = luma(rgbM);
    var lNW: f32 = luma(textureSampleLevel(src, srcSampler, vUV + vec2f(-1, -1) * px, 0.0).rgb);
    var lNE: f32 = luma(textureSampleLevel(src, srcSampler, vUV + vec2f(1, -1) * px, 0.0).rgb);
    var lSW: f32 = luma(textureSampleLevel(src, srcSampler, vUV + vec2f(-1, 1) * px, 0.0).rgb);
    var lSE: f32 = luma(textureSampleLevel(src, srcSampler, vUV + vec2f(1, 1) * px, 0.0).rgb);
    var lMin: f32 = min(lM, min(min(lNW, lNE), min(lSW, lSE)));
    var lMax: f32 = max(lM, max(max(lNW, lNE), max(lSW, lSE)));
    if (lMax - lMin < max(0.0312, lMax * 0.125)) {
        outColor = vec4f(rgbM, 1.0);
        return;
    }
    var dir: vec2f = vec2f(-((lNW + lNE) - (lSW + lSE)), (lNW + lSW) - (lNE + lSE));
    var dirReduce: f32 = max((lNW + lNE + lSW + lSE) * 0.03125, 0.0078125);
    var rcp: f32 = 1.0 / (min(abs(dir.x), abs(dir.y)) + dirReduce);
    dir = clamp(dir * rcp, vec2f(-8.0), vec2f(8.0)) * px;
    var rgbA: vec3f = 0.5 * (textureSampleLevel(src, srcSampler, vUV + dir * (1.0 / 3.0 - 0.5), 0.0).rgb + textureSampleLevel(src, srcSampler, vUV + dir * (2.0 / 3.0 - 0.5), 0.0).rgb);
    var rgbB: vec3f = rgbA * 0.5 + 0.25 * (textureSampleLevel(src, srcSampler, vUV + dir * -0.5, 0.0).rgb + textureSampleLevel(src, srcSampler, vUV + dir * 0.5, 0.0).rgb);
    var lB: f32 = luma(rgbB);
    outColor = vec4f(select(rgbB, rgbA, lB < lMin || lB > lMax), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
