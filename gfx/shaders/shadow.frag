#version 450

// Depth-only pass: writes nothing but discards alpha-cutout fragments so
// leaves and fences cast the shadow of their shape.
layout(set = 0, binding = 0) uniform sampler2D albedoTex;

layout(location = 0) in vec2 vUV;
layout(location = 1) flat in vec2 vCutout; // x base alpha, y cutoff

void main() {
    if (vCutout.y > 0.0 && texture(albedoTex, vUV).a * vCutout.x < vCutout.y) discard;
}
