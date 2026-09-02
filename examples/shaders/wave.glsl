// A 2D shader: ripples the texture coordinates over time and tints by
// the extra image, a noise texture.
UNIFORMS uniform Params {
    float amplitude;
    float frequency;
} u;

vec4 fragment(vec2 uv, vec4 color) {
    float n = texture(image0, uv * 2.0 + vec2(time() * 0.1, 0.0)).r;
    uv.x += sin(uv.y * u.frequency + time() * 3.0) * u.amplitude * n;
    vec4 c = texture(tex, uv) * color;
    return c * vec4(1.0, 0.85 + 0.15 * n, 0.7 + 0.3 * n, 1.0);
}
