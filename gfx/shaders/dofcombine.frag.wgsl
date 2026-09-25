var<private> fragCoordValue: vec4f;

// Depth of field, the combine half: each full-size pixel mixes the sharp
// scene with the half-resolution gather from dof.frag by how far it is
// from the focus plane, so a pixel in focus keeps every detail it had.
// The four gather texels around a pixel are blended bilinearly, but one
// whose depth is far from the pixel's own counts for little, so a sharp
// edge in front of a blurred background does not leave a halo of itself.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
@group(0) @binding(2) var blurred: texture_2d<f32>;
@group(0) @binding(3) var blurredSampler: sampler; // the gather, at half size
@group(0) @binding(4) var depthTex: texture_2d<f32>;
@group(0) @binding(5) var depthTexSampler: sampler; // the full-size depth
@group(0) @binding(6) var halfDepth: texture_2d<f32>;
@group(0) @binding(7) var halfDepthSampler: sampler; // the depth the gather ran at

struct PC {
    matrix: mat4x4f, // the inverse projection, for view-space distance from depth
    a: vec4f, // z = focus distance, w = focus range
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn distanceAt(uv: vec2f, d: f32) -> f32 {
    var v: vec4f = pc.matrix * vec4f(uv * 2.0 - 1.0, d, 1.0);
    return -v.z / v.w;
}

fn effect() {
    var px: vec2i = vec2i(fragCoordValue.xy);
    var sharp: vec3f = textureLoad(scene, px, 0).rgb;
    var dist: f32 = distanceAt(vUV, textureLoad(depthTex, px, 0).r);
    var range: f32 = max(pc.a.w, 1e-4);
    var here: f32 = clamp((abs(dist - pc.a.z) - range) / range, 0.0, 1.0);
    outColor = vec4f(sharp, 1.0);
    if (here <= 0.0) { return; }
    var hsize: vec2i = vec2i(textureDimensions(blurred, 0));
    var hp: vec2f = fragCoordValue.xy * 0.5 - 0.5;
    var base: vec2i = vec2i(floor(hp));
    var f: vec2f = hp - floor(hp);
    var sum: vec3f = vec3f(0.0);
    var total: f32 = 0.0;
    for (var j: i32 = 0; j < 4; j++) {
        var o: vec2i = vec2i(j & 1, j >> 1);
        var t: vec2i = clamp(base + o, vec2i(0), hsize - vec2i(1));
        var tuv: vec2f = (vec2f(t) + 0.5) / vec2f(hsize);
        var bx: f32 = select(1.0 - f.x, f.x, o.x == 1);
        var by: f32 = select(1.0 - f.y, f.y, o.y == 1);
        var dz: f32 = abs(distanceAt(tuv, textureLoad(halfDepth, t, 0).r) - dist) / max(dist, 1e-3);
        var w: f32 = bx * by / (1.0 + 2500.0 * dz * dz) + 1e-5;
        sum += textureLoad(blurred, t, 0).rgb * w;
        total += w;
    }
    outColor = vec4f(mix(sharp, sum / total, here), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f, @builtin(position) fragCoord: vec4f) -> EffectOutput {
    vUV = vUVIn;
    fragCoordValue = fragCoord;
    effect();
    return EffectOutput(outColor);
}
