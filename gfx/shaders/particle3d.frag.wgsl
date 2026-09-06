var<private> fragCoordValue: vec4f;

// 3D particles draw after the scene's depth has been written, in a pass
// with no depth attachment, so the depth image is readable here. The
// test against it is done by hand, which is what makes the soft fade
// possible: instead of a hard edge where a quad meets geometry, the
// particle fades over the last few units in front of it.
@group(0) @binding(0) var depthTex: texture_2d<f32>;
@group(0) @binding(1) var depthTexSampler: sampler;
@group(1) @binding(0) var tex: texture_2d<f32>;
@group(1) @binding(1) var texSampler: sampler;

struct PC {
    viewProj: mat4x4f,
    right: vec4f,
    up: vec4f,
    params: vec4f, // x near plane, y far plane, z soft fade distance
    mode: vec4f, // x is 1 for an orthographic camera, 0 for a perspective one
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> vColor: vec4f;
var<private> vViewZ: f32;
var<private> outColor: vec4f;

// linearize turns a depth-buffer value into a distance from the camera.
// An orthographic projection spreads depth evenly; a perspective one
// packs it towards the near plane.
fn linearize(d: f32) -> f32 {
    var n: f32 = pc.params.x;
    var f: f32 = pc.params.y;
    if (pc.mode.x > 0.5) { return n + d * (f - n); }
    return n * f / (f + d * (n - f));
}

fn effect() {
    var size: vec2f = vec2f(textureDimensions(depthTex, 0));
    var sceneZ: f32 = linearize(textureSampleLevel(depthTex, depthTexSampler, fragCoordValue.xy / size, 0.0).r);
    // Sampling before discard preserves the texture's implicit mip gradients.
    var c: vec4f = textureSample(tex, texSampler, vUV) * vec4f(vColor.rgb * vColor.a, vColor.a);
    if (sceneZ <= vViewZ) { discard; } // behind the scene
    var fade: f32 = 1.0;
    if (pc.params.z > 0.0) { fade = clamp((sceneZ - vViewZ) / pc.params.z, 0.0, 1.0); }
    outColor = c * fade;
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f, @location(1) vColorIn: vec4f, @location(2) vViewZIn: f32, @builtin(position) fragCoord: vec4f) -> EffectOutput {
    vUV = vUVIn;
    vColor = vColorIn;
    vViewZ = vViewZIn;
    fragCoordValue = fragCoord;
    effect();
    return EffectOutput(outColor);
}
