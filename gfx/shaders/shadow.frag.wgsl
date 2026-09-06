// Alpha-cutout depth pass. Gradients are evaluated before selecting a sampler.
@group(0) @binding(0) var tAlbedo: texture_2d<f32>;
@group(0) @binding(17) var materialSampler0: sampler;
@group(0) @binding(18) var materialSampler1: sampler;
@group(0) @binding(19) var materialSampler2: sampler;
@group(0) @binding(20) var materialSampler3: sampler;
@fragment fn main(@location(0) uv: vec2f, @location(1) @interpolate(flat) cutout: vec3f) {
    let dx = dpdx(uv); let dy = dpdy(uv);
    var alpha: f32;
    switch i32(cutout.z) {
    case 0: { alpha = textureSampleGrad(tAlbedo, materialSampler0, uv, dx, dy).a; }
    case 1: { alpha = textureSampleGrad(tAlbedo, materialSampler1, uv, dx, dy).a; }
    case 2: { alpha = textureSampleGrad(tAlbedo, materialSampler2, uv, dx, dy).a; }
    default: { alpha = textureSampleGrad(tAlbedo, materialSampler3, uv, dx, dy).a; }
    }
    if cutout.y > 0.0 && alpha * cutout.x < cutout.y { discard; }
}
