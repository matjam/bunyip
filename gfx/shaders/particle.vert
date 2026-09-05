#version 450

// Instanced 2D particles: one quad per instance, expanded here from a
// centre, a size, a rotation and a pair of texture coordinates. The six
// vertices come from gl_VertexIndex, so there is no per-vertex buffer.
layout(location = 0) in vec3 iPos;      // z is unused in 2D
layout(location = 1) in float iRotation;
layout(location = 2) in vec2 iSize;
layout(location = 3) in vec2 iUV0;
layout(location = 4) in vec2 iUV1;
layout(location = 5) in vec4 iColor;    // straight alpha; the fragment stage premultiplies

layout(push_constant) uniform PC {
    mat4 proj;
    vec4 frame; // x time in seconds, y view width, z view height, w pixels per view unit
} pc;

layout(location = 0) out vec2 vUV;
layout(location = 1) out vec4 vColor;

const vec2 corners[6] = vec2[6](
    vec2(-0.5, -0.5), vec2(0.5, -0.5), vec2(0.5, 0.5),
    vec2(-0.5, -0.5), vec2(0.5, 0.5), vec2(-0.5, 0.5));

void main() {
    vec2 c = corners[gl_VertexIndex] * iSize;
    float s = sin(iRotation), co = cos(iRotation);
    vec2 p = iPos.xy + vec2(c.x * co - c.y * s, c.x * s + c.y * co);
    gl_Position = pc.proj * vec4(p, 0.0, 1.0);
    vUV = mix(iUV0, iUV1, corners[gl_VertexIndex] + 0.5);
    vColor = iColor;
}
