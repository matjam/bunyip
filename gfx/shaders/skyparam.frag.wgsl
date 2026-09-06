// The procedural sky as the background: the atmosphere above the
// horizon, whether scattered or a gradient, the ground or planet below,
// the sun's disc with a haze glow, and stars through thin air. The sky
// must stay in step with skyColor in prelude_mesh.wgsl, which lights the
// meshes, and with Sky.radiance in Go, which projects the ambient.
struct Frame {
    viewProj: mat4x4f,
    view: mat4x4f,
    lightViewProj: array<mat4x4f, 3>,
    camPos: vec4f,
    lightDir: vec4f,
    lightColor: vec4f,
    sky: vec4f, // rgb zenith
    ground: vec4f, // rgb light from below
    params: vec4f,
    splits: vec4f,
    radii: vec4f,
    sh: array<vec4f, 9>,
    env: vec4f,
    invViewProj: mat4x4f,
    horizon: vec4f, // rgb the sky at the horizon, w = air (1 - vacuum)
    skyUp: vec4f, // xyz up, w = stars
    sun: vec4f, // xyz towards the sun, w = angular radius
    sunColor: vec4f, // rgb the drawn disc's radiance
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
    reflect: vec4f,
    atmos: vec4f, // x planet radius, y air height, z rayleigh, w mie falloff height
    betaR: vec4f, // rgb rayleigh scattering per unit at the ground, w = sun intensity
    betaM: vec4f, // x mie scattering, y forward lobe, z camera altitude, w = 1 with an atmosphere
}
@group(0) @binding(0) var<uniform> frame: Frame;

