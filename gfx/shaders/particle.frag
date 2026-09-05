#version 450

// The texture times the particle's tint. Textures are premultiplied on
// upload and the instance tint is not, so the tint is premultiplied
// here to match the pipeline's ONE, ONE_MINUS_SRC_ALPHA blending.
layout(set = 0, binding = 0) uniform sampler2D tex;

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vColor;
layout(location = 0) out vec4 outColor;

void main() {
    outColor = texture(tex, vUV) * vec4(vColor.rgb * vColor.a, vColor.a);
}
