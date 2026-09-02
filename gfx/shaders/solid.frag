#version 450

// A flat colour from the push constants: outlines and x-ray tints.
layout(push_constant) uniform PC {
    vec4 color;
    vec4 params;
} pc;

layout(location = 0) out vec4 outColor;

void main() {
    outColor = pc.color;
}
