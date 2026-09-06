// Both texture and tint are premultiplied; blending is ONE, ONE_MINUS_SRC_ALPHA.
fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    return textureSample(tex, texSampler, uv) * color;
}
