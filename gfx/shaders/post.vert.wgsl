var<private> positionOut: vec4f;
var<private> vertexIndexValue: u32;

// One triangle covers the screen; UV comes from the vertex index.
var<private> vUV: vec2f;

fn effect() {
    var uv: vec2f = vec2f(f32((vertexIndexValue << 1u) & 2u), f32(vertexIndexValue & 2u));
    vUV = uv;
    positionOut = vec4f(uv * 2.0 - 1.0, 0.0, 1.0);
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vUV: vec2f,
}
@vertex fn main(@builtin(vertex_index) vertexIndex: u32) -> EffectOutput {
    vertexIndexValue = vertexIndex;
    effect();
    return EffectOutput(positionOut, vUV);
}
