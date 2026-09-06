var<private> positionOut: vec4f;

// Debug lines: world-space positions with a colour each, no lighting.
var<private> iPos: vec3f;
var<private> iColor: vec4f;

struct PC {
    viewProj: mat4x4f,
}
var<push_constant> pc: PC;

var<private> vColor: vec4f;

fn effect() {
    positionOut = pc.viewProj * vec4f(iPos, 1.0);
    vColor = iColor;
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vColor: vec4f,
}
@vertex fn main(@location(0) iPosIn: vec3f, @location(1) iColorIn: vec4f) -> EffectOutput {
    iPos = iPosIn;
    iColor = iColorIn;
    effect();
    return EffectOutput(positionOut, vColor);
}
