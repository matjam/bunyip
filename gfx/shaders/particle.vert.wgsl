var<private> positionOut: vec4f;
var<private> vertexIndexValue: u32;

// Instanced 2D particles: one quad per instance, expanded here from a
// centre, a size, a rotation and a pair of texture coordinates. The six
// vertices come from vertexIndexValue, so there is no per-vertex buffer.
var<private> iPos: vec3f;      // z is unused in 2D
var<private> iRotation: f32;
var<private> iSize: vec2f;
var<private> iUV0: vec2f;
var<private> iUV1: vec2f;
var<private> iColor: vec4f;    // straight alpha; the fragment stage premultiplies

struct PC {
    proj: mat4x4f,
    frame: vec4f, // x time in seconds, y view width, z view height, w pixels per view unit
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> vColor: vec4f;

fn effect() {
    let corners: array<vec2f, 6> = array<vec2f, 6>(
    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(0.5, 0.5),
    vec2f(-0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5));

    var c: vec2f = corners[vertexIndexValue] * iSize;
    var s: f32 = sin(iRotation);
    var co: f32 = cos(iRotation);
    var p: vec2f = iPos.xy + vec2f(c.x * co - c.y * s, c.x * s + c.y * co);
    positionOut = pc.proj * vec4f(p, 0.0, 1.0);
    vUV = mix(iUV0, iUV1, corners[vertexIndexValue] + 0.5);
    vColor = iColor;
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vUV: vec2f,
    @location(1) vColor: vec4f,
}
@vertex fn main(@location(0) iPosIn: vec3f, @location(1) iRotationIn: f32, @location(2) iSizeIn: vec2f, @location(3) iUV0In: vec2f, @location(4) iUV1In: vec2f, @location(5) iColorIn: vec4f, @builtin(vertex_index) vertexIndex: u32) -> EffectOutput {
    iPos = iPosIn;
    iRotation = iRotationIn;
    iSize = iSizeIn;
    iUV0 = iUV0In;
    iUV1 = iUV1In;
    iColor = iColorIn;
    vertexIndexValue = vertexIndex;
    effect();
    return EffectOutput(positionOut, vUV, vColor);
}
