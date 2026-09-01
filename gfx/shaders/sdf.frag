#version 450

// Signed-distance-field text: the atlas stores distance to the glyph edge
// with 0.5 on the outline; the derivative width keeps edges one pixel soft
// at any scale.
layout(set = 0, binding = 0) uniform sampler2D tex;

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vColor;
layout(location = 0) out vec4 outColor;

void main() {
    float d = texture(tex, vUV).a;
    float w = max(fwidth(d) * 0.75, 0.002);
    float a = smoothstep(0.5 - w, 0.5 + w, d);
    outColor = vColor * a;
}
