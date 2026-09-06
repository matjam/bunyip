// God rays: the sky is the light source, so anything the depth buffer
// says is geometry blocks it. Each pixel walks towards the sun's place on
// screen, gathering the unoccluded steps and fading them as it goes, and
// what comes out is the shafts between an occluder's edges.
@group(0) @binding(0) var depthTex: texture_2d<f32>;
@group(0) @binding(1) var depthTexSampler: sampler;

struct PC {
    a: vec4f, // xy = the sun in texture coordinates, z = strength, w = decay
    b: vec4f, // x = sample count, y = density, z = step weight
    c: vec4f, // rgb = the light's colour
    d: vec4f,
}
var<push_constant> pc: PC;

var<private> vUV: vec2f;
var<private> outColor: vec4f;

fn effect() {
    var taps: i32 = max(i32(pc.b.x), 2);
    var delta: vec2f = (vUV - pc.a.xy) * (pc.b.y / f32(taps));
    var uv: vec2f = vUV;
    var fade: f32 = 1.0;
    var sum: f32 = 0.0;
    for (var i: i32 = 0; i < taps; i++) {
        uv -= delta;
        // Depth 1 is sky: nothing between this step and the sun.
        var lit: f32 = select(0.0, 1.0, textureSampleLevel(depthTex, depthTexSampler, clamp(uv, vec2f(0.0), vec2f(1.0)), 0.0).r >= 1.0);
        sum += lit * fade * pc.b.z;
        fade *= pc.a.w;
    }
    outColor = vec4f(pc.c.rgb * (sum * pc.a.z), 1.0);
}

struct EffectOutput {
    @location(0) outColor: vec4f,
}
@fragment fn main(@location(0) vUVIn: vec2f) -> EffectOutput {
    vUV = vUVIn;
    effect();
    return EffectOutput(outColor);
}
