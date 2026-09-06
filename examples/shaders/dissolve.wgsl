struct Params { progress: f32, edge: f32, };
@group(1) @binding(0) var<uniform> u: Params;

fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    let c = textureSample(tex, texSampler, uv) * color;
    let n = textureSample(image0, image0Sampler, uv).r;
    let cut = u.progress * (1.0 + u.edge);
    if n < cut - u.edge { return vec4f(0.0); }
    let glow = 1.0 - clamp((n - (cut - u.edge)) / u.edge, 0.0, 1.0);
    let fire = vec3f(1.0, 0.5, 0.1) * glow * 2.0 * c.a;
    return vec4f(c.rgb + fire, c.a);
}
