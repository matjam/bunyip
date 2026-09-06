var<private> positionOut: vec4f;

// Outline and x-ray pass: the mesh drawn again as a solid colour. For
// outlines each vertex is pushed out along its normal by a width in
// pixels, so the shell shows around the silhouette where the stencil
// says the mesh itself is not.
var<private> iPos: vec3f;
var<private> iNormal: vec3f;
var<private> iModel0: vec4f;
var<private> iModel1: vec4f;
var<private> iModel2: vec4f;

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
@group(0) @binding(0) var<uniform> frame: Frame;

struct PC {
    color: vec4f,
    params: vec4f, // x outline width in pixels, y viewport width, z viewport height
}
var<push_constant> pc: PC;

fn effect() {
    var m: mat4x4f = mat4x4f(vec4f(iModel0.x, iModel1.x, iModel2.x, 0.0), vec4f(iModel0.y, iModel1.y, iModel2.y, 0.0),
    vec4f(iModel0.z, iModel1.z, iModel2.z, 0.0), vec4f(iModel0.w, iModel1.w, iModel2.w, 1.0));
    var world: vec4f = m * vec4f(iPos, 1.0);
    var clip: vec4f = frame.viewProj * world;
    if (pc.params.x > 0.0) {
        var n: vec3f = normalize(mat3x3f(m[0].xyz, m[1].xyz, m[2].xyz) * iNormal);
        var clipN: vec4f = frame.viewProj * vec4f(world.xyz + n, 1.0);
        var dir: vec2f = clipN.xy / clipN.w - clip.xy / clip.w;
        var len: f32 = length(dir);
        if (len > 1e-5) {
            dir /= len;
            clip = vec4f(clip.xy + dir * pc.params.x * 2.0 / pc.params.yz * clip.w, clip.zw);
        }
    }
    positionOut = clip;
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
}
@vertex fn main(@location(0) iPosIn: vec3f, @location(1) iNormalIn: vec3f, @location(5) iModel0In: vec4f, @location(6) iModel1In: vec4f, @location(7) iModel2In: vec4f) -> EffectOutput {
    iPos = iPosIn;
    iNormal = iNormalIn;
    iModel0 = iModel0In;
    iModel1 = iModel1In;
    iModel2 = iModel2In;
    effect();
    return EffectOutput(positionOut);
}
