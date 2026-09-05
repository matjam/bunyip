#version 450

// Motion blur: each pixel is smeared back along the way it travelled
// since the last frame. The camera's part of that comes from reprojecting
// the pixel's depth, the object's part from the velocity buffer.
layout(set = 0, binding = 0) uniform sampler2D scene;
layout(set = 0, binding = 1) uniform sampler2D velocity;
layout(set = 0, binding = 2) uniform sampler2D depthTex;

layout(push_constant) uniform PC {
    mat4 matrix; // this frame's clip space back to the previous frame's
    vec4 a;      // xy = 1 / size, z = strength, w = sample count
    vec4 b;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    float depth = texture(depthTex, vUV).r;
    vec4 clip = pc.matrix * vec4(vUV * 2.0 - 1.0, depth, 1.0);
    vec2 prevUV = (clip.xy / clip.w) * 0.5 + 0.5 - texture(velocity, vUV).rg;
    vec2 motion = (vUV - prevUV) * pc.a.z;
    // Below a tenth of a pixel there is nothing to smear.
    if (dot(motion, motion) < dot(pc.a.xy * 0.1, pc.a.xy * 0.1)) {
        outColor = vec4(texture(scene, vUV).rgb, 1.0);
        return;
    }
    int taps = max(int(pc.a.w), 2);
    vec3 sum = vec3(0.0);
    for (int i = 0; i < taps; i++) {
        vec2 uv = clamp(vUV - motion * (float(i) / float(taps - 1)), vec2(0.0), vec2(1.0));
        sum += texture(scene, uv).rgb;
    }
    outColor = vec4(sum / float(taps), 1.0);
}
