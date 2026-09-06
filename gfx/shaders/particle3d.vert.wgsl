var<private> positionOut: vec4f;
var<private> vertexIndexValue: u32;

// Instanced 3D particles: each instance is a camera-facing quad built
// here from the camera's right and up axes, so no per-vertex buffer and
// no per-particle matrix are needed. The instance layout is the same one
// particle.vert reads, with the position in world units.
var<private> iPos: vec3f;
var<private> iRotation: f32; // radians about the view axis
var<private> iSize: vec2f;      // world units
var<private> iUV0: vec2f;
var<private> iUV1: vec2f;
var<private> iColor: vec4f;     // straight alpha; the fragment stage premultiplies

struct PC {
    viewProj: mat4x4f,
    right: vec4f, // xyz the camera's right axis, w the camera's x
    up: vec4f, // xyz the camera's up axis, w the camera's y
    params: vec4f, // x near plane, y far plane, z soft fade distance, w the camera's z
    mode: vec4f, // x is 1 for an orthographic camera, 0 for a perspective one
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> vColor: vec4f;
var<private> vViewZ: f32; // distance along the camera's forward axis

fn effect() {
    let corners: array<vec2f, 6> = array<vec2f, 6>(
    vec2f(-0.5, -0.5), vec2f(0.5, -0.5), vec2f(0.5, 0.5),
    vec2f(-0.5, -0.5), vec2f(0.5, 0.5), vec2f(-0.5, 0.5));

    var c: vec2f = corners[vertexIndexValue] * iSize;
    var s: f32 = sin(iRotation);
    var co: f32 = cos(iRotation);
    var r: vec2f = vec2f(c.x * co - c.y * s, c.x * s + c.y * co);
    var world: vec3f = iPos + pc.right.xyz * r.x + pc.up.xyz * r.y;
    positionOut = pc.viewProj * vec4f(world, 1.0);
    // The distance along the camera's forward axis, which the soft fade
    // compares against the scene's depth. The forward axis is the cross
    // product of the other two, so it need not be passed in, and this
    // holds for an orthographic camera where the clip w is 1.
    var camPos: vec3f = vec3f(pc.right.w, pc.up.w, pc.params.w);
    vViewZ = dot(world - camPos, cross(pc.up.xyz, pc.right.xyz));
    // The quad's v runs down the screen, matching the 2D convention, so
    // the same texture region draws the same way up in both paths.
    vUV = mix(iUV0, iUV1, corners[vertexIndexValue] + 0.5);
    vColor = iColor;
}

struct EffectOutput {
    @builtin(position) positionOut: vec4f,
    @location(0) vUV: vec2f,
    @location(1) vColor: vec4f,
    @location(2) vViewZ: f32,
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
    return EffectOutput(positionOut, vUV, vColor, vViewZ);
}
