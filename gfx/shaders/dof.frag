#version 450

// Depth of field: the circle of confusion grows with the distance from
// the focus plane, and each pixel gathers a disc of samples that wide.
// A sample nearer the camera than the pixel only contributes as much as
// its own blur allows, so a sharp foreground does not smear over the
// background behind it.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D depthTex;

layout(push_constant) uniform PC {
    mat4 matrix; // the inverse projection, for view-space distance from depth
    vec4 a;      // xy = 1 / size, z = focus distance, w = focus range
    vec4 b;      // x = blur radius in pixels, y = sample count
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

// viewDistance turns a depth sample into distance in front of the camera.
float viewDistance(vec2 uv) {
    vec4 v = pc.matrix * vec4(uv * 2.0 - 1.0, texture(depthTex, uv).r, 1.0);
    return -v.z / v.w;
}

// coc is how defocused a surface at that distance is, 0 sharp to 1 fully blurred.
float coc(float dist) {
    float range = max(pc.a.w, 1e-4);
    return clamp((abs(dist - pc.a.z) - range) / range, 0.0, 1.0);
}

void main() {
    vec3 sharp = texture(scene, vUV).rgb;
    float dist = viewDistance(vUV);
    float here = coc(dist);
    if (here <= 0.0) {
        outColor = vec4(sharp, 1.0);
        return;
    }
    float radius = here * pc.b.x;
    int taps = int(pc.b.y);
    vec3 sum = sharp;
    float weight = 1.0;
    for (int i = 1; i <= taps; i++) {
        float angle = float(i) * 2.39996323; // the golden angle spreads the disc evenly
        float r = sqrt(float(i) / float(taps)) * radius;
        vec2 uv = vUV + vec2(cos(angle), sin(angle)) * r * pc.a.xy;
        float d = viewDistance(uv);
        // Behind this pixel it contributes fully; in front only as far as
        // its own defocus spreads it.
        float w = d >= dist ? 1.0 : coc(d);
        sum += texture(scene, uv).rgb * w;
        weight += w;
    }
    outColor = vec4(mix(sharp, sum / weight, here), 1.0);
}
