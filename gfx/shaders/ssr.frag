#version 450

// Screen-space reflections. The pass runs after the opaque draws and
// before the blended ones, over a copy of the scene whose alpha channel
// each opaque draw filled with how much reflection it wants. A ray is
// marched along the reflection direction in world space, projected to the
// screen at every step and compared against the depth buffer; where it
// meets the scene the colour there is blended over the surface, and where
// it misses the surface keeps the environment or probe reflection the
// mesh shader already gave it.
layout(set = 0, binding = 0) uniform sampler2D sceneTex; // the opaque scene copy, alpha = reflection weight
layout(set = 0, binding = 1) uniform sampler2D depthTex; // the scene depth

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3];
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 sky;
    vec4 ground;
    vec4 params;
    vec4 splits;
    vec4 radii;
    vec4 sh[9];
    vec4 env;
    mat4 invViewProj;
    vec4 horizon;
    vec4 skyUp;
    vec4 sun;
    vec4 sunColor;
    vec4 fog;
    vec4 fogRange;
    mat4 spotViewProj[4];
    mat4 pointViewProj[24];
    vec4 cluster;
    vec4 probePos[8];
    vec4 probeMin[8];
    vec4 probeMax[8];
    vec4 probeParams[8];
    vec4 gridOrigin;
    vec4 gridSpacing;
    vec4 gridCounts;
    vec4 reflect;   // x strength, y max roughness, z max distance, w steps
} frame;

layout(push_constant) uniform PC {
    vec4 a; // xy one texel of the scene image
    vec4 b;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

// worldAt reconstructs the world position of a depth texel.
vec3 worldAt(vec2 uv, float depth) {
    vec4 p = frame.invViewProj * vec4(uv * 2.0 - 1.0, depth, 1.0);
    return p.xyz / p.w;
}

vec3 worldAt(vec2 uv) { return worldAt(uv, texture(depthTex, uv).r); }

// Interleaved gradient noise (Jimenez), to spread the first step of
// neighbouring rays and hide the marching stride.
float hash(vec2 p) { return fract(52.9829189 * fract(0.06711056 * p.x + 0.00583715 * p.y)); }

// screenOf projects a world point to screen coordinates; w comes back
// negative behind the camera.
vec3 screenOf(vec3 p) {
    vec4 clip = frame.viewProj * vec4(p, 1.0);
    return vec3(clip.xy / clip.w * 0.5 + 0.5, clip.w);
}

void main() {
    outColor = vec4(0.0);
    if (frame.reflect.x <= 0.0) return;
    float d0 = texture(depthTex, vUV).r;
    if (d0 >= 1.0) return; // the sky reflects nothing
    float weight = texture(sceneTex, vUV).a;
    if (weight <= 0.002) return;

    vec3 p = worldAt(vUV, d0);
    vec3 v = normalize(frame.camPos.xyz - p);
    // The normal comes from the depth buffer, so a surface is as flat as
    // its triangles: take the nearer neighbour on each axis so a silhouette
    // does not tilt the frame.
    vec2 texel = pc.a.xy;
    vec3 px1 = worldAt(vUV + vec2(texel.x, 0.0)) - p, px2 = p - worldAt(vUV - vec2(texel.x, 0.0));
    vec3 py1 = worldAt(vUV + vec2(0.0, texel.y)) - p, py2 = p - worldAt(vUV - vec2(0.0, texel.y));
    vec3 dx = dot(px1, px1) < dot(px2, px2) ? px1 : px2;
    vec3 dy = dot(py1, py1) < dot(py2, py2) ? py1 : py2;
    vec3 n = normalize(cross(dx, dy));
    if (dot(n, v) < 0.0) n = -n;
    vec3 r = reflect(-v, n);

    int steps = int(clamp(frame.reflect.w, 1.0, 256.0));
    float maxDist = max(frame.reflect.z, 1e-3);
    float stride = maxDist / float(steps);
    float thickness = stride * 2.0 + 0.05;
    float t = stride * (0.5 + 0.5 * hash(gl_FragCoord.xy));
    float prev = 0.0;
    bool hit = false;
    for (int i = 0; i < steps; i++) {
        vec3 s = p + r * t;
        vec3 scr = screenOf(s);
        if (scr.z <= 0.0) break;
        if (scr.x < 0.0 || scr.x > 1.0 || scr.y < 0.0 || scr.y > 1.0) break;
        float behind = distance(s, frame.camPos.xyz) - distance(worldAt(scr.xy), frame.camPos.xyz);
        if (behind > 0.0) {
            if (behind > thickness) break; // the ray passed behind something thin
            // Halve the last stride a few times to land on the surface.
            float lo = prev, hi = t;
            for (int k = 0; k < 4; k++) {
                float mid = 0.5 * (lo + hi);
                vec3 sm = p + r * mid;
                vec3 sm2 = screenOf(sm);
                if (distance(sm, frame.camPos.xyz) - distance(worldAt(sm2.xy), frame.camPos.xyz) > 0.0) hi = mid;
                else lo = mid;
            }
            t = hi;
            hit = true;
            break;
        }
        prev = t;
        t += stride;
    }
    if (!hit) return;

    vec3 scr = screenOf(p + r * t);
    vec2 uv = clamp(scr.xy, vec2(0.0), vec2(1.0));
    // Fade at the edges of the screen, with the distance travelled, and
    // for rays coming back towards the camera, which the screen holds
    // nothing for.
    vec2 edge = smoothstep(vec2(0.0), vec2(0.12), uv) * (1.0 - smoothstep(vec2(0.88), vec2(1.0), uv));
    float fade = edge.x * edge.y;
    fade *= 1.0 - smoothstep(0.6, 1.0, t / maxDist);
    fade *= clamp(1.0 - dot(r, v), 0.0, 1.0);
    float w = clamp(weight * fade, 0.0, 1.0);
    if (w <= 0.0) return;
    outColor = vec4(textureLod(sceneTex, uv, 0.0).rgb * w, w);
}
