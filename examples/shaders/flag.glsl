// A mesh shader with a vertex hook: a flag rippling in the wind. The
// vertex() hook displaces the cloth before the model matrix, in the lit
// and shadow passes alike; surface() stripes it.
UNIFORMS uniform Params {
    float strength;
} u;

void vertex(inout VertexData v) {
    // Fixed at the pole (u = 0), waving more towards the free edge.
    float free = v.uv.x;
    float wave = sin(v.uv.x * 6.0 - time() * 4.0) + 0.5 * sin(v.uv.y * 4.0 - time() * 6.0);
    v.position.z += wave * 0.15 * free * u.strength;
    // Tilt the normal with the slope of the wave so the lighting ripples.
    float slope = cos(v.uv.x * 6.0 - time() * 4.0) * 6.0 * 0.15 * free * u.strength;
    v.normal = normalize(vec3(-slope * 0.5, 0.0, 1.0));
}

void surface(inout Surface s) {
    float band = step(0.5, fract(s.uv.y * 3.0));
    s.albedo = mix(vec3(0.9, 0.2, 0.15), vec3(0.95, 0.95, 0.9), band);
    s.roughness = 0.8;
}
