#version 450

// 3D particles draw after the scene's depth has been written, in a pass
// with no depth attachment, so the depth image is readable here. The
// test against it is done by hand, which is what makes the soft fade
// possible: instead of a hard edge where a quad meets geometry, the
// particle fades over the last few units in front of it.
layout(set = 0, binding = 0) uniform sampler2D depthTex;
layout(set = 1, binding = 0) uniform sampler2D tex;

layout(push_constant) uniform PC {
    mat4 viewProj;
    vec4 right;
    vec4 up;
    vec4 params; // x near plane, y far plane, z soft fade distance
    vec4 mode;   // x is 1 for an orthographic camera, 0 for a perspective one
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vColor;
layout(location = 2) in float vViewZ;
layout(location = 0) out vec4 outColor;

// linearize turns a depth-buffer value into a distance from the camera.
// An orthographic projection spreads depth evenly; a perspective one
// packs it towards the near plane.
float linearize(float d) {
    float n = pc.params.x, f = pc.params.y;
    if (pc.mode.x > 0.5) return n + d * (f - n);
    return n * f / (f + d * (n - f));
}

void main() {
    vec2 size = vec2(textureSize(depthTex, 0));
    float sceneZ = linearize(texture(depthTex, gl_FragCoord.xy / size).r);
    if (sceneZ <= vViewZ) discard; // behind the scene
    float fade = 1.0;
    if (pc.params.z > 0.0) fade = clamp((sceneZ - vViewZ) / pc.params.z, 0.0, 1.0);
    vec4 c = texture(tex, vUV) * vec4(vColor.rgb * vColor.a, vColor.a);
    outColor = c * fade;
}
