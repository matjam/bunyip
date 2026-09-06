// Apply the colour transform in straight colour, then premultiply again.
struct Matrix {
    m: mat4x4f,
    offset: vec4f,
}
@group(1) @binding(0) var<uniform> cm: Matrix;

fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    let c = textureSample(tex, texSampler, uv) * color;
    let a = c.a;
    var rgb = c.rgb;
    if (a > 0.0) { rgb /= a; }
    rgb = (cm.m * vec4f(rgb, 1.0)).rgb + cm.offset.rgb;
    return vec4f(clamp(rgb, vec3f(0.0), vec3f(1.0)) * a, a);
}
