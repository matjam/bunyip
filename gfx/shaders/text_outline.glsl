#version 450

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 fragColor;
layout(set = 0, binding = 0) uniform sampler2D tex;

layout(std140, set = 1, binding = 0) uniform TextOutline {
    vec4 outlineColor;
    vec4 parameters; // x: outer threshold; remaining words reserved
};

void main() {
    float d = texture(tex, vUV).a;
    float w = max(fwidth(d) * 0.75, 0.002);
    float outside = smoothstep(parameters.x - w, parameters.x + w, d);
    float inside = smoothstep(0.5 - w, 0.5 + w, d);
    fragColor = outlineColor * max(outside - inside, 0.0);
}
