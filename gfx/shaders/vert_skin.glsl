
// Skinned meshes: joints and weights per vertex, joint matrices from a
// storage buffer indexed by the instance's joint base.
layout(location = 10) in uvec4 iJoints;
layout(location = 11) in vec4 iWeights;

layout(std430, set = 3, binding = 0) readonly buffer Joints { mat4 joints[]; };

// skinMatrix blends this vertex's four joint matrices.
mat4 skinMatrix() {
    uint base = uint(iExtra.x);
    return iWeights.x * joints[base + iJoints.x] + iWeights.y * joints[base + iJoints.y]
         + iWeights.z * joints[base + iJoints.z] + iWeights.w * joints[base + iJoints.w];
}
