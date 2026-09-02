#version 450

// Debug lines: world-space positions with a colour each, no lighting.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec4 iColor;

layout(push_constant) uniform PC { mat4 viewProj; } pc;

layout(location = 0) out vec4 vColor;

void main() {
    gl_Position = pc.viewProj * vec4(iPos, 1.0);
    vColor = iColor;
}
