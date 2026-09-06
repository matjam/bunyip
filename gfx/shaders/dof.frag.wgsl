// Depth of field: the circle of confusion grows with the distance from
// the focus plane, and each pixel gathers a disc of samples that wide.
// A sample nearer the camera than the pixel only contributes as much as
// its own blur allows, so a sharp foreground does not smear over the
// background behind it.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
@group(0) @binding(2) var depthTex: texture_2d<f32>;
@group(0) @binding(3) var depthTexSampler: sampler;

struct PC {
    matrix: mat4x4f, // the inverse projection, for view-space distance from depth
    a: vec4f, // xy = 1 / size, z = focus distance, w = focus range
    b: vec4f, // x = blur radius in pixels, y = sample count
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

// viewDistance turns a depth sample into distance in front of the camera.
fn viewDistance(uv: vec2f) -> f32 {
    var v: vec4f = pc.matrix * vec4f(uv * 2.0 - 1.0, textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r, 1.0);
    return -v.z / v.w;
}

// coc is how defocused a surface at that distance is, 0 sharp to 1 fully blurred.
fn coc(dist: f32) -> f32 {
    var range: f32 = max(pc.a.w, 1e-4);
    return clamp((abs(dist - pc.a.z) - range) / range, 0.0, 1.0);
}

fn effect() {
    var sharp: vec3f = textureSampleLevel(scene, sceneSampler, vUV, 0.0).rgb;
    var dist: f32 = viewDistance(vUV);
    var here: f32 = coc(dist);
    if (here <= 0.0) {
        outColor = vec4f(sharp, 1.0);
        return;
    }
    var radius: f32 = here * pc.b.x;
    var taps: i32 = i32(pc.b.y);
    var sum: vec3f = sharp;
    var weight: f32 = 1.0;
    // The disc is the same in every pixel. Turning it per pixel would
    // trade the pattern a fixed spiral leaves on fine detail for noise,
    // but it scatters the texture fetches: measured on an RTX 4090 at
    // 1280 by 720 it cost 2.7 times as much. A wide bokeh takes more
    // taps instead.
    for (var i: i32 = 1; i <= taps; i++) {
        var angle: f32 = f32(i) * 2.39996323; // the golden angle spreads the disc evenly
        var r: f32 = sqrt(f32(i) / f32(taps)) * radius;
        var uv: vec2f = vUV + vec2f(cos(angle), sin(angle)) * r * pc.a.xy;
        var d: f32 = viewDistance(uv);
        // Behind this pixel it contributes fully; in front only as far as
        // its own defocus spreads it.
        var w: f32 = select(coc(d), 1.0, d >= dist);
        sum += textureSampleLevel(scene, sceneSampler, uv, 0.0).rgb * w;
        weight += w;
    }
    outColor = vec4f(mix(sharp, sum / weight, here), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
