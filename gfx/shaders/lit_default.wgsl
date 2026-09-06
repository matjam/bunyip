// Normal-mapped sprite lights. image1 stores one polar occluder row per light,
// with distance divided by radius encoded as sixteen bits in red and green.
struct Lights {
    ambient: vec4f, // RGB ambient, W light count
    pos: array<vec4f, 8>, // XY position, Z height, W radius
    color: array<vec4f, 8>,
    shadow: array<vec4f, 8>, // enabled, row, softness, directions per row
}
@group(1) @binding(0) var<uniform> lights: Lights;
const invTau: f32 = 0.15915494;

fn visible(shadow: vec4f, radius: f32, d: vec2f, dist: f32) -> f32 {
    if (shadow.x < 0.5) { return 1.0; }
    let u = atan2(d.y, d.x) * invTau + 0.5;
    let spread = max(shadow.z * 0.5 / max(dist, 1e-3) * invTau, 0.5 / max(shadow.w, 1.0));
    var lit = 0.0;
    for (var t = -2; t <= 2; t++) {
        let texel = textureSampleLevel(image1, image1Sampler, vec2f(fract(u + f32(t) * spread * 0.5), shadow.y), 0.0).rg;
        let stored = (texel.r * 65280.0 + texel.g * 255.0) / 65535.0 * radius;
        lit += step(dist, stored);
    }
    return lit * 0.2;
}

fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    let albedo = textureSample(tex, texSampler, uv) * color;
    var n = textureSample(image0, image0Sampler, uv).xyz * 2.0 - vec3f(1.0);
    n.y = -n.y; // normal maps are Y-up; view coordinates are Y-down
    n = normalize(n);
    var light = lights.ambient.rgb;
    let count = i32(lights.ambient.w);
    let p = position();
    for (var i = 0; i < count; i++) {
        let toLight = lights.pos[i].xy - p;
        let d = vec3f(toLight, lights.pos[i].z);
        let dist = max(length(d), 1e-3);
        let radius = max(lights.pos[i].w, 1e-3);
        let att = clamp(1.0 - dist / radius, 0.0, 1.0);
        let reach = visible(lights.shadow[i], radius, -toLight, length(toLight));
        light += lights.color[i].rgb * max(dot(n, d / dist), 0.0) * att * att * reach;
    }
    return vec4f(albedo.rgb * light, albedo.a);
}
