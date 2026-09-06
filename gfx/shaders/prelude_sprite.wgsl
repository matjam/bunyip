// Bunyip sprite bindings. Textures contain linear premultiplied colours.
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var texSampler: sampler;
@group(0) @binding(2) var image0: texture_2d<f32>;
@group(0) @binding(3) var image0Sampler: sampler;
@group(0) @binding(4) var image1: texture_2d<f32>;
@group(0) @binding(5) var image1Sampler: sampler;
@group(0) @binding(6) var image2: texture_2d<f32>;
@group(0) @binding(7) var image2Sampler: sampler;
@group(0) @binding(8) var image3: texture_2d<f32>;
@group(0) @binding(9) var image3Sampler: sampler;

struct SpriteFrame {
    proj: mat4x4f,
    frame: vec4f, // time, view width, view height, pixels per view unit
}
var<push_constant> spriteFrame: SpriteFrame;
var<private> spritePosition: vec2f;

// Seconds since the game started.
fn time() -> f32 { return spriteFrame.frame.x; }
// Size of the current 2D view in view units.
fn viewSize() -> vec2f { return spriteFrame.frame.yz; }
// Framebuffer pixels per view unit.
fn pixelScale() -> f32 { return spriteFrame.frame.w; }
// Current fragment position in view units, before projection.
fn position() -> vec2f { return spritePosition; }
