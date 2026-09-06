struct Params { strength: f32, };
@group(4) @binding(0) var<uniform> u: Params;

fn vertex(input: VertexData) -> VertexData {
    var v = input;
    let free = v.uv.x;
    let wave = sin(v.uv.x * 6.0 - time() * 4.0) + 0.5 * sin(v.uv.y * 4.0 - time() * 6.0);
    v.position.z += wave * 0.15 * free * u.strength;
    let slope = cos(v.uv.x * 6.0 - time() * 4.0) * 6.0 * 0.15 * free * u.strength;
    v.normal = normalize(vec3f(-slope * 0.5, 0.0, 1.0));
    return v;
}

fn surface(input: Surface) -> Surface {
    var s = input;
    let band = step(0.5, fract(s.uv.y * 3.0));
    s.albedo = mix(vec3f(0.9, 0.2, 0.15), vec3f(0.95, 0.95, 0.9), band);
    s.roughness = 0.8;
    return s;
}
