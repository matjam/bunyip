var<private> fragCoordValue: vec4f;

// Screen-space reflections, traced at half resolution. The pass runs after
// the opaque draws, reading the scene whose alpha channel each opaque draw
// filled with how much reflection it wants, and the half-resolution depth.
// A pixel's reflected ray is projected to the screen once and marched
// along that line in even screen steps; the ray's depth is interpolated
// in the same space the depth buffer stores it in, so a step costs one
// depth read. The first step behind the scene is refined by halving, and
// the colour there is what the surface reflects. Where the ray leaves the
// screen or hits nothing the output is zero and the surface keeps the
// environment or probe reflection the mesh shader already gave it.
//
// The output is the reflected colour times how much of it survives the
// fades, with that fraction in alpha. With pc.a.x set both are also
// multiplied by the surface's own weight, averaged over the four pixels
// this one covers, for the pass that blends the result over the scene
// without being able to read the scene's weights itself.
@group(0) @binding(0) var sceneTex: texture_2d<f32>;
@group(0) @binding(1) var sceneTexSampler: sampler; // the opaque scene, alpha = reflection weight
@group(0) @binding(2) var depthTex: texture_2d<f32>;
@group(0) @binding(3) var depthTexSampler: sampler; // the half-resolution depth
@group(0) @binding(4) var fullDepth: texture_2d<f32>;
@group(0) @binding(5) var fullDepthSampler: sampler; // the scene depth, for the surface and its normal

struct Frame {
    viewProj: mat4x4f,
    view: mat4x4f,
    lightViewProj: array<mat4x4f, 3>,
    camPos: vec4f,
    lightDir: vec4f,
    lightColor: vec4f,
    sky: vec4f,
    ground: vec4f,
    params: vec4f,
    splits: vec4f,
    radii: vec4f,
    sh: array<vec4f, 9>,
    env: vec4f,
    invViewProj: mat4x4f,
    horizon: vec4f,
    skyUp: vec4f,
    sun: vec4f,
    sunColor: vec4f,
    fog: vec4f,
    fogRange: vec4f,
    spotViewProj: array<mat4x4f, 4>,
    pointViewProj: array<mat4x4f, 24>,
    cluster: vec4f,
    probePos: array<vec4f, 8>,
    probeMin: array<vec4f, 8>,
    probeMax: array<vec4f, 8>,
    probeParams: array<vec4f, 8>,
    gridOrigin: vec4f,
    gridSpacing: vec4f,
    gridCounts: vec4f,
    reflect: vec4f, // x strength, y max roughness, z max distance, w steps
}
@group(1) @binding(0) var<uniform> frame: Frame;

