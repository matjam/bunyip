var<private> positionOut: vec4f;

// A decal is a unit box projected onto whatever geometry lies inside it.
// The box is drawn with its front faces culled, so it works from inside.
var<private> iPos: vec3f;

struct Frame {
    viewProj: mat4x4f,
    view: mat4x4f,
    lightViewProj: array<mat4x4f, 3>,
    camPos: vec4f,
    lightDir: vec4f,
    lightColor: vec4f,
    sky: vec4f,
    ground: vec4f,
    params: vec4f,
    splits: vec4f,
    radii: vec4f,
    sh: array<vec4f, 9>,
    env: vec4f,
    invViewProj: mat4x4f,
}
@group(1) @binding(0) var<uniform> frame: Frame;

struct PC {
    box: mat4x4f, // unit box to world
    invBox0: vec4f, // world to box, three rows
    invBox1: vec4f,
    invBox2: vec4f,
    tint: vec4f,
}
var<push_constant> pc: PC;

fn effect() {
    positionOut = frame.viewProj * pc.box * vec4f(iPos, 1.0);
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
}
@vertex fn main(@location(0) iPosIn: vec3f) -> EffectOutput {
    iPos = iPosIn;
    effect();
    return EffectOutput(positionOut);
}
