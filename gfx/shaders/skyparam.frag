#version 450

// The procedural sky as the background: the atmosphere above the
// horizon, whether scattered or a gradient, the ground or planet below,
// the sun's disc with a haze glow, and stars through thin air. The sky
// must stay in step with skyColor in prelude_mesh.glsl, which lights the
// meshes, and with Sky.radiance in Go, which projects the ambient.
layout(set = 0, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3];
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 sky;          // rgb zenith
    vec4 ground;       // rgb light from below
    vec4 params;
    vec4 splits;
    vec4 radii;
    vec4 sh[9];
    vec4 env;
    mat4 invViewProj;
    vec4 horizon;      // rgb the sky at the horizon, w = air (1 - vacuum)
    vec4 skyUp;        // xyz up, w = stars
    vec4 sun;          // xyz towards the sun, w = angular radius
    vec4 sunColor;     // rgb the drawn disc's radiance
    vec4 fog;
    vec4 fogRange;
    mat4 spotViewProj[4];
    mat4 pointViewProj[24];
    vec4 cluster;
    // The global illumination block, which this shader does not read but
    // must declare to reach what follows it.
    vec4 probePos[8];
    vec4 probeMin[8];
    vec4 probeMax[8];
    vec4 probeParams[8];
    vec4 gridOrigin;
    vec4 gridSpacing;
    vec4 gridCounts;
    vec4 reflect;
    vec4 atmos;        // x planet radius, y air height, z rayleigh, w mie falloff height
    vec4 betaR;        // rgb rayleigh scattering per unit at the ground, w = sun intensity
    vec4 betaM;        // x mie scattering, y forward lobe, z camera altitude, w = 1 with an atmosphere
} frame;

