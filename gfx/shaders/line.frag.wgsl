var<private> vColor: vec4f;
var<private> outColor: vec4f;

fn effect() { outColor = vColor; }

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vColorIn: vec4f) -> EffectOutput {
    vColor = vColorIn;
    effect();
    return EffectOutput(outColor);
}
