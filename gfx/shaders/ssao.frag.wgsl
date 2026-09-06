var<private> fragCoordValue: vec4f;

// Screen-space ambient occlusion from the scene depth: view positions are
// reconstructed with the inverse projection, normals from neighbouring depths, and a rotated hemisphere kernel tests nearby depth.
@group(0) @binding(0) var depthTex: texture_2d<f32>;
@group(0) @binding(1) var depthTexSampler: sampler;
struct PC {
    proj: mat4x4f,
    invProj: mat4x4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

// proj[3][3] carries the kernel radius (it is 0 in a perspective matrix);
// the projection used here has it restored.
fn viewPos(uv: vec2f) -> vec3f {
    var d: f32 = textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r;
    var p: vec4f = pc.invProj * vec4f(uv * 2.0 - 1.0, d, 1.0);
    return p.xyz / p.w;
}

// viewPosAt reconstructs the position of an exact depth texel. This pass
// runs at half resolution, so its pixel centres fall on full-resolution
// texel boundaries; sampling there rounds unpredictably and tilts the
// finite-difference normal, which false-occludes flat floors in streaks.
fn viewPosAt(pixel: vec2i, size: vec2i) -> vec3f {
    let px = clamp(pixel, vec2i(0), size - vec2i(1));
    var d: f32 = textureLoad(depthTex, px, 0).r;
    var uv: vec2f = (vec2f(px) + 0.5) / vec2f(size);
    var p: vec4f = pc.invProj * vec4f(uv * 2.0 - 1.0, d, 1.0);
    return p.xyz / p.w;
}

// Interleaved gradient noise (Jimenez): a per-pixel rotation without the
// precision banding a sine-based hash shows on large coordinates.
fn hash(p: vec2f) -> f32 { return fract(52.9829189 * fract(0.06711056 * p.x + 0.00583715 * p.y)); }

fn effect() {
    var d0: f32 = textureSampleLevel(depthTex, depthTexSampler, vUV, 0.0).r;
    if (d0 >= 1.0) { outColor = vec4f(1.0); return; }
    var size: vec2i = vec2i(textureDimensions(depthTex, 0));
    var c: vec2i = vec2i(vUV * vec2f(size));
    var p: vec3f = viewPosAt(c, size);
    // Normal from explicit neighbours rather than quad derivatives, which
    // band on flat surfaces; pick the smaller difference on each axis so
    // depth edges do not smear the normal.
    var px1: vec3f = viewPosAt(c + vec2i(1, 0), size) - p;
    var px2: vec3f = p - viewPosAt(c - vec2i(1, 0), size);
    var py1: vec3f = viewPosAt(c + vec2i(0, 1), size) - p;
    var py2: vec3f = p - viewPosAt(c - vec2i(0, 1), size);
    var dx: vec3f = select(px2, px1, length(px1) < length(px2));
    var dy: vec3f = select(py2, py1, length(py1) < length(py2));
    var n: vec3f = normalize(cross(dx, dy));
    if (dot(n, -p) < 0.0) { n = -n; } // face the camera regardless of winding
    var radius: f32 = pc.proj[3][3];
    var proj: mat4x4f = pc.proj;
    proj[3][3] = 0.0;
    var angle: f32 = hash(fragCoordValue.xy) * 6.2831853;
    var occlusion: f32 = 0.0;
    let N: i32 = 16;
    var up: vec3f = select(vec3f(1, 0, 0), vec3f(0, 0, 1), abs(n.z) < 0.999);
    var t: vec3f = normalize(cross(up, n));
    var b: vec3f = cross(n, t);
    for (var i: i32 = 0; i < N; i++) {
        // Spiral of directions on the hemisphere around n, with sample
        // distances clustered towards the point so nearby creases count.
        var a: f32 = angle + f32(i) * 2.399963;
        var r: f32 = sqrt((f32(i) + 0.5) / f32(N)) * 0.9; // keep samples off the surface plane
        var dir: vec3f = vec3f(cos(a) * r, sin(a) * r, sqrt(1.0 - r * r));
        var k: f32 = f32(i + 1) / f32(N);
        var dist: f32 = mix(0.1, 1.0, k * k) * radius;
        var s: vec3f = p + (t * dir.x + b * dir.y + n * dir.z) * dist;
        var c: vec4f = proj * vec4f(s, 1.0);
        var uv: vec2f = (c.xy / c.w) * 0.5 + 0.5;
        if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) { continue; }
        var sceneZ: f32 = viewPos(uv).z;
        // Only nearby geometry occludes: a far silhouette must not halo.
        var rangeCheck: f32 = 1.0 - smoothstep(radius, radius * 2.0, abs(p.z - sceneZ));
        // The bias grows with distance: a depth texel of far floor spans
        // more view-space z than a fixed epsilon.
        var bias: f32 = 0.015 + 0.01 * abs(p.z);
        occlusion += (select(0.0, 1.0, sceneZ >= s.z + bias)) * rangeCheck;
    }
    outColor = vec4f(vec3f(1.0 - occlusion / f32(N)), 1.0);
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
