#version 450

// The 2D vertex stream: sprites, text, shapes and strokes all arrive as
// plain triangles with a position in view units, a texture coordinate
// and a premultiplied colour.
layout(location = 0) in vec2 iPos;
layout(location = 1) in vec2 iUV;
layout(location = 2) in vec4 iColor;

layout(push_constant) uniform PC {
    mat4 proj;
    vec4 frame; // x time in seconds, y view width, z view height, w pixels per view unit
    vec4 transformX;
    vec4 transformY;
} pc;

layout(location = 0) out vec2 vUV;
layout(location = 1) out vec4 vColor;
layout(location = 2) out vec2 vPos;

void main() {
    vec2 pos = vec2(dot(pc.transformX.xyz, vec3(iPos, 1.0)),
                    dot(pc.transformY.xyz, vec3(iPos, 1.0)));
    gl_Position = pc.proj * vec4(pos, 0.0, 1.0);
    vUV = iUV;
    vColor = iColor;
    vPos = pos;
}
