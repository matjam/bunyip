// The atlas holds signed distance; only coverage outside the fill is drawn.
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var texSampler: sampler;
struct TextOutline {
    outlineColor: vec4f,
    parameters: vec4f, // x: outer threshold; remaining words reserved
}
@group(1) @binding(0) var<uniform> outline: TextOutline;

@fragment fn main(@location(0) uv: vec2f) -> @location(0) vec4f {
    let d = textureSample(tex, texSampler, uv).a;
    let w = max(fwidth(d) * 0.75, 0.002);
    let outside = smoothstep(outline.parameters.x - w, outline.parameters.x + w, d);
    let inside = smoothstep(0.5 - w, 0.5 + w, d);
    return outline.outlineColor * max(outside - inside, 0.0);
}
