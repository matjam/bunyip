// A mesh shader: a cooled crust with glowing cracks that pulse, on top
// of the standard lighting.
UNIFORMS uniform Params {
    float heat;
} u;

void surface(inout Surface s) {
    vec2 p = s.worldPos.xz * 1.5 + vec2(time() * 0.05, 0.0);
    float n = texture(image0, p * 0.25).r;
    float crack = smoothstep(0.45, 0.55, n);
    float pulse = 0.6 + 0.4 * sin(time() * 2.0 + n * 12.0);
    s.albedo = mix(vec3(0.05, 0.04, 0.04), vec3(0.2, 0.1, 0.08), n);
    s.roughness = mix(0.95, 0.4, crack);
    s.emissive += vec3(1.0, 0.35, 0.05) * crack * pulse * u.heat;
}

vec4 finish(vec4 lit, Surface s) {
    // Fade the far edges to black so the slab reads as a pool.
    float rim = smoothstep(0.0, 0.5, 1.0 - abs(s.uv.x - 0.5) * 2.0) * smoothstep(0.0, 0.5, 1.0 - abs(s.uv.y - 0.5) * 2.0);
    return vec4(lit.rgb * mix(0.3, 1.0, rim), lit.a);
}
