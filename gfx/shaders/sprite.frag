#version 450

layout(set = 0, binding = 0) uniform sampler2D tex;

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vColor;
layout(location = 0) out vec4 outColor;

void main() {
    // Textures and tints are premultiplied; the pipeline blends ONE, ONE_MINUS_SRC_ALPHA.
    outColor = texture(tex, vUV) * vColor;
}
