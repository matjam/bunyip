// Final composite: lens distortion and chromatic aberration on the way
// in, then exposure, bloom, light shafts, ACES tone mapping, the grade,
// grain and vignette. The swapchain is sRGB, so the output stays linear
// here. In 2D mode (pc.d.y) exposure and tone mapping are skipped, so a
// 2D game with no post settings on gets the colours it drew.
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var sceneSampler: sampler;
@group(0) @binding(2) var bloom: texture_2d<f32>;
@group(0) @binding(3) var bloomSampler: sampler;
@group(0) @binding(4) var ao: texture_2d<f32>;
@group(0) @binding(5) var aoSampler: sampler;
@group(0) @binding(6) var rays: texture_2d<f32>;
@group(0) @binding(7) var raysSampler: sampler;
@group(1) @binding(0) var lut: texture_2d<f32>;
@group(1) @binding(1) var lutSampler: sampler; // colour grading strip: n slices of n by n
struct PC {
    a: vec4f, // x exposure, y bloom strength, z vignette, w saturation
    b: vec4f, // x contrast, y ambient occlusion strength, z show occlusion, w LUT strength
    c: vec4f, // x aberration, y distortion, z grain, w grain seed
    d: vec4f, // x ghost strength, y 2D mode
}
var<push_constant> pc: PC;

// grade looks a colour up in the LUT strip, in gamma space where LUTs
// are authored, blending between the two nearest blue slices.
fn grade(c: vec3f) -> vec3f {
    var n: f32 = f32(textureDimensions(lut, 0).y);
    var s: vec3f = pow(clamp(c, vec3f(0.0), vec3f(1.0)), vec3f(1.0 / 2.2));
    var b: f32 = s.b * (n - 1.0);
    var s0: f32 = floor(b);
    var s1: f32 = min(s0 + 1.0, n - 1.0);
    var uv: vec2f = vec2f((s.r * (n - 1.0) + 0.5) / (n * n), (s.g * (n - 1.0) + 0.5) / n);
    var lo: vec3f = textureSample(lut, lutSampler, uv + vec2f(s0 / n, 0.0)).rgb;
    var hi: vec3f = textureSample(lut, lutSampler, uv + vec2f(s1 / n, 0.0)).rgb;
    return pow(mix(lo, hi, b - s0), vec3f(2.2));
}

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn aces(x: vec3f) -> vec3f {
    let a: f32 = 2.51;
    let b: f32 = 0.03;
    let c: f32 = 2.43;
    let d: f32 = 0.59;
    let e: f32 = 0.14;
    return clamp((x * (a * x + vec3f(b))) / (x * (c * x + vec3f(d)) + vec3f(e)), vec3f(0.0), vec3f(1.0));
}

// noise is a cheap per-pixel hash, moved each frame by the seed.
fn noise(p: vec2f, seed: f32) -> f32 {
    return fract(sin(dot(p + seed, vec2f(12.9898, 78.233))) * 43758.5453);
}

fn effect() {
    if (pc.b.z > 0.5) { // debug view: occlusion only
        outColor = vec4f(vec3f(textureSample(ao, aoSampler, vUV).r), 1.0);
        return;
    }
    var flat2D: bool = pc.d.y > 0.5;
    // The lens bends the image before anything samples it: a positive
    // distortion pushes pixels out (barrel), a negative one pulls them in.
    var off: vec2f = vUV - 0.5;
    var uv: vec2f = vUV;
    if (pc.c.y != 0.0) { uv = 0.5 + off * (1.0 + pc.c.y * dot(off, off)); }
    var c: vec3f;
    if (pc.c.x > 0.0) {
        // Chromatic aberration: red and blue sampled either side of green,
        // further apart towards the edge of the frame.
        var shift: vec2f = off * pc.c.x * 0.005;
        c = vec3f(textureSample(scene, sceneSampler, uv + shift).r, textureSample(scene, sceneSampler, uv).g, textureSample(scene, sceneSampler, uv - shift).b);
    } else {
        c = textureSample(scene, sceneSampler, uv).rgb;
    }
    if (!flat2D) { c *= pc.a.x; }
    c *= mix(1.0, pow(textureSample(ao, aoSampler, uv).r, 4.0), pc.b.y); // shaped occlusion, blended by strength
    var glow: vec3f = textureSample(bloom, bloomSampler, uv).rgb;
    c += glow * pc.a.y;
    c += textureSample(rays, raysSampler, uv).rgb;
    if (pc.d.x > 0.0) {
        // Lens ghosts: the bright pass mirrored through the centre a few
        // times over, each copy dimmer than the last.
        var toCentre: vec2f = vec2f(0.5) - uv;
        var ghost: vec3f = vec3f(0.0);
        for (var k: i32 = 1; k <= 3; k++) {
            ghost += textureSample(bloom, bloomSampler, uv + toCentre * (f32(k) * 0.55)).rgb / f32(k);
        }
        c += ghost * pc.d.x;
    }
    if (!flat2D) { c = aces(c); }
    var lum: f32 = dot(c, vec3f(0.2126, 0.7152, 0.0722));
    c = mix(vec3f(lum), c, pc.a.w);
    c = (c - 0.5) * pc.b.x + 0.5;
    if (pc.b.w > 0.0) { c = mix(c, grade(c), pc.b.w); }
    if (pc.c.z > 0.0) { c += (noise(vUV * 1024.0, pc.c.w) - 0.5) * pc.c.z; }
    c *= 1.0 - pc.a.z * dot(off, off) * 2.0;
    outColor = vec4f(clamp(c, vec3f(0.0), vec3f(1.0)), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
