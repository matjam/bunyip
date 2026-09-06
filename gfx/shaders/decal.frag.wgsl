var<private> fragCoordValue: vec4f;

// Projects a texture down the box's y axis onto the scene: the depth
// buffer gives the world position under each pixel, the inverse box
// matrix says whether it lies inside, and the box's x and z become the
// texture coordinates. Surfaces facing away from the projection fade out.
@group(0) @binding(0) var depthTex: texture_2d<f32>;
@group(0) @binding(1) var depthTexSampler: sampler;
@group(2) @binding(0) var decalTex: texture_2d<f32>;
@group(2) @binding(1) var decalTexSampler: sampler;

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
    box: mat4x4f,
    invBox0: vec4f,
    invBox1: vec4f,
    invBox2: vec4f,
    tint: vec4f,
}
var<push_constant> pc: PC;

var<private> outColor: vec4f;

fn worldAt(uv: vec2f) -> vec3f {
    var d: f32 = textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r;
    var p: vec4f = frame.invViewProj * vec4f(uv * 2.0 - 1.0, d, 1.0);
    return p.xyz / p.w;
}

fn effect() {
    var size: vec2f = vec2f(textureDimensions(depthTex, 0));
    var uv: vec2f = fragCoordValue.xy / size;
    let sky = textureSampleLevel(depthTex, depthTexSampler, uv, 0.0).r >= 1.0;
    var world: vec3f = worldAt(uv);
    var local: vec3f = vec3f(dot(pc.invBox0, vec4f(world, 1.0)), dot(pc.invBox1, vec4f(world, 1.0)), dot(pc.invBox2, vec4f(world, 1.0)));
    let outside = abs(local.x) > 0.5 || abs(local.y) > 0.5 || abs(local.z) > 0.5;
    // The surface normal from neighbouring depths, against the box's
    // projection axis (world y of the box).
    var dx: vec3f = worldAt(uv + vec2f(1.0 / size.x, 0.0)) - world;
    var dy: vec3f = worldAt(uv + vec2f(0.0, 1.0 / size.y)) - world;
    var n: vec3f = normalize(cross(dx, dy));
    var axis: vec3f = normalize(vec3f(pc.invBox1.xyz));
    var facing: f32 = abs(dot(n, axis));
    var fade: f32 = smoothstep(0.2, 0.5, facing);
    var duv: vec2f = vec2f(local.x + 0.5, 0.5 - local.z);
    // Compute mip gradients across the whole quad before rejecting pixels.
    let du = dpdx(duv);
    let dv = dpdy(duv);
    if (sky || outside) { discard; }
    outColor = textureSampleGrad(decalTex, decalTexSampler, duv, du, dv) * pc.tint * fade;
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@builtin(position) fragCoord: vec4f) -> EffectOutput {
    fragCoordValue = fragCoord;
    effect();
    return EffectOutput(outColor);
}
