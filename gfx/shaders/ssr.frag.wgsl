var<private> fragCoordValue: vec4f;

// Screen-space reflections. The pass runs after the opaque draws and
// before the blended ones, over a copy of the scene whose alpha channel
// each opaque draw filled with how much reflection it wants. A ray is
// marched along the reflection direction in world space, projected to the
// screen at every step and compared against the depth buffer; where it
// meets the scene the colour there is blended over the surface, and where
// it misses the surface keeps the environment or probe reflection the
// mesh shader already gave it.
@group(0) @binding(0) var sceneTex: texture_2d<f32>;
@group(0) @binding(1) var sceneTexSampler: sampler; // the opaque scene copy, alpha = reflection weight
@group(0) @binding(2) var depthTex: texture_2d<f32>;
@group(0) @binding(3) var depthTexSampler: sampler; // the scene depth

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
    a: vec4f, // xy one texel of the scene image
    b: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

// worldAt reconstructs the world position of a depth texel.
fn worldAtDepth(uv: vec2f, depth: f32) -> vec3f {
    var p: vec4f = frame.invViewProj * vec4f(uv * 2.0 - 1.0, depth, 1.0);
    return p.xyz / p.w;
}

fn worldAt(uv: vec2f) -> vec3f { return worldAtDepth(uv, textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r); }

// Interleaved gradient noise (Jimenez), to spread the first step of
// neighbouring rays and hide the marching stride.
fn hash(p: vec2f) -> f32 { return fract(52.9829189 * fract(0.06711056 * p.x + 0.00583715 * p.y)); }

// screenOf projects a world point to screen coordinates; w comes back
// negative behind the camera.
fn screenOf(p: vec3f) -> vec3f {
    var clip: vec4f = frame.viewProj * vec4f(p, 1.0);
    return vec3f(clip.xy / clip.w * 0.5 + 0.5, clip.w);
}

fn effect() {
    outColor = vec4f(0.0);
    if (frame.reflect.x <= 0.0) { return; }
    var d0: f32 = textureSampleLevel(depthTex, depthTexSampler, vUV, 0.0).r;
    if (d0 >= 1.0) { return; } // the sky reflects nothing
    var weight: f32 = textureSampleLevel(sceneTex, sceneTexSampler, vUV, 0.0).a;
    if (weight <= 0.002) { return; }

    var p: vec3f = worldAtDepth(vUV, d0);
    var v: vec3f = normalize(frame.camPos.xyz - p);
    // The normal comes from the depth buffer, so a surface is as flat as
    // its triangles: take the nearer neighbour on each axis so a silhouette
    // does not tilt the frame.
    var texel: vec2f = pc.a.xy;
    var px1: vec3f = worldAt(vUV + vec2f(texel.x, 0.0)) - p;
    var px2: vec3f = p - worldAt(vUV - vec2f(texel.x, 0.0));
    var py1: vec3f = worldAt(vUV + vec2f(0.0, texel.y)) - p;
    var py2: vec3f = p - worldAt(vUV - vec2f(0.0, texel.y));
    var dx: vec3f = select(px2, px1, dot(px1, px1) < dot(px2, px2));
    var dy: vec3f = select(py2, py1, dot(py1, py1) < dot(py2, py2));
    var n: vec3f = normalize(cross(dx, dy));
    if (dot(n, v) < 0.0) { n = -n; }
    var r: vec3f = reflect(-v, n);

    var steps: i32 = i32(clamp(frame.reflect.w, 1.0, 256.0));
    var maxDist: f32 = max(frame.reflect.z, 1e-3);
    var stride: f32 = maxDist / f32(steps);
    var thickness: f32 = stride * 2.0 + 0.05;
    var t: f32 = stride * (0.5 + 0.5 * hash(fragCoordValue.xy));
    var prev: f32 = 0.0;
    var hit: bool = false;
    for (var i: i32 = 0; i < steps; i++) {
        var s: vec3f = p + r * t;
        var scr: vec3f = screenOf(s);
        if (scr.z <= 0.0) { break; }
        if (scr.x < 0.0 || scr.x > 1.0 || scr.y < 0.0 || scr.y > 1.0) { break; }
        var behind: f32 = distance(s, frame.camPos.xyz) - distance(worldAt(scr.xy), frame.camPos.xyz);
        if (behind > 0.0) {
            if (behind > thickness) { break; } // the ray passed behind something thin
            // Halve the last stride a few times to land on the surface.
            var lo: f32 = prev;
            var hi: f32 = t;
            for (var k: i32 = 0; k < 4; k++) {
                var mid: f32 = 0.5 * (lo + hi);
                var sm: vec3f = p + r * mid;
                var sm2: vec3f = screenOf(sm);
                if (distance(sm, frame.camPos.xyz) - distance(worldAt(sm2.xy), frame.camPos.xyz) > 0.0) { hi = mid; }
                else { lo = mid; }
            }
            t = hi;
            hit = true;
            break;
        }
        prev = t;
        t += stride;
    }
    if (!hit) { return; }

    var scr: vec3f = screenOf(p + r * t);
    var uv: vec2f = clamp(scr.xy, vec2f(0.0), vec2f(1.0));
    // Fade at the edges of the screen, with the distance travelled, and
    // for rays coming back towards the camera, which the screen holds
    // nothing for.
    var edge: vec2f = smoothstep(vec2f(0.0), vec2f(0.12), uv) * (1.0 - smoothstep(vec2f(0.88), vec2f(1.0), uv));
    var fade: f32 = edge.x * edge.y;
    fade *= 1.0 - smoothstep(0.6, 1.0, t / maxDist);
    fade *= clamp(1.0 - dot(r, v), 0.0, 1.0);
    var w: f32 = clamp(weight * fade, 0.0, 1.0);
    if (w <= 0.0) { return; }
    outColor = vec4f(textureSampleLevel(sceneTex, sceneTexSampler, uv, 0.0).rgb * w, w);
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
