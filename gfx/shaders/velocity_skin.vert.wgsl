var<private> positionOut: vec4f;

// Velocity pass, skinned meshes. The skinning matrix is this frame's for
// both positions, so the motion vector carries the model matrix's motion
// and not the pose's: a character that walks across the screen reprojects
// correctly, an arm swinging in place does not.
var<private> iPos: vec3f;
var<private> iModel0: vec4f;
var<private> iModel1: vec4f;
var<private> iModel2: vec4f;
var<private> iPrev0: vec4f;
var<private> iPrev1: vec4f;
var<private> iPrev2: vec4f;
var<private> iExtra: vec4f; // x = the instance's first joint matrix
var<private> iJoints: vec4u;
var<private> iWeights: vec4f;

struct Joints {
    joints: array<mat4x4f>,
}
@group(0) @binding(0) var<storage, read> jointsBlock: Joints;

struct PC {
    viewProj: mat4x4f,
    prevViewProj: mat4x4f,
}
var<push_constant> pc: PC;

var<private> vNow: vec4f;
var<private> vThen: vec4f;

fn rows(a: vec4f, b: vec4f, c: vec4f) -> mat4x4f {
    return mat4x4f(vec4f(a.x, b.x, c.x, 0.0), vec4f(a.y, b.y, c.y, 0.0),
    vec4f(a.z, b.z, c.z, 0.0), vec4f(a.w, b.w, c.w, 1.0));
}

fn skinMatrix() -> mat4x4f {
    var base: u32 = u32(iExtra.x);
    return iWeights.x * jointsBlock.joints[base + iJoints.x] + iWeights.y * jointsBlock.joints[base + iJoints.y]
    + iWeights.z * jointsBlock.joints[base + iJoints.z] + iWeights.w * jointsBlock.joints[base + iJoints.w];
}

fn effect() {
    var local: vec4f = skinMatrix() * vec4f(iPos, 1.0);
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
@vertex fn main(@location(0) iPosIn: vec3f, @location(1) iModel0In: vec4f, @location(2) iModel1In: vec4f, @location(3) iModel2In: vec4f, @location(4) iPrev0In: vec4f, @location(5) iPrev1In: vec4f, @location(6) iPrev2In: vec4f, @location(7) iExtraIn: vec4f, @location(8) iJointsIn: vec4u, @location(9) iWeightsIn: vec4f) -> EffectOutput {
    iPos = iPosIn;
    iModel0 = iModel0In;
    iModel1 = iModel1In;
    iModel2 = iModel2In;
    iPrev0 = iPrev0In;
    iPrev1 = iPrev1In;
    iPrev2 = iPrev2In;
    iExtra = iExtraIn;
    iJoints = iJointsIn;
    iWeights = iWeightsIn;
    effect();
    return EffectOutput(positionOut, vNow, vThen);
}
