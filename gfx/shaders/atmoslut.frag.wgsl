// The atmosphere's lookup tables, one fullscreen pass per table, chosen
// by pc.betaM.w: 0 the transmittance table, 1 the sky view, 2 the aerial
// perspective, 3 the reflection table. Each texel runs the same
// single-scattering integral the sky used to run per pixel, with the
// same steps, so what the tables hold is Sky.scatter in gfx/sky.go at
// the texel's ray. The lit meshes and the sky background read them
// through the ATMOSPHERE LOOKUP block in prelude_mesh.wgsl and
// skyparam.frag.wgsl.
struct PC {
    atmos: vec4f, // planet radius, air height, rayleigh and mie falloff heights
    betaR: vec4f, // rgb rayleigh scattering per unit at the ground, w the sun's intensity
    betaM: vec4f, // x mie scattering, y forward lobe, z camera altitude, w the table to build
    up: vec4f, // xyz away from the ground, w the air (1 - vacuum)
    sun: vec4f, // xyz towards the sun
}
var<push_constant> pc: PC;

// The transmittance table, which the sky view reads for the light that
// reaches each of its samples. The other two passes bind a stand-in.
@group(0) @binding(0) var transTex: texture_2d<f32>;
@group(0) @binding(1) var transSampler: sampler;

// ATMOSPHERE MAPPING. Everything between this line and END ATMOSPHERE
// MAPPING is the same text in atmoslut.frag.wgsl, which builds the
// atmosphere's lookup tables, and in prelude_mesh.wgsl and
// skyparam.frag.wgsl, which read them: where a table keeps a ray has to
// be the same on both sides. TestAtmosphereBlocksMatch compares the
// three, and TestAtmosphereTablesMatchGo checks the tables against
// Sky.scatter in gfx/sky.go.
const ATMOS_PI: f32 = 3.14159265359;
// The transmittance table: the optical depth from a point to the top of
// the air, by the ray's angle across and the point's height down, both
// on Bruneton's mapping, which spends the texels near the ground and the
// horizon. Red and green are the four-step integrals of air and haze,
// blue and alpha the eight-step ones, each over its falloff height.
const ATMOS_TRANS_W: f32 = 256.0;
const ATMOS_TRANS_H: f32 = 64.0;
// The sky view: the light scattered towards the camera from every
// direction, without the phase functions, which the reader applies. The
// azimuth from the sun runs across, from towards it to away from it,
// since the sky is the same either side of the sun. Down each block
// runs the angle from the zenith, the sky above the ground's horizon in
// the top half and the ground below it in the bottom half, each on a
// square-root mapping that spends the texels near the horizon. Air is
// the top block and haze the bottom one.
const ATMOS_SKY_W: f32 = 128.0;
const ATMOS_SKY_H: f32 = 128.0;
// The aerial perspective: the same view mapping at ATMOS_AERIAL_D
// distances side by side, each a fraction of the way along the ray's
// run through the air (atmosSpan), closer together near the camera and
// near the ray's end, with the optical depth back to the camera in
// alpha.
const ATMOS_AERIAL_W: f32 = 32.0;
const ATMOS_AERIAL_H: f32 = 64.0;
const ATMOS_AERIAL_D: f32 = 32.0;

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
    var ik: f32 = inverseSqrt(1.0 + g2 - 2.0 * g * mu);
    return 3.0 / (8.0 * ATMOS_PI) * (1.0 - g2) / (2.0 + g2) * (1.0 + mu * mu) * ik * ik * ik;
}

// atmosTexel places x, from 0 to 1, between the centres of the first
// and last of n texels, in texels.
fn atmosTexel(x: f32, n: f32) -> f32 { return clamp(x, 0.0, 1.0) * (n - 1.0) + 0.5; }

// atmosUnit is the inverse of atmosTexel.
fn atmosUnit(t: f32, n: f32) -> f32 { return clamp((t - 0.5) / (n - 1.0), 0.0, 1.0); }

