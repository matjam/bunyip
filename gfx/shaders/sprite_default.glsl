// The default 2D shader: the texture times the tint. Textures and tints
// are premultiplied; the pipeline blends ONE, ONE_MINUS_SRC_ALPHA.
vec4 fragment(vec2 uv, vec4 color) {
    return texture(tex, uv) * color;
}
