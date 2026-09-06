// Atlas distance is 0.5 at the edge; derivative width keeps it one pixel soft.
fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    let d = textureSample(tex, texSampler, uv).a;
    let w = max(fwidth(d) * 0.75, 0.002);
    return color * smoothstep(0.5 - w, 0.5 + w, d);
}
