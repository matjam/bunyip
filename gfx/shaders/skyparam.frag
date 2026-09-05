#version 450

// The procedural sky as the background: the atmosphere's gradient above
// the horizon and the ground or planet below, the sun's disc with a haze
// glow, and stars through thin air. The gradient must stay in step with
// skyRadiance in prelude_mesh.glsl, which lights the meshes.
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
} frame;

layout(push_constant) uniform PC {
    mat4 invViewProj;
    vec4 params;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

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
    vec3 above = mix(frame.horizon.rgb, frame.sky.rgb, pow(clamp(up, 0.0, 1.0), 0.7)) * air;
    vec3 below = mix(frame.horizon.rgb * air, frame.ground.rgb, pow(clamp(-up, 0.0, 1.0), 0.5));
    vec3 color = up >= 0.0 ? above : below;

    // The sun: a soft-edged disc, hidden by the ground, with a glow that
    // only the air can scatter.
    vec3 sunDir = frame.sun.xyz;
    float r = frame.sun.w;
    float c = dot(dir, sunDir);
    float visible = smoothstep(-r, r, dot(sunDir, frame.skyUp.xyz)) * step(0.0, up);
    float disc = smoothstep(cos(r * 1.25), cos(r), c);
    color += frame.sunColor.rgb * disc * visible;
    color += frame.sunColor.rgb * air * 0.01 * pow(max(c, 0.0), 24.0) * visible;

    // Stars, where the air is thin and the ground does not hide them.
    float night = frame.skyUp.w * (1.0 - air) * smoothstep(-0.01, 0.01, up);
    if (night > 0.0) color += stars(dir) * night;

    outColor = vec4(color, 1.0);
}
