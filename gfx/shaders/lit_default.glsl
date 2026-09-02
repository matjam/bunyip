// The lit sprite shader: the texture times the tint, lit by up to eight
// point lights through a tangent-space normal map in image0. Lights are
// in the same units as sprite positions, hovering a height above the
// plane; each fades to nothing at its radius.
UNIFORMS uniform Lights {
    vec4 ambient;   // rgb ambient light, w = light count
    vec4 pos[8];    // xy position, z height above the plane, w radius
    vec4 color[8];  // rgb
} lights;

vec4 fragment(vec2 uv, vec4 color) {
    vec4 albedo = texture(tex, uv) * color;
    vec3 n = texture(image0, uv).xyz * 2.0 - 1.0;
    n.y = -n.y; // normal maps are painted y-up; the view's y points down
    n = normalize(n);
    vec3 light = lights.ambient.rgb;
    int count = int(lights.ambient.w);
    vec2 p = position();
    for (int i = 0; i < count; i++) {
        vec3 d = vec3(lights.pos[i].xy - p, lights.pos[i].z);
        float dist = max(length(d), 1e-3);
        float att = clamp(1.0 - dist / max(lights.pos[i].w, 1e-3), 0.0, 1.0);
        light += lights.color[i].rgb * max(dot(n, d / dist), 0.0) * att * att;
    }
    return vec4(albedo.rgb * light, albedo.a);
}
