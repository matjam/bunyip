#version 450

// Depth-only pass from the light's point of view.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;

layout(location = 3) in vec4 iModel0;
layout(location = 4) in vec4 iModel1;
layout(location = 5) in vec4 iModel2;
layout(location = 6) in vec4 iModel3;
layout(location = 9) in vec4 iExtra;
layout(location = 10) in uvec4 iJoints;
layout(location = 11) in vec4 iWeights;

layout(std430, set = 3, binding = 0) readonly buffer Joints { mat4 joints[]; };

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 lightViewProj;
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 ambient;
    vec4 params;
    vec4 pointPos[8];
    vec4 pointColor[8];
} frame;

void main() {
    uint base = uint(iExtra.x);
    mat4 skin = iWeights.x * joints[base + iJoints.x] + iWeights.y * joints[base + iJoints.y]
              + iWeights.z * joints[base + iJoints.z] + iWeights.w * joints[base + iJoints.w];
    gl_Position = frame.lightViewProj * mat4(iModel0, iModel1, iModel2, iModel3) * skin * vec4(iPos, 1.0);
}