layout(push_constant) uniform PC {
    mat4 invViewProj;
    vec4 params;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

// ATMOSPHERE. Everything between this line and END ATMOSPHERE is the
// same text in prelude_mesh.glsl and skyparam.frag, and Sky.scatter and
// Sky.radiance in gfx/sky.go are the same functions in Go: the ambient
// harmonics are projected from the Go side and the pixels come from
// here, so the three must stay in step. TestAtmosphereBlocksMatch
// compares the two shaders and TestAtmosphereMatchesGo the Go side.
const int ATMOS_VIEW_STEPS = 8;
const int ATMOS_SUN_STEPS = 4;
const float ATMOS_PI = 3.14159265359;

// raySphere returns where a ray from o along d crosses a sphere of
// radius r about the origin, as two distances along the ray. x is
// greater than y when the ray misses.
vec2 raySphere(vec3 o, vec3 d, float r) {
    float b = dot(o, d);
    float c = dot(o, o) - r * r;
    float h = b * b - c;
    if (h < 0.0) return vec2(1.0, -1.0);
    h = sqrt(h);
    return vec2(-b - h, -b + h);
}

// phaseRayleigh is how much air scatters towards an angle whose cosine
// is mu: nearly even, a little more forwards and backwards.
float phaseRayleigh(float mu) { return 3.0 / (16.0 * ATMOS_PI) * (1.0 + mu * mu); }

// phaseMie is the Henyey-Greenstein lobe haze scatters into, forwards
// by g, which is the glare around the sun.
float phaseMie(float mu, float g) {
    float g2 = g * g;
    float d = (2.0 + g2) * pow(1.0 + g2 - 2.0 * g * mu, 1.5);
    return 3.0 / (8.0 * ATMOS_PI) * ((1.0 - g2) * (1.0 + mu * mu)) / d;
}

// atmosphereScatter integrates single scattering along a ray leaving the
// camera in direction d, for at most dist world units: air and haze
// thinning with height, each sample lit by what is left of the sunlight
// that reached it and dimmed by the air back to the camera. Samples the
// planet shadows are dark, which is what makes dusk fall. transmittance
// comes back as how much of the light from beyond the segment survives
// it, for aerial perspective and the sun's disc.
vec3 atmosphereScatter(vec3 d, float dist, int steps, int sunSteps, out vec3 transmittance) {
    float radius = frame.atmos.x, height = frame.atmos.y;
    float hR = frame.atmos.z, hM = frame.atmos.w;
    vec3 betaR = frame.betaR.rgb;
    float betaM = frame.betaM.x;
    vec3 sun = frame.sun.xyz;
    vec3 origin = frame.skyUp.xyz * (radius + frame.betaM.z);
    transmittance = vec3(1.0);
    vec2 shell = raySphere(origin, d, radius + height);
    float t0 = max(shell.x, 0.0), t1 = shell.y;
    if (t1 <= t0) return vec3(0.0); // outside the air, looking away from the planet
    vec2 gnd = raySphere(origin, d, radius);
    if (gnd.y > 0.0 && gnd.x > 0.0) t1 = min(t1, gnd.x); // the ray meets the ground first
    t1 = min(t1, t0 + dist);
    if (t1 <= t0) return vec3(0.0);
    float ds = (t1 - t0) / float(steps);
    float mu = dot(d, sun);
    float odR = 0.0, odM = 0.0;
    vec3 sumR = vec3(0.0), sumM = vec3(0.0);
    for (int i = 0; i < steps; i++) {
        vec3 p = origin + d * (t0 + (float(i) + 0.5) * ds);
        float h = max(length(p) - radius, 0.0);
        float dR = exp(-h / hR) * ds;
        float dM = exp(-h / hM) * ds;
        odR += dR;
        odM += dM;
        vec2 shadow = raySphere(p, sun, radius);
        if (shadow.y > 0.0 && shadow.x > 0.0) continue; // the planet stands in the way
        float lightStep = max(raySphere(p, sun, radius + height).y, 0.0) / float(sunSteps);
        float lodR = 0.0, lodM = 0.0;
        for (int j = 0; j < sunSteps; j++) {
            vec3 q = p + sun * ((float(j) + 0.5) * lightStep);
            float hj = max(length(q) - radius, 0.0);
            lodR += exp(-hj / hR) * lightStep;
            lodM += exp(-hj / hM) * lightStep;
        }
        vec3 att = exp(-(betaR * (odR + lodR) + betaM * 1.1 * (odM + lodM)));
        sumR += att * dR;
        sumM += att * dM;
    }
    transmittance = exp(-(betaR * odR + betaM * 1.1 * odM));
    return frame.betaR.w * (sumR * betaR * phaseRayleigh(mu) + sumM * betaM * phaseMie(mu, frame.betaM.y));
}

// skyColor is the sky's light from a direction without the sun's disc:
// the atmosphere when the light's Sky has one, otherwise the gradient
// above the horizon, and the ground or planet below. Below the horizon a
// camera inside the air looks the colour up along the horizon instead,
// because its own ray meets the ground at once and would leave a dark
// band; from above the air the ray itself is integrated, so a planet
// seen from orbit keeps the glow around its limb.
vec3 skyColor(vec3 d) {
    float up = dot(d, frame.skyUp.xyz);
    float air = frame.horizon.w;
    if (frame.betaM.w > 0.5) {
        vec3 dir = d;
        if (up < 0.0 && frame.betaM.z < frame.atmos.y) {
            vec3 side = d - frame.skyUp.xyz * up;
            float len = length(side);
            if (len > 1e-4) dir = side / len;
        }
        vec3 tr;
        vec3 c = atmosphereScatter(dir, 1e9, ATMOS_VIEW_STEPS, ATMOS_SUN_STEPS, tr) * air;
        if (up < 0.0) c = mix(c, frame.ground.rgb, pow(-up, 0.5));
        return c;
    }
    vec3 above = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    vec3 below = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    return up >= 0.0 ? above : below;
}
// END ATMOSPHERE.

float hash(vec3 p) {
    p = fract(p * 0.3183099 + vec3(0.1, 0.2, 0.3));
    p *= 17.0;
    return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
}

// stars is a fixed field of points: one in about every sixtieth cell of
// a grid over the direction, offset within its cell, varying in
// brightness and slightly in colour.
vec3 stars(vec3 d) {
    vec3 p = d * 90.0;
    vec3 cell = floor(p);
    float h = hash(cell);
    if (h < 0.985) return vec3(0.0);
    vec3 centre = cell + 0.5 + (vec3(hash(cell + 1.0), hash(cell + 2.0), hash(cell + 3.0)) - 0.5) * 0.6;
    float glow = smoothstep(0.14, 0.0, length(p - centre));
    float bright = 0.4 + 3.0 * fract(h * 13.0);
    vec3 tint = mix(vec3(0.8, 0.9, 1.0), vec3(1.0, 0.9, 0.75), fract(h * 7.0));
    return tint * glow * bright;
}

void main() {
    vec2 ndc = vUV * 2.0 - 1.0;
    vec4 near = pc.invViewProj * vec4(ndc, 0.0, 1.0);
    vec4 far = pc.invViewProj * vec4(ndc, 1.0, 1.0);
    vec3 dir = normalize(far.xyz / far.w - near.xyz / near.w);

    float up = dot(dir, frame.skyUp.xyz);
    float air = frame.horizon.w;
    vec3 color = skyColor(dir);

    // The sun: a soft-edged disc, hidden by the ground, with a glow that
    // only the air can scatter. An atmosphere reddens and dims the disc
    // by the air it shines through, which is a transmittance and needs no
    // samples towards the sun.
    vec3 sunDir = frame.sun.xyz;
    float r = frame.sun.w;
    float c = dot(dir, sunDir);
    float visible = smoothstep(-r, r, dot(sunDir, frame.skyUp.xyz)) * step(0.0, up);
    float disc = smoothstep(cos(r * 1.25), cos(r), c);
    vec3 sunTint = vec3(1.0);
    if (frame.betaM.w > 0.5) {
        vec3 tr;
        atmosphereScatter(sunDir, 1e9, ATMOS_VIEW_STEPS / 2, 1, tr);
        sunTint = tr;
    }
    color += frame.sunColor.rgb * sunTint * disc * visible;
    color += frame.sunColor.rgb * sunTint * air * 0.01 * pow(max(c, 0.0), 24.0) * visible;

    // Stars, where the air is thin and the ground does not hide them.
    float night = frame.skyUp.w * (1.0 - air) * smoothstep(-0.01, 0.01, up);
    if (night > 0.0) color += stars(dir) * night;

    outColor = vec4(color, 1.0);
}
