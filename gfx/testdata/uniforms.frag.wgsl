// Keep scalar-array, nested-struct, vec3+scalar and matrix-array members:
// they distinguish Go memory layout from the engine's std140 packing.
// WGSL has no std140 switch: explicit wrapper size/alignment supplies its
// sixteen-byte scalar-array stride and nested-structure alignment.
struct Scalar { @size(16) value: f32, }
struct Pair { @align(16) xy: vec2<f32>, z: f32, }
struct Parameters {
    vector: vec3<f32>,
    scalar: f32,
    values: array<Scalar, 2>,
    pairs: array<Pair, 2>,
    matrices: array<mat3x3<f32>, 2>,
    enabled: u32,
    signedValue: i32,
    unsignedValue: u32,
    colour: vec4<f32>,
    transform: mat4x4<f32>,
}
@group(1) @binding(0) var<uniform> u: Parameters;

@fragment fn main() -> @location(0) vec4<f32> {
    var good = u.vector.x == 1 && u.vector.y == 2 && u.vector.z == 3 && u.scalar == 4
        && u.values[0].value == 5 && u.values[1].value == 6
        && u.pairs[0].xy.x == 7 && u.pairs[0].xy.y == 8 && u.pairs[0].z == 9
        && u.pairs[1].xy.x == 10 && u.pairs[1].xy.y == 11 && u.pairs[1].z == 12
        && u.enabled != 0u && u.signedValue == -13 && u.unsignedValue == 14u
        && u.colour.x == 0.25 && u.colour.y == 0.5 && u.colour.z == 0.75 && u.colour.w == 1;
    for (var m = 0; m < 2; m++) {
        for (var c = 0; c < 3; c++) {
            for (var r = 0; r < 3; r++) {
                good = good && u.matrices[m][c][r] == f32(m*9+c*3+r+20);
            }
        }
    }
    for (var c = 0; c < 4; c++) {
        for (var r = 0; r < 4; r++) {
            good = good && u.transform[c][r] == f32(c*4+r+40);
        }
    }
    return select(vec4<f32>(1,0,0,1), vec4<f32>(0,1,0,1), good);
}
