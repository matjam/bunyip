#version 450

// Outline and x-ray pass: the mesh drawn again as a solid colour. For
// outlines each vertex is pushed out along its normal by a width in
// pixels, so the shell shows around the silhouette where the stencil
// says the mesh itself is not.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 5) in vec4 iModel0;
layout(location = 6) in vec4 iModel1;
layout(location = 7) in vec4 iModel2;

layout(set = 0, binding = 0) uniform Frame {
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
    vec4 pointPos[8];
    vec4 pointColor[8];
    vec4 sh[9];
    vec4 env;
    mat4 invViewProj;
} frame;

layout(push_constant) uniform PC {
    vec4 color;
    vec4 params; // x outline width in pixels, y viewport width, z viewport height
} pc;

void main() {
    mat4 m = mat4(vec4(iModel0.x, iModel1.x, iModel2.x, 0.0), vec4(iModel0.y, iModel1.y, iModel2.y, 0.0),
                  vec4(iModel0.z, iModel1.z, iModel2.z, 0.0), vec4(iModel0.w, iModel1.w, iModel2.w, 1.0));
    vec4 world = m * vec4(iPos, 1.0);
    vec4 clip = frame.viewProj * world;
    if (pc.params.x > 0.0) {
        vec3 n = normalize(mat3(m) * iNormal);
        vec4 clipN = frame.viewProj * vec4(world.xyz + n, 1.0);
        vec2 dir = clipN.xy / clipN.w - clip.xy / clip.w;
        float len = length(dir);
        if (len > 1e-5) {
            dir /= len;
            clip.xy += dir * pc.params.x * 2.0 / pc.params.yz * clip.w;
        }
    }
    gl_Position = clip;
}