struct PC {
    a: vec4f, // x = multiply by the surface's weight
    b: vec4f, // the projection's z row: [2][2], [3][2], [2][3], [3][3]
    c: vec4f,
    d: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn worldAtDepth(uv: vec2f, depth: f32) -> vec3f {
    var p: vec4f = frame.invViewProj * vec4f(uv * 2.0 - 1.0, depth, 1.0);
    return p.xyz / p.w;
}

// worldAtTexel reconstructs the world position of a full-size depth texel.
fn worldAtTexel(px: vec2i, size: vec2i) -> vec3f {
    let c = clamp(px, vec2i(0), size - vec2i(1));
    return worldAtDepth((vec2f(c) + 0.5) / vec2f(size), textureLoad(fullDepth, c, 0).r);
}

// nearer picks the smaller of two differences, but never one that is
// zero because a neighbour at the edge of the image is the texel itself.
fn nearer(a: vec3f, b: vec3f) -> vec3f {
    return select(b, a, (dot(a, a) < dot(b, b) && dot(a, a) > 0.0) || dot(b, b) <= 0.0);
}

// depthAt reads the half-resolution depth texel under a point.
fn depthAt(uv: vec2f) -> f32 { return textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r; }

// viewZ turns a stored depth into view-space z, negative in front of the
// camera, for a perspective or an orthographic projection alike.
fn viewZ(d: f32) -> f32 { return (pc.b.y - d * pc.b.w) / (d * pc.b.z - pc.b.x); }

// Interleaved gradient noise (Jimenez), to spread the first step of
// neighbouring rays and hide the marching stride.
fn hash(p: vec2f) -> f32 { return fract(52.9829189 * fract(0.06711056 * p.x + 0.00583715 * p.y)); }

fn effect() {
    outColor = vec4f(0.0);
    if (frame.reflect.x <= 0.0) { return; }
    var size: vec2f = vec2f(textureDimensions(depthTex, 0));
    var weight: f32 = textureSampleLevel(sceneTex, sceneTexSampler, vUV, 0.0).a;
    if (weight <= 0.002) { return; }
    // The surface and its normal come from the full-size depth texel at
    // the pixel, so a curved surface keeps its shape; only the march reads
    // the half-size depth.
    var fsize: vec2i = vec2i(textureDimensions(fullDepth, 0));
    var c: vec2i = vec2i(vUV * vec2f(fsize));
    if (textureLoad(fullDepth, clamp(c, vec2i(0), fsize - vec2i(1)), 0).r >= 1.0) { return; } // the sky reflects nothing
    var p: vec3f = worldAtTexel(c, fsize);
    var v: vec3f = normalize(frame.camPos.xyz - p);
    // The normal comes from the depth buffer, so a surface is as flat as
    // its triangles: take the nearer neighbour on each axis so a silhouette
    // does not tilt the frame.
    var dx: vec3f = nearer(worldAtTexel(c + vec2i(1, 0), fsize) - p, p - worldAtTexel(c - vec2i(1, 0), fsize));
    var dy: vec3f = nearer(worldAtTexel(c + vec2i(0, 1), fsize) - p, p - worldAtTexel(c - vec2i(0, 1), fsize));
    var n: vec3f = normalize(cross(dx, dy));
    if (dot(n, v) < 0.0) { n = -n; }
    var r: vec3f = reflect(-v, n);

    // The ray from the surface to as far as it may travel, cut at the near
    // plane, where clip-space z is zero, so both ends project.
    var maxDist: f32 = max(frame.reflect.z, 1e-3);
    var c0: vec4f = frame.viewProj * vec4f(p, 1.0);
    var c1: vec4f = frame.viewProj * vec4f(p + r * maxDist, 1.0);
    if (c1.z < 0.0) {
        c1 = mix(c0, c1, 0.999 * c0.z / (c0.z - c1.z));
    }
    if (c1.w <= 0.0) { return; }
    var s0: vec3f = vec3f(c0.xy / c0.w * 0.5 + 0.5, c0.z / c0.w);
    var s1: vec3f = vec3f(c1.xy / c1.w * 0.5 + 0.5, c1.z / c1.w);

    // Even steps along the line on screen, no more than one a texel. The
    // stored depth is linear on screen, so the ray's own depth at a step
    // is the same interpolation of its two ends.
    var maxSteps: i32 = i32(clamp(frame.reflect.w, 1.0, 256.0));
    var span: f32 = length((s1.xy - s0.xy) * size);
    var steps: i32 = clamp(i32(ceil(span)), 1, maxSteps);
    var thickness: f32 = maxDist / f32(maxSteps) * 2.0 + 0.05;
    var jitter: f32 = hash(fragCoordValue.xy);
    // The march starts two texels out, so a surface seen at a grazing
    // angle does not find itself in the nearest depth of its neighbour.
    var first: f32 = min(2.0 / max(span, 1.0), 1.0);
    var prev: f32 = first;
    var hit: bool = false;
    var at: f32 = 0.0;
    for (var i: i32 = 0; i < steps; i++) {
        var s: f32 = mix(first, 1.0, (f32(i) + jitter) / f32(steps));
        var q: vec3f = mix(s0, s1, s);
        if (q.x < 0.0 || q.x > 1.0 || q.y < 0.0 || q.y > 1.0) { break; }
        var behind: f32 = viewZ(depthAt(q.xy)) - viewZ(q.z);
        if (behind > 0.0) {
            if (behind > thickness) { break; } // the ray passed behind something thin
            // Halve the last step a few times to land on the surface.
            var lo: f32 = prev;
            var hi: f32 = s;
            for (var k: i32 = 0; k < 4; k++) {
                var mid: f32 = 0.5 * (lo + hi);
                var m: vec3f = mix(s0, s1, mid);
                if (viewZ(depthAt(m.xy)) - viewZ(m.z) > 0.0) { hi = mid; }
                else { lo = mid; }
            }
            at = hi;
            hit = true;
            break;
        }
        prev = s;
    }
    if (!hit) { return; }

    var h: vec3f = mix(s0, s1, at);
    var uv: vec2f = clamp(h.xy, vec2f(0.0), vec2f(1.0));
    var travelled: f32 = distance(p, worldAtDepth(uv, h.z));
    // Fade at the edges of the screen, with the distance travelled, and
    // for rays coming back towards the camera, which the screen holds
    // nothing for.
    var edge: vec2f = smoothstep(vec2f(0.0), vec2f(0.12), uv) * (1.0 - smoothstep(vec2f(0.88), vec2f(1.0), uv));
    var fade: f32 = edge.x * edge.y;
    fade *= 1.0 - smoothstep(0.6, 1.0, travelled / maxDist);
    fade *= clamp(1.0 - dot(r, v), 0.0, 1.0);
    if (pc.a.x > 0.5) { fade *= weight; }
    fade = clamp(fade, 0.0, 1.0);
    if (fade <= 0.0) { return; }
    outColor = vec4f(textureSampleLevel(sceneTex, sceneTexSampler, uv, 0.0).rgb * fade, fade);
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
