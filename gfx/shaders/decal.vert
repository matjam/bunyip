#version 450

// A decal is a unit box projected onto whatever geometry lies inside it.
// The box is drawn with its front faces culled, so it works from inside.
layout(location = 0) in vec3 iPos;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3];
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 sky;
    vec4 ground;
    vec4 params;
    vec4 splits;
    vec4 radii;
    vec4 pointPos[32];
    vec4 pointColor[32];
    vec4 spotDir[32];
    vec4 sh[9];
    vec4 env;
    mat4 invViewProj;
} frame;

layout(push_constant) uniform PC {
    mat4 box;     // unit box to world
    vec4 invBox0; // world to box, three rows
    vec4 invBox1;
    vec4 invBox2;
    vec4 tint;
} pc;

void main() {
    gl_Position = frame.viewProj * pc.box * vec4(iPos, 1.0);
}
