#version 450

// Velocity pass, static meshes. It rasterises with the same jittered
// projection as the scene pass so the motion vectors land on the pixels
// the scene wrote, and it measures each vertex's motion through the
// previous frame's projection alone. What comes out is the object's own
// motion: zero for anything that did not move, because the resolve
// passes reconstruct the camera's part from depth and add this to it.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec4 iModel0;
layout(location = 2) in vec4 iModel1;
layout(location = 3) in vec4 iModel2;
layout(location = 4) in vec4 iPrev0;
layout(location = 5) in vec4 iPrev1;
layout(location = 6) in vec4 iPrev2;

layout(push_constant) uniform PC {
    mat4 viewProj;     // this frame's, jittered
    mat4 prevViewProj; // the previous frame's, not jittered
} pc;

layout(location = 0) out vec4 vNow;  // where the vertex is now
layout(location = 1) out vec4 vThen; // where it was, both in the previous clip space

// rows rebuilds a model matrix from the three instance rows.
mat4 rows(vec4 a, vec4 b, vec4 c) {
    return mat4(vec4(a.x, b.x, c.x, 0.0), vec4(a.y, b.y, c.y, 0.0),
                vec4(a.z, b.z, c.z, 0.0), vec4(a.w, b.w, c.w, 1.0));
}

void main() {
    vec4 local = vec4(iPos, 1.0);
    vec4 world = rows(iModel0, iModel1, iModel2) * local;
    gl_Position = pc.viewProj * world;
    vNow = pc.prevViewProj * world;
    vThen = pc.prevViewProj * (rows(iPrev0, iPrev1, iPrev2) * local);
}