// atmosTransCoord is where the transmittance table keeps a ray from
// radius r whose cosine with the zenith is mu, in texels. The ray must
// not meet the ground.
fn atmosTransCoord(r: f32, mu: f32, radius: f32, top: f32) -> vec2f {
    var span: f32 = sqrt(top * top - radius * radius);
    var rho: f32 = sqrt(max(r * r - radius * radius, 0.0));
    var d: f32 = max(-r * mu + sqrt(max(r * r * (mu * mu - 1.0) + top * top, 0.0)), 0.0);
    var dMin: f32 = top - r;
    var dMax: f32 = rho + span;
    return vec2f(atmosTexel((d - dMin) / max(dMax - dMin, 1e-6), ATMOS_TRANS_W), atmosTexel(rho / span, ATMOS_TRANS_H));
}

// atmosTransRay is the inverse of atmosTransCoord: the radius and the
// cosine with the zenith a texel stands for.
fn atmosTransRay(t: vec2f, radius: f32, top: f32) -> vec2f {
    var span: f32 = sqrt(top * top - radius * radius);
    var rho: f32 = span * atmosUnit(t.y, ATMOS_TRANS_H);
    var r: f32 = sqrt(rho * rho + radius * radius);
    var dMin: f32 = top - r;
    var dMax: f32 = rho + span;
    var d: f32 = dMin + atmosUnit(t.x, ATMOS_TRANS_W) * (dMax - dMin);
    var mu: f32 = 1.0;
    if (d > 0.0) { mu = clamp((span * span - rho * rho - d * d) / (2.0 * r * d), -1.0, 1.0); }
    return vec2f(r, mu);
}

// The view tables measure a direction's angle from the zenith by the
// tangent of half of it, which needs no trigonometry to find or invert:
// it is 0 at the zenith, 1 at the horizontal and grows without bound
// towards the nadir. Below the horizon they use its reciprocal, which
// is 0 at the nadir. Across, they measure the cosine of the azimuth
// from the sun: the light the tables hold leaves out the phase
// functions, so it changes slowly towards the sun and away from it.

// atmosHorizon describes the view from radius r for the view tables,
// as tangents of half the angle from the zenith: x of the first ray
// that reaches the air, 0 inside it, and y of the ray that grazes the
// ground. z is 1 / (y - x). From space the sky half of a table spans
// only the limb, between x and y.
fn atmosHorizon(r: f32, radius: f32, top: f32) -> vec3f {
    var rr: f32 = max(r, radius);
    var first: f32 = 0.0;
    if (rr > top) { first = (rr + sqrt(rr * rr - top * top)) / top; }
    var grazing: f32 = (rr + sqrt(max(rr * rr - radius * radius, 0.0))) / radius;
    return vec3f(first, grazing, 1.0 / (grazing - first));
}

// atmosSunSide is the horizontal direction towards the sun, which the
// view tables measure azimuth from. With the sun at the zenith every
// azimuth is the same and any horizontal direction does.
fn atmosSunSide(up: vec3f, sun: vec3f) -> vec3f {
    var side: vec3f = sun - up * dot(sun, up);
    if (dot(side, side) < 1e-12) {
        side = cross(up, select(vec3f(1.0, 0.0, 0.0), vec3f(0.0, 0.0, 1.0), abs(up.x) > 0.9));
    }
    return normalize(side);
}

// atmosViewCoord is where a view table w texels across and h down keeps
// direction d, in texels. It is in the bottom half, and meets the
// ground, when y is more than h / 2.
fn atmosViewCoord(d: vec3f, up: vec3f, side: vec3f, horizon: vec3f, w: f32, h: f32) -> vec2f {
    var cosZ: f32 = clamp(dot(d, up), -1.0, 1.0);
    // One over the sine of the angle from the zenith gives the tangent
    // of half of it, (1 - cos) / sin, and its reciprocal, (1 + cos) / sin.
    // side is level, so its dot with d is its dot with d's level part.
    var inv: f32 = inverseSqrt(max(1.0 - cosZ * cosZ, 1e-12));
    var cosA: f32 = clamp(dot(d, side) * inv, -1.0, 1.0);
    var t: f32 = (1.0 - cosZ) * inv;
    var rows: f32 = h * 0.5;
    var y: f32;
    if (t < horizon.y) {
        y = atmosTexel(1.0 - sqrt(clamp((horizon.y - t) * horizon.z, 0.0, 1.0)), rows);
    } else {
        y = rows + atmosTexel(sqrt(clamp(1.0 - (1.0 + cosZ) * inv * horizon.y, 0.0, 1.0)), rows);
    }
    return vec2f(atmosTexel(0.5 - 0.5 * cosA, w), y);
}

