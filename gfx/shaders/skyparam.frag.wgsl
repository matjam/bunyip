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
    atmosView: vec4f, // xyz the horizontal direction towards the sun, w the camera's distance from the planet's centre
    atmosLimb: vec4f, // xyz the view tables' horizon, atmosHorizon at the camera
}
@group(0) @binding(0) var<uniform> frame: Frame;
@group(1) @binding(0) var spaceMap: texture_cube<f32>;
@group(1) @binding(1) var spaceSampler: sampler;
// The atmosphere's lookup tables share the mesh pass's set 2 with the
// shadow atlas, which this program does not read.
@group(2) @binding(2) var atmosTransTable: texture_2d<f32>;
@group(2) @binding(3) var atmosSkyTable: texture_2d<f32>;
@group(2) @binding(4) var atmosAerialTable: texture_2d<f32>;
@group(2) @binding(5) var atmosReflectTable: texture_2d<f32>;
@group(2) @binding(6) var atmosSampler: sampler;

struct PC {
    invViewProj: mat4x4f,
    params: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

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

// ATMOSPHERE LOOKUP. Everything between this line and END ATMOSPHERE
// LOOKUP is the same text in prelude_mesh.wgsl and skyparam.frag.wgsl:
// the sky and the lit meshes read the atmosphere from the tables
// atmoslut.frag.wgsl builds, and Sky.radiance in gfx/sky.go projects
// the ambient harmonics from the same model, so they have to agree.
// TestAtmosphereBlocksMatch compares the two shaders and
// TestAtmosphereMatchesGo the drawn sky with the Go side.

// ScatterResult is the light a stretch of air scatters towards the
// camera, and how much of the light from beyond it survives.
struct ScatterResult { radiance: vec3f, transmittance: vec3f }

// atmosTransmittance is how much light survives from radius r along a
// ray whose cosine with the zenith is mu to the top of the air, from
// the transmittance table's eight-step integral: how the air dims the
// view of space.
fn atmosTransmittance(r: f32, mu: f32) -> vec3f {
    var c: vec2f = atmosTransCoord(r, mu, frame.atmos.x, frame.atmos.x + frame.atmos.y);
    var od: vec4f = textureSampleLevel(atmosTransTable, atmosSampler, c / vec2f(ATMOS_TRANS_W, ATMOS_TRANS_H), 0.0);
    var depth: vec2f = od.zw * frame.atmos.zw;
    return exp(-(frame.betaR.rgb * depth.x + frame.betaM.x * 1.1 * depth.y));
}

// atmosSkyLight is the sunlight the whole air scatters towards the
// camera from direction d, from the sky view table. frame.atmosView and
// frame.atmosLimb hold what atmosSunSide and atmosHorizon give for this
// frame's camera, found once on the processor.
fn atmosSkyLight(d: vec3f) -> vec3f {
    var c: vec2f = atmosViewCoord(d, frame.skyUp.xyz, frame.atmosView.xyz, frame.atmosLimb.xyz, ATMOS_SKY_W, ATMOS_SKY_H);
    var scale: vec2f = vec2f(1.0 / ATMOS_SKY_W, 0.5 / ATMOS_SKY_H);
    var ray: vec3f = textureSampleLevel(atmosSkyTable, atmosSampler, c * scale, 0.0).rgb;
    var mie: vec3f = textureSampleLevel(atmosSkyTable, atmosSampler, (c + vec2f(0.0, ATMOS_SKY_H)) * scale, 0.0).rgb;
    var mu: f32 = dot(d, frame.sun.xyz);
    return frame.betaR.w * (ray * phaseRayleigh(mu) + mie * phaseMie(mu, frame.betaM.y));
}

// atmosphereScatter is the light the first dist world units of air
// along direction d scatter towards the camera, and how much of the
// light from beyond them survives: the aerial perspective, from the
// table's two nearest distances.
fn atmosphereScatter(d: vec3f, dist: f32) -> ScatterResult {
    var radius: f32 = frame.atmos.x;
    var up: vec3f = frame.skyUp.xyz;
    var c: vec2f = atmosViewCoord(d, up, frame.atmosView.xyz, frame.atmosLimb.xyz, ATMOS_AERIAL_W, ATMOS_AERIAL_H);
    var span: vec2f = atmosSpan(frame.atmosView.w, dot(d, up), radius, radius + frame.atmos.y, c.y > ATMOS_AERIAL_H * 0.5);
    if (span.y <= 0.0) { return ScatterResult(vec3f(0.0), vec3f(1.0)); }
    // The two slices either side, blended by distance rather than by
    // slice, since the light a short stretch of air scatters grows with
    // its length.
    var x: f32 = clamp(dist / span.y, 0.0, 1.0);
    var k0: f32 = min(floor(atmosSliceOf(x) * (ATMOS_AERIAL_D - 1.0)), ATMOS_AERIAL_D - 2.0);
    var x0: f32 = atmosSliceFraction(k0 / (ATMOS_AERIAL_D - 1.0));
    var x1: f32 = atmosSliceFraction((k0 + 1.0) / (ATMOS_AERIAL_D - 1.0));
    var f: f32 = clamp((x - x0) / (x1 - x0), 0.0, 1.0);
    var scale: vec2f = vec2f(1.0 / (ATMOS_AERIAL_W * ATMOS_AERIAL_D), 0.5 / ATMOS_AERIAL_H);
    var a: vec2f = (c + vec2f(k0 * ATMOS_AERIAL_W, 0.0)) * scale;
    var b: vec2f = a + vec2f(ATMOS_AERIAL_W, 0.0) * scale;
    var m: vec2f = vec2f(0.0, 0.5);
    var ray: vec4f = mix(textureSampleLevel(atmosAerialTable, atmosSampler, a, 0.0),
        textureSampleLevel(atmosAerialTable, atmosSampler, b, 0.0), f);
    var mie: vec4f = mix(textureSampleLevel(atmosAerialTable, atmosSampler, a + m, 0.0),
        textureSampleLevel(atmosAerialTable, atmosSampler, b + m, 0.0), f);
    var mu: f32 = dot(d, frame.sun.xyz);
    var light: vec3f = frame.betaR.w * (ray.rgb * phaseRayleigh(mu) + mie.rgb * phaseMie(mu, frame.betaM.y));
    var through: vec3f = exp(-(frame.betaR.rgb * ray.a * frame.atmos.z + frame.betaM.x * 1.1 * mie.a * frame.atmos.w));
    return ScatterResult(light, through);
}

// skyColor is the sky's light from a direction without the sun's disc:
// the atmosphere when the light's Sky has one, otherwise the gradient
// above the horizon, and the ground or planet below. Below the horizon a
// camera inside the air looks the colour up along the horizon instead,
// because its own ray meets the ground at once and would leave a dark
// band; from above the air the ray itself is looked up, so a planet
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
        var c: vec3f = atmosSkyLight(dir) * air;
        if (up < 0.0) { c = mix(c, frame.ground.rgb, pow(-up, 0.5)); }
        return c;
    }
    return skyGradient(d);
}

