#version 450

layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;

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
    vec4 radii;        // half-size of each cascade's orthographic box
    vec4 pointPos[8];  // xyz, w = range
    vec4 pointColor[8];
} frame;

// Per-instance stream: model matrix columns, base colour and material params.
layout(location = 3) in vec4 iModel0;
layout(location = 4) in vec4 iModel1;
layout(location = 5) in vec4 iModel2;
layout(location = 6) in vec4 iModel3;
layout(location = 7) in vec4 iBaseColor;
layout(location = 8) in vec4 iMaterial; // x metallic, y roughness, z emissive strength, w normal map on
layout(location = 9) in vec4 iExtra;    // x joint base index in the joint buffer
layout(location = 10) in uvec4 iJoints;
layout(location = 11) in vec4 iWeights;

layout(std430, set = 3, binding = 0) readonly buffer Joints { mat4 joints[]; };

layout(location = 0) out vec3 vWorldPos;
layout(location = 1) out vec3 vNormal;
layout(location = 2) out vec2 vUV;
layout(location = 3) out float vViewDepth;
layout(location = 4) flat out vec4 vBaseColor;
layout(location = 5) flat out vec4 vMaterial;

void main() {
    uint base = uint(iExtra.x);
    mat4 skin = iWeights.x * joints[base + iJoints.x] + iWeights.y * joints[base + iJoints.y]
              + iWeights.z * joints[base + iJoints.z] + iWeights.w * joints[base + iJoints.w];
    mat4 model = mat4(iModel0, iModel1, iModel2, iModel3) * skin;
    vec4 world = model * vec4(iPos, 1.0);
    gl_Position = frame.viewProj * world;
    vWorldPos = world.xyz;
    vNormal = normalize(mat3(model) * iNormal);
    vUV = iUV;
    vViewDepth = -(frame.view * world).z;
    vBaseColor = iBaseColor;
    vMaterial = iMaterial;
}