// atmosViewDir is the inverse of atmosViewCoord: the direction a texel
// stands for.
fn atmosViewDir(t: vec2f, up: vec3f, side: vec3f, horizon: vec3f, w: f32, h: f32) -> vec3f {
    var rows: f32 = h * 0.5;
    var cosZ: f32;
    var sinZ: f32;
    if (t.y < rows) {
        var c: f32 = 1.0 - atmosUnit(t.y, rows);
        var p: f32 = horizon.y - (horizon.y - horizon.x) * c * c;
        cosZ = (1.0 - p * p) / (1.0 + p * p);
        sinZ = 2.0 * p / (1.0 + p * p);
    } else {
        var c: f32 = atmosUnit(t.y - rows, rows);
        var q: f32 = (1.0 - c * c) / horizon.y;
        cosZ = -(1.0 - q * q) / (1.0 + q * q);
        sinZ = 2.0 * q / (1.0 + q * q);
    }
    var cosA: f32 = 1.0 - 2.0 * atmosUnit(t.x, w);
    var sinA: f32 = sqrt(max(1.0 - cosA * cosA, 0.0));
    return up * cosZ + (side * cosA + cross(up, side) * sinA) * sinZ;
}

// atmosSpan is where a view ray from radius r whose cosine with the
// zenith is cosZ enters the air, and how far it runs through it before
// it leaves or, with ground set, meets the ground: the stretch the
// aerial perspective table's distances divide. A ray meets the ground
// when it starts above it and lies in the bottom half of a view table,
// so a table and its reader agree about a ray that grazes the ground.
fn atmosSpan(r: f32, cosZ: f32, radius: f32, top: f32, ground: bool) -> vec2f {
    var b: f32 = r * cosZ;
    var h: f32 = b * b - (r * r - top * top);
    if (h < 0.0) { return vec2f(0.0); }
    h = sqrt(h);
    var t0: f32 = max(-b - h, 0.0);
    var t1: f32 = -b + h;
    if (ground && r > radius) { t1 = min(t1, -b - sqrt(max(b * b - (r * r - radius * radius), 0.0))); }
    return vec2f(t0, max(t1 - t0, 0.0));
}

// atmosSliceFraction is how far along the span the aerial perspective
// table's slice u, from 0 to 1, lies: quadratic from each end, so the
// slices crowd near the camera and near the ray's end. atmosSliceOf is
// its inverse.
fn atmosSliceFraction(u: f32) -> f32 {
    if (u < 0.5) { return 2.0 * u * u; }
    return 1.0 - 2.0 * (1.0 - u) * (1.0 - u);
}
fn atmosSliceOf(x: f32) -> f32 {
    if (x < 0.5) { return sqrt(0.5 * x); }
    return 1.0 - sqrt(0.5 - 0.5 * x);
}

// The reflection table: skyColor for every direction, ATMOS_REFLECT_N
// texels square on an octahedral mapping about the zenith with the
// sun's side along its first axis. The height above or below the
// horizon is warped to its square root first, which crowds the texels
// towards the horizon, where the sky changes fastest. The lit meshes'
// reflections read it in one lookup and no trigonometry.
const ATMOS_REFLECT_N: f32 = 256.0;

// atmosOctCoord is where the reflection table keeps direction d, as a
// texture coordinate. The warped direction is still of unit length: its
// level part shrinks by 1 / sqrt(1 + |z|) as its height z grows to
// sqrt(|z|).
fn atmosOctCoord(d: vec3f, up: vec3f, side: vec3f) -> vec2f {
    var z: f32 = dot(d, up);
    var a: f32 = abs(z);
    var level: vec2f = vec2f(dot(d, side), dot(d, cross(up, side))) * inverseSqrt(1.0 + a);
    var l: vec3f = vec3f(level, sign(z) * sqrt(a));
    var p: vec2f = l.xy / (abs(l.x) + abs(l.y) + abs(l.z));
    if (l.z < 0.0) {
        p = (vec2f(1.0) - abs(p.yx)) * select(vec2f(-1.0), vec2f(1.0), p >= vec2f(0.0));
    }
    return p * 0.5 + vec2f(0.5);
}