// skyGradient is skyColor without an atmosphere: the sky's gradient
// above the horizon and the ground or planet below.
fn skyGradient(d: vec3f) -> vec3f {
    var up: f32 = dot(d, frame.skyUp.xyz);
    var air: f32 = frame.horizon.w;
    var above: vec3f = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    var below: vec3f = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    return select(below, above, up >= 0.0);
}

// atmosSkyReflection is skyColor with an atmosphere, from the reflection
// table, for the lit meshes' reflections. The table leaves out the
// haze's phase and the ground's blend below the horizon, the two things
// that change too fast for its texels, and this puts them back.
fn atmosSkyReflection(d: vec3f) -> vec3f {
    var uv: vec2f = atmosOctCoord(d, frame.skyUp.xyz, frame.atmosView.xyz);
    var c: vec3f = textureSampleLevel(atmosReflectTable, atmosSampler, uv, 0.0).rgb * phaseMie(dot(d, frame.sun.xyz), frame.betaM.y);
    var up: f32 = dot(d, frame.skyUp.xyz);
    if (up < 0.0) { c = mix(c, frame.ground.rgb, sqrt(-up)); }
    return c;
}

// spaceTransmittance attenuates distant radiance with the atmosphere and
// hides it behind the solid planet. It matches Sky.spaceTransmittance in Go.
fn spaceTransmittance(d: vec3f) -> vec3f {
    if (frame.betaM.w < 0.5) {
        return vec3f(select(1.0 - frame.horizon.w, 1.0, dot(d, frame.skyUp.xyz) >= 0.0));
    }
    let origin = frame.skyUp.xyz * (frame.atmos.x + frame.betaM.z);
    let ground = raySphere(origin, d, frame.atmos.x);
    if (ground.y > 0.0 && ground.x >= 0.0) { return vec3f(0.0); }
    let shell = raySphere(origin, d, frame.atmos.x + frame.atmos.y);
    let start = max(shell.x, 0.0);
    if (shell.y <= start) { return vec3f(1.0); }
    let p = origin + d * start;
    let r = max(length(p), frame.atmos.x);
    return mix(vec3f(1.0), atmosTransmittance(r, dot(p, d) / r), vec3f(frame.horizon.w));
}
// END ATMOSPHERE LOOKUP.

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
	if (frame.env.w != 0.0) {
		color += textureSampleLevel(spaceMap, spaceSampler, dir, 0.0).rgb * frame.env.w * spaceTransmittance(dir);
	}

    // The sun: a soft-edged disc, hidden by the ground, with a glow that
    // only the air can scatter. An atmosphere reddens and dims the disc
    // by the air it shines through, the same at every pixel, so the
    // processor folds that into frame.sunColor (Sky.sunTint).
    var sunDir: vec3f = frame.sun.xyz;
    var r: f32 = frame.sun.w;
    var c: f32 = dot(dir, sunDir);
    var visible: f32 = smoothstep(-r, r, dot(sunDir, frame.skyUp.xyz)) * step(0.0, up);
    var disc: f32 = smoothstep(cos(r * 1.25), cos(r), c);
    color += frame.sunColor.rgb * disc * visible;
    color += frame.sunColor.rgb * air * 0.01 * pow(max(c, 0.0), 24.0) * visible;

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
