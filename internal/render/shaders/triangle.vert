#version 450

// A single triangle from the vertex index alone; the renderer's smoke test.
layout(location = 0) out vec3 vColor;

void main() {
    const vec2 pos[3] = vec2[](vec2(0.0, -0.6), vec2(0.6, 0.6), vec2(-0.6, 0.6));
    const vec3 col[3] = vec3[](vec3(1, 0, 0), vec3(0, 1, 0), vec3(0, 0, 1));
    gl_Position = vec4(pos[gl_VertexIndex], 0.0, 1.0);
    vColor = col[gl_VertexIndex];
}
