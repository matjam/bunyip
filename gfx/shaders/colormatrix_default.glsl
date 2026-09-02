// The colour-matrix sprite shader: the texture times the tint, then the
// straight colour through a 4x4 matrix and an offset, for hue, saturation,
// brightness, contrast and inversion without a shader of the game's own.
UNIFORMS uniform Matrix {
    mat4 m;
    vec4 offset;
} cm;

vec4 fragment(vec2 uv, vec4 color) {
    vec4 c = texture(tex, uv) * color;
    float a = c.a;
    vec3 rgb = a > 0.0 ? c.rgb / a : c.rgb;
    rgb = (cm.m * vec4(rgb, 1.0)).rgb + cm.offset.rgb;
    return vec4(clamp(rgb, 0.0, 1.0) * a, a);
}
