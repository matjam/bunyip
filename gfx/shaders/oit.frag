#version 450

// Resolving weighted blended order-independent transparency: the colour
// every translucent fragment accumulated, divided by the weight it
// accumulated with, and the revealage those fragments left. The pipeline
// blends it over the scene, source times one minus the alpha written
// here plus the destination times it, so a pixel no fragment touched
// keeps the scene exactly.
layout(set = 0, binding = 0) uniform sampler2D accum;
layout(set = 0, binding = 1) uniform sampler2D reveal;

layout(push_constant) uniform PC {
    vec4 a;
    vec4 b;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

void main() {
    vec4 a = texture(accum, vUV);
    outColor = vec4(a.rgb / max(a.a, 1e-5), texture(reveal, vUV).r);
}
