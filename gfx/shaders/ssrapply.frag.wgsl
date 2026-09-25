var<private> fragCoordValue: vec4f;

// Applies the half-resolution reflection trace to the full-resolution
// scene, writing a new image rather than blending over the old one, so it
// can read each pixel's own reflection weight from the scene's alpha. The
// four trace texels around a pixel are blended bilinearly, but a texel
// whose depth is far from the pixel's own counts for little, so a
// reflection on the floor does not spread onto the edge of the box
// standing on it.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler; // the scene, alpha = reflection weight
@group(0) @binding(2) var refl: texture_2d<f32>;
@group(0) @binding(3) var reflSampler: sampler; // the trace: colour times fade, fade in alpha
@group(0) @binding(4) var depthTex: texture_2d<f32>;
@group(0) @binding(5) var depthTexSampler: sampler; // the full-resolution depth
@group(0) @binding(6) var halfDepth: texture_2d<f32>;
@group(0) @binding(7) var halfDepthSampler: sampler; // the depth the trace ran at

struct PC {
    a: vec4f,
    b: vec4f, // the projection's z row: [2][2], [3][2], [2][3], [3][3]
    c: vec4f,
    d: vec4f,
}
var<push_constant> pc: PC;

var<private> outColor: vec4f;

fn viewZ(d: f32) -> f32 { return (pc.b.y - d * pc.b.w) / (d * pc.b.z - pc.b.x); }

fn effect() {
    var px: vec2i = vec2i(fragCoordValue.xy);
    var c: vec4f = textureLoad(scene, px, 0);
    outColor = c;
    if (c.a <= 0.002) { return; }
    var d: f32 = textureLoad(depthTex, px, 0).r;
    if (d >= 1.0) { return; }
    var z: f32 = viewZ(d);
    var hsize: vec2i = vec2i(textureDimensions(refl, 0));
    var hp: vec2f = fragCoordValue.xy * 0.5 - 0.5;
    var base: vec2i = vec2i(floor(hp));
    var f: vec2f = hp - floor(hp);
    var sum: vec4f = vec4f(0.0);
    var total: f32 = 0.0;
    for (var j: i32 = 0; j < 4; j++) {
        var o: vec2i = vec2i(j & 1, j >> 1);
        var t: vec2i = clamp(base + o, vec2i(0), hsize - vec2i(1));
        var bx: f32 = select(1.0 - f.x, f.x, o.x == 1);
        var by: f32 = select(1.0 - f.y, f.y, o.y == 1);
        var dz: f32 = abs(viewZ(textureLoad(halfDepth, t, 0).r) - z) / max(abs(z), 1e-3);
        var w: f32 = bx * by / (1.0 + 2500.0 * dz * dz) + 1e-5;
        sum += textureLoad(refl, t, 0) * w;
        total += w;
    }
    var r: vec4f = sum / total;
    var w: f32 = clamp(c.a * r.a, 0.0, 1.0);
    outColor = vec4f(c.rgb * (1.0 - w) + r.rgb * c.a, c.a);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@builtin(position) fragCoord: vec4f) -> EffectOutput {
    fragCoordValue = fragCoord;
    effect();
    return EffectOutput(outColor);
}
