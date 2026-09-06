// The object's motion in texture coordinates: where the surface is now
// less where it was, both seen through the previous frame's projection.
// Clip space spans two units across the screen and texture coordinates
// one, hence the half.
var<private> vNow: vec4f;
var<private> vThen: vec4f;
var<private> outVelocity: vec2f;

fn effect() {
    outVelocity = (vNow.xy / vNow.w - vThen.xy / vThen.w) * 0.5;
}

struct EffectOutput {
    @location(0) outVelocity: vec2f,
}
@fragment fn main(@location(0) vNowIn: vec4f, @location(1) vThenIn: vec4f) -> EffectOutput {
    vNow = vNowIn;
    vThen = vThenIn;
    effect();
    return EffectOutput(outVelocity);
}
