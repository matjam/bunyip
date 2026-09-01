#version 450

// One instance per sprite; six vertices per instance form two triangles.
layout(location = 0) in vec2 iPos;
layout(location = 1) in vec2 iSize;
layout(location = 2) in vec2 iUV0;
layout(location = 3) in vec2 iUV1;
layout(location = 4) in vec4 iColor;
layout(location = 5) in float iRotation;
layout(location = 6) in vec2 iOrigin;

layout(push_constant) uniform PC { mat4 proj; } pc;

layout(location = 0) out vec2 vUV;
layout(location = 1) out vec4 vColor;

void main() {
    const vec2 corners[6] = vec2[](vec2(0, 0), vec2(1, 0), vec2(1, 1), vec2(0, 0), vec2(1, 1), vec2(0, 1));
    vec2 c = corners[gl_VertexIndex % 6];
    vec2 local = (c - iOrigin) * iSize;
    float s = sin(iRotation), co = cos(iRotation);
    vec2 world = iPos + vec2(local.x * co - local.y * s, local.x * s + local.y * co);
    gl_Position = pc.proj * vec4(world, 0.0, 1.0);
    vUV = mix(iUV0, iUV1, c);
    vColor = iColor;
}
