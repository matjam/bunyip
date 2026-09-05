#version 450

// Depth-only pass: writes nothing but discards alpha-cutout fragments so
// leaves and fences cast the shadow of their shape. Set 0 is the
// material set, whose images and samplers are separate; only the albedo
// image is read here.
layout(set = 0, binding = 0) uniform texture2D tAlbedo;
layout(set = 0, binding = 17) uniform sampler samplers[4];

layout(location = 0) in vec2 vUV;
layout(location = 1) flat in vec3 vCutout; // x base alpha, y cutoff, z albedo sampler

void main() {
    if (vCutout.y > 0.0 && texture(sampler2D(tAlbedo, samplers[int(vCutout.z)]), vUV).a * vCutout.x < vCutout.y) discard;
}
