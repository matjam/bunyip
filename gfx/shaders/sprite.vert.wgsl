var<private> positionOut: vec4f;

// The 2D vertex stream: sprites, text, shapes and strokes all arrive as
// plain triangles with a position in view units, a texture coordinate
// and a premultiplied colour.
var<private> iPos: vec2f;
var<private> iUV: vec2f;
var<private> iColor: vec4f;

struct PC {
    proj: mat4x4f,
    frame: vec4f, // x time in seconds, y view width, z view height, w pixels per view unit
    transformX: vec4f,
    transformY: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> vColor: vec4f;
var<private> vPos: vec2f;

fn effect() {
    var pos: vec2f = vec2f(dot(pc.transformX.xyz, vec3f(iPos, 1.0)),
    dot(pc.transformY.xyz, vec3f(iPos, 1.0)));
    positionOut = pc.proj * vec4f(pos, 0.0, 1.0);
    vUV = iUV;
    vColor = iColor;
    vPos = pos;
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vUV: vec2f,
    @location(1) vColor: vec4f,
    @location(2) vPos: vec2f,
}
@vertex fn main(@location(0) iPosIn: vec2f, @location(1) iUVIn: vec2f, @location(2) iColorIn: vec4f) -> EffectOutput {
    iPos = iPosIn;
    iUV = iUVIn;
    iColor = iColorIn;
    effect();
    return EffectOutput(positionOut, vUV, vColor, vPos);
}
