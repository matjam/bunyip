#version 450

// God rays: the sky is the light source, so anything the depth buffer
// says is geometry blocks it. Each pixel walks towards the sun's place on
// screen, gathering the unoccluded steps and fading them as it goes, and
// what comes out is the shafts between an occluder's edges.
layout(set = 0, binding = 0) uniform sampler2D depthTex;

layout(push_constant) uniform PC {
    vec4 a; // xy = the sun in texture coordinates, z = strength, w = decay
    vec4 b; // x = sample count, y = density, z = step weight
    vec4 c; // rgb = the light's colour
    vec4 d;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    int taps = max(int(pc.b.x), 2);
    vec2 delta = (vUV - pc.a.xy) * (pc.b.y / float(taps));
    vec2 uv = vUV;
    float fade = 1.0;
    float sum = 0.0;
    for (int i = 0; i < taps; i++) {
        uv -= delta;
        // Depth 1 is sky: nothing between this step and the sun.
        float lit = texture(depthTex, clamp(uv, vec2(0.0), vec2(1.0))).r >= 1.0 ? 1.0 : 0.0;
        sum += lit * fade * pc.b.z;
        fade *= pc.a.w;
    }
    outColor = vec4(pc.c.rgb * (sum * pc.a.z), 1.0);
}
