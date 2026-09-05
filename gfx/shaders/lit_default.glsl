// The lit sprite shader: the texture times the tint, lit by up to eight
// point lights through a tangent-space normal map in image0. Lights are
// in the same units as sprite positions, hovering a height above the
// plane; each fades to nothing at its radius. image1 holds the polar
// shadow maps, one row per light: the distance to the nearest occluder
// along each direction, as a fraction of the light's radius in sixteen
// bits across the red and green channels.
UNIFORMS uniform Lights {
    vec4 ambient;   // rgb ambient light, w = light count
    vec4 pos[8];    // xy position, z height above the plane, w radius
    vec4 color[8];  // rgb
    vec4 shadow[8]; // x casts shadows, y the row, z softness, w directions per row
} lights;

const float invTau = 0.15915494; // 1 / (2 pi)

// visible is how much of a light reaches a point: 1 in the open, 0 in
// shadow, and a soft edge between. d is the offset from the light to the
// point in the sprite plane, the direction the map is indexed by, and
// dist is its length.
float visible(vec4 shadow, float radius, vec2 d, float dist) {
    if (shadow.x < 0.5) {
        return 1.0;
    }
    float u = atan(d.y, d.x) * invTau + 0.5;
    // The softness is a width at the shadowed point, which is an angle at
    // the light and so a step along the row. Half a texel is the hardest
    // edge the map can hold.
    float spread = max(shadow.z * 0.5 / max(dist, 1e-3) * invTau, 0.5 / max(shadow.w, 1.0));
    float lit = 0.0;
    for (int t = -2; t <= 2; t++) {
        vec2 texel = texture(image1, vec2(fract(u + float(t) * spread * 0.5), shadow.y)).rg;
        float stored = (texel.r * 65280.0 + texel.g * 255.0) / 65535.0 * radius;
        lit += step(dist, stored);
    }
    return lit * 0.2;
}

vec4 fragment(vec2 uv, vec4 color) {
    vec4 albedo = texture(tex, uv) * color;
    vec3 n = texture(image0, uv).xyz * 2.0 - 1.0;
    n.y = -n.y; // normal maps are painted y-up; the view's y points down
    n = normalize(n);
    vec3 light = lights.ambient.rgb;
    int count = int(lights.ambient.w);
    vec2 p = position();
    for (int i = 0; i < count; i++) {
        vec2 toLight = lights.pos[i].xy - p;
        vec3 d = vec3(toLight, lights.pos[i].z);
        float dist = max(length(d), 1e-3);
        float radius = max(lights.pos[i].w, 1e-3);
        float att = clamp(1.0 - dist / radius, 0.0, 1.0);
        // The map is indexed by the direction from the light outwards.
        float reach = visible(lights.shadow[i], radius, -toLight, length(toLight));
        light += lights.color[i].rgb * max(dot(n, d / dist), 0.0) * att * att * reach;
    }
    return vec4(albedo.rgb * light, albedo.a);
}
