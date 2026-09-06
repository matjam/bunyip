// A flat colour from the push constants: outlines and x-ray tints.
struct PC {
    color: vec4f,
    params: vec4f,
}
var<push_constant> pc: PC;

var<private> outColor: vec4f;

fn effect() {
    outColor = pc.color;
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main() -> EffectOutput {

    effect();
    return EffectOutput(outColor);
}
