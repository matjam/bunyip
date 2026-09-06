struct Params { amplitude: f32, frequency: f32, };
@group(1) @binding(0) var<uniform> u: Params;

fn fragment(inputUV: vec2f, color: vec4f) -> vec4f {
    var uv = inputUV;
    let n = textureSample(image0, image0Sampler, uv * 2.0 + vec2f(time() * 0.1, 0.0)).r;
    uv.x += sin(uv.y * u.frequency + time() * 3.0) * u.amplitude * n;
    let c = textureSample(tex, texSampler, uv) * color;
    return c * vec4f(1.0, 0.85 + 0.15 * n, 0.7 + 0.3 * n, 1.0);
}
