@group(3) @binding(0) var<storage, read> joints: array<mat4x4f>;
fn skinMatrix(j: vec4u, w: vec4f) -> mat4x4f { let base = u32(iExtra.x); return w.x * joints[base+j.x] + w.y * joints[base+j.y] + w.z * joints[base+j.z] + w.w * joints[base+j.w]; }
