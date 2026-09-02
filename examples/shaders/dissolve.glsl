// A 2D shader: burns the sprite away along a noise image, with a glowing
// edge, as progress goes from 0 to 1.
UNIFORMS uniform Params {
    float progress;
    float edge;
} u;

vec4 fragment(vec2 uv, vec4 color) {
    vec4 c = texture(tex, uv) * color;
    float n = texture(image0, uv).r;
    float cut = u.progress * (1.0 + u.edge);
    if (n < cut - u.edge) return vec4(0.0);
    float glow = 1.0 - clamp((n - (cut - u.edge)) / u.edge, 0.0, 1.0);
    vec3 fire = vec3(1.0, 0.5, 0.1) * glow * 2.0 * c.a;
    return vec4(c.rgb + fire, c.a);
}