// atmosOctDir is the inverse of atmosOctCoord: the direction a texture
// coordinate stands for. The fold below the horizon is its own inverse.
fn atmosOctDir(uv: vec2f, up: vec3f, side: vec3f) -> vec3f {
    var p: vec2f = uv * 2.0 - vec2f(1.0);
    var w: f32 = 1.0 - abs(p.x) - abs(p.y);
    if (w < 0.0) {
        p = (vec2f(1.0) - abs(p.yx)) * select(vec2f(-1.0), vec2f(1.0), p >= vec2f(0.0));
    }
    var l: vec3f = normalize(vec3f(p, w));
    var z: f32 = sign(l.z) * l.z * l.z;
    var level: vec2f = l.xy * sqrt(1.0 + abs(z));
    return side * level.x + cross(up, side) * level.y + up * z;
}
// END ATMOSPHERE MAPPING.

// opticalDepth integrates the air and the haze from p along dir to the
// top of the air in steps samples, over world units: the light a sample
// of the view ray receives from the sun, or the camera from space.
fn opticalDepth(p: vec3f, dir: vec3f, steps: i32) -> vec2f {
    var radius: f32 = pc.atmos.x;
    var lightStep: f32 = max(raySphere(p, dir, radius + pc.atmos.y).y, 0.0) / f32(steps);
    var od: vec2f = vec2f(0.0);
    for (var j: i32 = 0; j < steps; j++) {
        var q: vec3f = p + dir * ((f32(j) + 0.5) * lightStep);
        var hj: f32 = max(length(q) - radius, 0.0);
        od.x += exp(-hj / pc.atmos.z) * lightStep;
        od.y += exp(-hj / pc.atmos.w) * lightStep;
    }
    return od;
}

// Terms is single scattering along a view ray without the phase
// functions and the sun's intensity: air and haze, each times its
// scattering coefficient, and the ray's optical depth in world units.
struct Terms { ray: vec3f, mie: vec3f, od: vec2f }

// scatterTerms integrates single scattering along a ray leaving the
// camera in direction d, for at most dist world units of the stretch
// span (from atmosSpan) in steps samples: air and haze thinning with
// height, each sample lit by what is left of the sunlight that reached
// it and dimmed by the air back to the camera. Samples the planet
// shadows are dark. The sunlight comes from the transmittance table's
// four-step integral when useTable is set, and from sunSteps samples of
// its own otherwise. It is Sky.scatterTerms in gfx/sky.go.
fn scatterTerms(d: vec3f, dist: f32, steps: i32, sunSteps: i32, useTable: bool, span: vec2f) -> Terms {
    var radius: f32 = pc.atmos.x;
    var top: f32 = radius + pc.atmos.y;
    var hR: f32 = pc.atmos.z;
    var hM: f32 = pc.atmos.w;
    var betaR: vec3f = pc.betaR.rgb;
    var betaM: f32 = pc.betaM.x;
    var sun: vec3f = pc.sun.xyz;
    var origin: vec3f = pc.up.xyz * (radius + pc.betaM.z);
    var none: Terms = Terms(vec3f(0.0), vec3f(0.0), vec2f(0.0));
    var t0: f32 = span.x;
    var t1: f32 = t0 + min(span.y, dist);
    if (t1 <= t0) { return none; } // outside the air, looking away from the planet
    var ds: f32 = (t1 - t0) / f32(steps);
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
        var lod: vec2f;
        if (useTable) {
            var r: f32 = length(p);
            var c: vec2f = atmosTransCoord(r, dot(p, sun) / r, radius, top);
            lod = textureSampleLevel(transTex, transSampler, c / vec2f(ATMOS_TRANS_W, ATMOS_TRANS_H), 0.0).xy * vec2f(hR, hM);
        } else {
            lod = opticalDepth(p, sun, sunSteps);
        }
        var att: vec3f = exp(-(betaR * (odR + lod.x) + betaM * 1.1 * (odM + lod.y)));
        sumR += att * dR;
        sumM += att * dM;
    }
    return Terms(sumR * betaR, sumM * betaM, vec2f(odR, odM));
}

