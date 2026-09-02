#version 450

// Prelude for 2D shaders. bunyip-shader puts this before the game's
// code and a main after it; the game defines
//
//     vec4 fragment(vec2 uv, vec4 color)
//
// returning a premultiplied colour. tex is the sprite's own texture
// and image0..image3 are the shader's extra images; a uniform block is
// declared as UNIFORMS uniform Params { ... } u; with std140 layout.
layout(set = 0, binding = 0) uniform sampler2D tex;
layout(set = 0, binding = 1) uniform sampler2D image0;
layout(set = 0, binding = 2) uniform sampler2D image1;
layout(set = 0, binding = 3) uniform sampler2D image2;
layout(set = 0, binding = 4) uniform sampler2D image3;

layout(push_constant) uniform PC {
    mat4 proj;
    vec4 frame; // x time in seconds, y view width, z view height, w pixels per view unit
} pc;

#define UNIFORMS layout(set = 1, binding = 0)

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vColor;
layout(location = 2) in vec2 vPos;
layout(location = 0) out vec4 outColor;

// time is seconds since the game started.
float time() { return pc.frame.x; }
// viewSize is the 2D coordinate space in view units.
vec2 viewSize() { return pc.frame.yz; }
// pixelScale is framebuffer pixels per view unit.
float pixelScale() { return pc.frame.w; }
// position is this fragment's position in view units, before projection.
vec2 position() { return vPos; }

vec4 fragment(vec2 uv, vec4 color);
