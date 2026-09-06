struct Params { heat: f32, };
@group(4) @binding(0) var<uniform> u: Params;

fn surface(input: Surface) -> Surface {
    var s = input;
    let p = s.worldPos.xz * 1.5 + vec2f(time() * 0.05, 0.0);
    let n = sampleImage0(p * 0.25).r;
    let crack = smoothstep(0.45, 0.55, n);
    let pulse = 0.6 + 0.4 * sin(time() * 2.0 + n * 12.0);
    s.albedo = mix(vec3f(0.05, 0.04, 0.04), vec3f(0.2, 0.1, 0.08), n);
    s.roughness = mix(0.95, 0.4, crack);
    s.emissive += vec3f(1.0, 0.35, 0.05) * crack * pulse * u.heat;
    return s;
}

fn finish(lit: vec4f, s: Surface) -> vec4f {
    let rim = smoothstep(0.0, 0.5, 1.0 - abs(s.uv.x - 0.5) * 2.0) * smoothstep(0.0, 0.5, 1.0 - abs(s.uv.y - 0.5) * 2.0);
    return vec4f(lit.rgb * mix(0.3, 1.0, rim), lit.a);
}
