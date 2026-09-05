#version 450

// Instanced 3D particles: each instance is a camera-facing quad built
// here from the camera's right and up axes, so no per-vertex buffer and
// no per-particle matrix are needed. The instance layout is the same one
// particle.vert reads, with the position in world units.
layout(location = 0) in vec3 iPos;
layout(location = 1) in float iRotation; // radians about the view axis
layout(location = 2) in vec2 iSize;      // world units
layout(location = 3) in vec2 iUV0;
layout(location = 4) in vec2 iUV1;
layout(location = 5) in vec4 iColor;     // straight alpha; the fragment stage premultiplies

layout(push_constant) uniform PC {
    mat4 viewProj;
    vec4 right;  // xyz the camera's right axis, w the camera's x
    vec4 up;     // xyz the camera's up axis, w the camera's y
    vec4 params; // x near plane, y far plane, z soft fade distance, w the camera's z
    vec4 mode;   // x is 1 for an orthographic camera, 0 for a perspective one
} pc;

layout(location = 0) out vec2 vUV;
layout(location = 1) out vec4 vColor;
layout(location = 2) out float vViewZ; // distance along the camera's forward axis

const vec2 corners[6] = vec2[6](
    vec2(-0.5, -0.5), vec2(0.5, -0.5), vec2(0.5, 0.5),
    vec2(-0.5, -0.5), vec2(0.5, 0.5), vec2(-0.5, 0.5));

void main() {
    vec2 c = corners[gl_VertexIndex] * iSize;
    float s = sin(iRotation), co = cos(iRotation);
    vec2 r = vec2(c.x * co - c.y * s, c.x * s + c.y * co);
    vec3 world = iPos + pc.right.xyz * r.x + pc.up.xyz * r.y;
    gl_Position = pc.viewProj * vec4(world, 1.0);
    // The distance along the camera's forward axis, which the soft fade
    // compares against the scene's depth. The forward axis is the cross
    // product of the other two, so it need not be passed in, and this
    // holds for an orthographic camera where the clip w is 1.
    vec3 camPos = vec3(pc.right.w, pc.up.w, pc.params.w);
    vViewZ = dot(world - camPos, cross(pc.up.xyz, pc.right.xyz));
    // The quad's v runs down the screen, matching the 2D convention, so
    // the same texture region draws the same way up in both paths.
    vUV = mix(iUV0, iUV1, corners[gl_VertexIndex] + 0.5);
    vColor = iColor;
}
