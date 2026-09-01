#version 450

// One triangle covers the screen; UV comes from the vertex index.
layout(location = 0) out vec2 vUV;

void main() {
    vec2 uv = vec2((gl_VertexIndex << 1) & 2, gl_VertexIndex & 2);
    vUV = uv;
    gl_Position = vec4(uv * 2.0 - 1.0, 0.0, 1.0);
}
