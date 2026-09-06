var<private> positionOut: vec4f;

// Velocity pass, static meshes. It rasterises with the same jittered
// projection as the scene pass so the motion vectors land on the pixels
// the scene wrote, and it measures each vertex's motion through the
// previous frame's projection alone. What comes out is the object's own
// motion: zero for anything that did not move, because the resolve
// passes reconstruct the camera's part from depth and add this to it.
var<private> iPos: vec3f;
var<private> iModel0: vec4f;
var<private> iModel1: vec4f;
var<private> iModel2: vec4f;
var<private> iPrev0: vec4f;
var<private> iPrev1: vec4f;
var<private> iPrev2: vec4f;

struct PC {
    viewProj: mat4x4f, // this frame's, jittered
    prevViewProj: mat4x4f, // the previous frame's, not jittered
}
var<push_constant> pc: PC;

var<private> vNow: vec4f;  // where the vertex is now
var<private> vThen: vec4f; // where it was, both in the previous clip space

// rows rebuilds a model matrix from the three instance rows.
fn rows(a: vec4f, b: vec4f, c: vec4f) -> mat4x4f {
    return mat4x4f(vec4f(a.x, b.x, c.x, 0.0), vec4f(a.y, b.y, c.y, 0.0),
    vec4f(a.z, b.z, c.z, 0.0), vec4f(a.w, b.w, c.w, 1.0));
}

fn effect() {
    var local: vec4f = vec4f(iPos, 1.0);
    var world: vec4f = rows(iModel0, iModel1, iModel2) * local;
    positionOut = pc.viewProj * world;
    vNow = pc.prevViewProj * world;
    vThen = pc.prevViewProj * (rows(iPrev0, iPrev1, iPrev2) * local);
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vNow: vec4f,
    @location(1) vThen: vec4f,
}
@vertex fn main(@location(0) iPosIn: vec3f, @location(1) iModel0In: vec4f, @location(2) iModel1In: vec4f, @location(3) iModel2In: vec4f, @location(4) iPrev0In: vec4f, @location(5) iPrev1In: vec4f, @location(6) iPrev2In: vec4f) -> EffectOutput {
    iPos = iPosIn;
    iModel0 = iModel0In;
    iModel1 = iModel1In;
    iModel2 = iModel2In;
    iPrev0 = iPrev0In;
    iPrev1 = iPrev1In;
    iPrev2 = iPrev2In;
    effect();
    return EffectOutput(positionOut, vNow, vThen);
}
