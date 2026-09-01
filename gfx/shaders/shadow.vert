#version 450

// Depth-only pass from the light's point of view.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;

layout(location = 3) in vec4 iModel0;
layout(location = 4) in vec4 iModel1;
layout(location = 5) in vec4 iModel2;
layout(location = 6) in vec4 iModel3;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3]; // shadow cascades
    vec4 camPos;
    vec4 lightDir;     // direction the light travels
    vec4 lightColor;   // rgb, w = shadow strength
    vec4 sky;          // rgb ambient from above
    vec4 ground;       // rgb ambient from below
    vec4 params;       // x = shadow map size, y = shadows enabled, z = point light count
    vec4 splits;       // view-space distances where cascades end
    vec4 pointPos[8];  // xyz, w = range
    vec4 pointColor[8];
} frame;

layout(push_constant) uniform PC { int cascade; } pc;

void main() {
    gl_Position = frame.lightViewProj[pc.cascade] * mat4(iModel0, iModel1, iModel2, iModel3) * vec4(iPos, 1.0);
}
