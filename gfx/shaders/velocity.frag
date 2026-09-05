#version 450

// The object's motion in texture coordinates: where the surface is now
// less where it was, both seen through the previous frame's projection.
// Clip space spans two units across the screen and texture coordinates
// one, hence the half.
layout(location = 0) in vec4 vNow;
layout(location = 1) in vec4 vThen;
layout(location = 0) out vec2 outVelocity;

void main() {
    outVelocity = (vNow.xy / vNow.w - vThen.xy / vThen.w) * 0.5;
}