struct PC {
    invViewProj: mat4x4f,
    params: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

// ATMOSPHERE. Everything between this line and END ATMOSPHERE is the
// same text in prelude_mesh.wgsl and skyparam.frag.wgsl, and Sky.scatter and
// Sky.radiance in gfx/sky.go are the same functions in Go: the ambient
// harmonics are projected from the Go side and the pixels come from
// here, so the three must stay in step. TestAtmosphereBlocksMatch
// compares the two shaders and TestAtmosphereMatchesGo the Go side.
const ATMOS_VIEW_STEPS: i32 = 8;
const ATMOS_SUN_STEPS: i32 = 4;
const ATMOS_PI: f32 = 3.14159265359;

// raySphere returns where a ray from o along d crosses a sphere of
// radius r about the origin, as two distances along the ray. x is
// greater than y when the ray misses.
fn raySphere(o: vec3f, d: vec3f, r: f32) -> vec2f {
    var b: f32 = dot(o, d);
    var c: f32 = dot(o, o) - r * r;
    var h: f32 = b * b - c;
    if (h < 0.0) { return vec2f(1.0, -1.0); }
    h = sqrt(h);
    return vec2f(-b - h, -b + h);
}

// phaseRayleigh is how much air scatters towards an angle whose cosine
// is mu: nearly even, a little more forwards and backwards.
fn phaseRayleigh(mu: f32) -> f32 { return 3.0 / (16.0 * ATMOS_PI) * (1.0 + mu * mu); }

// phaseMie is the Henyey-Greenstein lobe haze scatters into, forwards
// by g, which is the glare around the sun.
fn phaseMie(mu: f32, g: f32) -> f32 {
    var g2: f32 = g * g;
    var d: f32 = (2.0 + g2) * pow(1.0 + g2 - 2.0 * g * mu, 1.5);
    return 3.0 / (8.0 * ATMOS_PI) * ((1.0 - g2) * (1.0 + mu * mu)) / d;
}

// atmosphereScatter integrates single scattering along a ray leaving the
// camera in direction d, for at most dist world units: air and haze
// thinning with height, each sample lit by what is left of the sunlight
// that reached it and dimmed by the air back to the camera. Samples the
// planet shadows are dark, which is what makes dusk fall. transmittance
// comes back as how much of the light from beyond the segment survives
// it, for aerial perspective and the sun's disc.
struct ScatterResult { radiance: vec3f, transmittance: vec3f }
fn atmosphereScatter(d: vec3f, dist: f32, steps: i32, sunSteps: i32) -> ScatterResult {
    var radius: f32 = frame.atmos.x;
    var height: f32 = frame.atmos.y;
    var hR: f32 = frame.atmos.z;
    var hM: f32 = frame.atmos.w;
    var betaR: vec3f = frame.betaR.rgb;
    var betaM: f32 = frame.betaM.x;
    var sun: vec3f = frame.sun.xyz;
    var origin: vec3f = frame.skyUp.xyz * (radius + frame.betaM.z);
    var transmittance = vec3f(1.0);
    var shell: vec2f = raySphere(origin, d, radius + height);
    var t0: f32 = max(shell.x, 0.0);
    var t1: f32 = shell.y;
    if (t1 <= t0) { return ScatterResult(vec3f(0.0), transmittance); } // outside the air, looking away from the planet
    var gnd: vec2f = raySphere(origin, d, radius);
    if (gnd.y > 0.0 && gnd.x > 0.0) { t1 = min(t1, gnd.x); } // the ray meets the ground first
    t1 = min(t1, t0 + dist);
    if (t1 <= t0) { return ScatterResult(vec3f(0.0), transmittance); }
    var ds: f32 = (t1 - t0) / f32(steps);
    var mu: f32 = dot(d, sun);
    var odR: f32 = 0.0;
    var odM: f32 = 0.0;
    var sumR: vec3f = vec3f(0.0);
    var sumM: vec3f = vec3f(0.0);
    for (var i: i32 = 0; i < steps; i++) {
        var p: vec3f = origin + d * (t0 + (f32(i) + 0.5) * ds);
        var h: f32 = max(length(p) - radius, 0.0);
        var dR: f32 = exp(-h / hR) * ds;
        var dM: f32 = exp(-h / hM) * ds;
        odR += dR;
        odM += dM;
        var shadow: vec2f = raySphere(p, sun, radius);
        if (shadow.y > 0.0 && shadow.x > 0.0) { continue; } // the planet stands in the way
        var lightStep: f32 = max(raySphere(p, sun, radius + height).y, 0.0) / f32(sunSteps);
        var lodR: f32 = 0.0;
        var lodM: f32 = 0.0;
        for (var j: i32 = 0; j < sunSteps; j++) {
            var q: vec3f = p + sun * ((f32(j) + 0.5) * lightStep);
            var hj: f32 = max(length(q) - radius, 0.0);
            lodR += exp(-hj / hR) * lightStep;
            lodM += exp(-hj / hM) * lightStep;
        }
        var att: vec3f = exp(-(betaR * (odR + lodR) + betaM * 1.1 * (odM + lodM)));
        sumR += att * dR;
        sumM += att * dM;
    }
    transmittance = exp(-(betaR * odR + betaM * 1.1 * odM));
    return ScatterResult(frame.betaR.w * (sumR * betaR * phaseRayleigh(mu) + sumM * betaM * phaseMie(mu, frame.betaM.y)), transmittance);
}

// skyColor is the sky's light from a direction without the sun's disc:
// the atmosphere when the light's Sky has one, otherwise the gradient
// above the horizon, and the ground or planet below. Below the horizon a
// camera inside the air looks the colour up along the horizon instead,
// because its own ray meets the ground at once and would leave a dark
// band; from above the air the ray itself is integrated, so a planet
// seen from orbit keeps the glow around its limb.
fn skyColor(d: vec3f) -> vec3f {
    var up: f32 = dot(d, frame.skyUp.xyz);
    var air: f32 = frame.horizon.w;
    if (frame.betaM.w > 0.5) {
        var dir: vec3f = d;
        if (up < 0.0 && frame.betaM.z < frame.atmos.y) {
            var side: vec3f = d - frame.skyUp.xyz * up;
            var len: f32 = length(side);
            if (len > 1e-4) { dir = side / len; }
        }
        var tr: vec3f;
        var c: vec3f = atmosphereScatter(dir, 1e9, ATMOS_VIEW_STEPS, ATMOS_SUN_STEPS).radiance * air;
        if (up < 0.0) { c = mix(c, frame.ground.rgb, pow(-up, 0.5)); }
        return c;
    }
    var above: vec3f = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    var below: vec3f = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    return select(below, above, up >= 0.0);
}
// END ATMOSPHERE.

fn hash(point: vec3f) -> f32 {
    var p = fract(point * 0.3183099 + vec3f(0.1, 0.2, 0.3));
    p *= 17.0;
    return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
}

// stars is a fixed field of points: one in about every sixtieth cell of
// a grid over the direction, offset within its cell, varying in
// brightness and slightly in colour.
fn stars(d: vec3f) -> vec3f {
    var p: vec3f = d * 90.0;
    var cell: vec3f = floor(p);
    var h: f32 = hash(cell);
    if (h < 0.985) { return vec3f(0.0); }
    var centre: vec3f = cell + 0.5 + (vec3f(hash(cell + 1.0), hash(cell + 2.0), hash(cell + 3.0)) - 0.5) * 0.6;
    var glow: f32 = smoothstep(0.14, 0.0, length(p - centre));
    var bright: f32 = 0.4 + 3.0 * fract(h * 13.0);
    var tint: vec3f = mix(vec3f(0.8, 0.9, 1.0), vec3f(1.0, 0.9, 0.75), fract(h * 7.0));
    return tint * glow * bright;
}

fn effect() {
    var ndc: vec2f = vUV * 2.0 - 1.0;
    var near: vec4f = pc.invViewProj * vec4f(ndc, 0.0, 1.0);
    var far: vec4f = pc.invViewProj * vec4f(ndc, 1.0, 1.0);
    var dir: vec3f = normalize(far.xyz / far.w - near.xyz / near.w);

    var up: f32 = dot(dir, frame.skyUp.xyz);
    var air: f32 = frame.horizon.w;
    var color: vec3f = skyColor(dir);

    // The sun: a soft-edged disc, hidden by the ground, with a glow that
    // only the air can scatter. An atmosphere reddens and dims the disc
    // by the air it shines through, which is a transmittance and needs no
    // samples towards the sun.
    var sunDir: vec3f = frame.sun.xyz;
    var r: f32 = frame.sun.w;
    var c: f32 = dot(dir, sunDir);
    var visible: f32 = smoothstep(-r, r, dot(sunDir, frame.skyUp.xyz)) * step(0.0, up);
    var disc: f32 = smoothstep(cos(r * 1.25), cos(r), c);
    var sunTint: vec3f = vec3f(1.0);
    if (frame.betaM.w > 0.5) {
        var tr: vec3f;
        sunTint = atmosphereScatter(sunDir, 1e9, ATMOS_VIEW_STEPS / 2, 1).transmittance;
    }
    color += frame.sunColor.rgb * sunTint * disc * visible;
    color += frame.sunColor.rgb * sunTint * air * 0.01 * pow(max(c, 0.0), 24.0) * visible;

    // Stars, where the air is thin and the ground does not hide them.
    var night: f32 = frame.skyUp.w * (1.0 - air) * smoothstep(-0.01, 0.01, up);
    if (night > 0.0) { color += stars(dir) * night; }

    outColor = vec4f(color, 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
