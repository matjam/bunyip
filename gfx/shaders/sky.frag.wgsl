// The environment as the background: each pixel looks up the cube map
// along its view ray.
@group(0) @binding(0) var envMap: texture_cube<f32>;
@group(0) @binding(1) var envMapSampler: sampler;

struct PC {
    invViewProj: mat4x4f,
    params: vec4f, // x intensity
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var ndc: vec2f = vUV * 2.0 - 1.0;
    var near: vec4f = pc.invViewProj * vec4f(ndc, 0.0, 1.0);
    var far: vec4f = pc.invViewProj * vec4f(ndc, 1.0, 1.0);
    var dir: vec3f = normalize(far.xyz / far.w - near.xyz / near.w);
    outColor = vec4f(textureSampleLevel(envMap, envMapSampler, dir, 0.0).rgb * pc.params.x, 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
