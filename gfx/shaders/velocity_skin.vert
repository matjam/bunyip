#version 450

// Velocity pass, skinned meshes. The skinning matrix is this frame's for
// both positions, so the motion vector carries the model matrix's motion
// and not the pose's: a character that walks across the screen reprojects
// correctly, an arm swinging in place does not.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec4 iModel0;
layout(location = 2) in vec4 iModel1;
layout(location = 3) in vec4 iModel2;
layout(location = 4) in vec4 iPrev0;
layout(location = 5) in vec4 iPrev1;
layout(location = 6) in vec4 iPrev2;
layout(location = 7) in vec4 iExtra; // x = the instance's first joint matrix
layout(location = 8) in uvec4 iJoints;
layout(location = 9) in vec4 iWeights;

layout(std430, set = 0, binding = 0) readonly buffer Joints { mat4 joints[]; };

layout(push_constant) uniform PC {
    mat4 viewProj;
    mat4 prevViewProj;
} pc;

layout(location = 0) out vec4 vNow;
layout(location = 1) out vec4 vThen;

mat4 rows(vec4 a, vec4 b, vec4 c) {
    return mat4(vec4(a.x, b.x, c.x, 0.0), vec4(a.y, b.y, c.y, 0.0),
                vec4(a.z, b.z, c.z, 0.0), vec4(a.w, b.w, c.w, 1.0));
}

mat4 skinMatrix() {
    uint base = uint(iExtra.x);
    return iWeights.x * joints[base + iJoints.x] + iWeights.y * joints[base + iJoints.y]
         + iWeights.z * joints[base + iJoints.z] + iWeights.w * joints[base + iJoints.w];
}

void main() {
    vec4 local = skinMatrix() * vec4(iPos, 1.0);
    vec4 world = rows(iModel0, iModel1, iModel2) * local;
    gl_Position = pc.viewProj * world;
    vNow = pc.prevViewProj * world;
    vThen = pc.prevViewProj * (rows(iPrev0, iPrev1, iPrev2) * local);
}