fn build(t: vec2f) -> vec4f {
    var radius: f32 = pc.atmos.x;
    var top: f32 = radius + pc.atmos.y;
    var mode: i32 = i32(pc.betaM.w + 0.5);
    if (mode == 0) {
        // The transmittance table: the ray's optical depth to the top of
        // the air, over the falloff heights, from four steps and eight.
        var rm: vec2f = atmosTransRay(t, radius, top);
        var p: vec3f = vec3f(0.0, rm.x, 0.0);
        var dir: vec3f = vec3f(sqrt(max(1.0 - rm.y * rm.y, 0.0)), rm.y, 0.0);
        var four: vec2f = opticalDepth(p, dir, 4);
        var eight: vec2f = opticalDepth(p, dir, 8);
        return vec4f(four / pc.atmos.zw, eight / pc.atmos.zw);
    }
    var up: vec3f = pc.up.xyz;
    var side: vec3f = atmosSunSide(up, pc.sun.xyz);
    var horizon: vec3f = atmosHorizon(radius + pc.betaM.z, radius, top);
    if (mode == 3) {
        // The reflection table: skyColor in prelude_mesh.wgsl, integrated
        // along the texel's own direction, before the ground is blended in
        // below the horizon, which the reader does: the blend is steepest
        // at the horizon, where the table's texels would soften it. Below
        // the horizon a camera in the air looks the colour up along the
        // horizon, as the sky does.
        var d: vec3f = atmosOctDir(t / ATMOS_REFLECT_N, up, side);
        var cosZ: f32 = dot(d, up);
        var dir: vec3f = d;
        if (cosZ < 0.0 && pc.betaM.z < pc.atmos.y) {
            var level: vec3f = d - up * cosZ;
            var len: f32 = length(level);
            if (len > 1e-4) { dir = level / len; }
        }
        var z: f32 = clamp(dot(dir, up), -1.0, 1.0);
        var below: bool = (1.0 - z) * inverseSqrt(max(1.0 - z * z, 1e-12)) >= horizon.y;
        var s: Terms = scatterTerms(dir, 1e9, 8, 4, true, atmosSpan(radius + pc.betaM.z, z, radius, top, below));
        // Divided by the haze's phase towards the texel's own direction,
        // which peaks sharply towards the sun and which the reader
        // multiplies back in, so the table holds nothing its texels would
        // blur.
        var mu: f32 = dot(dir, pc.sun.xyz);
        var light: vec3f = pc.betaR.w * (s.ray * phaseRayleigh(mu) + s.mie * phaseMie(mu, pc.betaM.y)) * pc.up.w;
        return vec4f(light / phaseMie(dot(d, pc.sun.xyz), pc.betaM.y), 1.0);
    }
    var w: f32 = ATMOS_SKY_W;
    var h: f32 = ATMOS_SKY_H;
    var k: f32 = 0.0;
    var at: vec2f = t;
    if (mode == 2) {
        // Which slice.
        w = ATMOS_AERIAL_W;
        h = ATMOS_AERIAL_H;
        k = floor(t.x / w);
        at.x = t.x - k * w;
    }
    // Haze in the bottom block.
    var mie: bool = at.y >= h;
    if (mie) { at.y -= h; }
    var d: vec3f = atmosViewDir(at, up, side, horizon, w, h);
    var span: vec2f = atmosSpan(radius + pc.betaM.z, dot(d, up), radius, top, at.y >= h * 0.5);
    var s: Terms;
    if (mode == 1) {
        s = scatterTerms(d, 1e9, 8, 4, true, span);
    } else {
        // The slice's fraction of the way along the ray's run through the
        // air, so every ray's last slice ends where the ray does.
        s = scatterTerms(d, span.y * atmosSliceFraction(k / (ATMOS_AERIAL_D - 1.0)), 4, 2, false, span);
    }
    if (mie) { return vec4f(s.mie, s.od.y / pc.atmos.w); }
    return vec4f(s.ray, s.od.x / pc.atmos.z);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@builtin(position) fragCoord: vec4f) -> EffectOutput {
    return EffectOutput(build(fragCoord.xy));
}
