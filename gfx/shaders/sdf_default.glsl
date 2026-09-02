// Signed-distance-field text: the atlas stores distance to the glyph edge
// with 0.5 on the outline; the derivative width keeps edges one pixel soft
// at any scale.
vec4 fragment(vec2 uv, vec4 color) {
    float d = texture(tex, uv).a;
    float w = max(fwidth(d) * 0.75, 0.002);
    float a = smoothstep(0.5 - w, 0.5 + w, d);
    return color * a;
}
