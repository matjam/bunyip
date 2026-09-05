// The built-in terrain shader, behind gfx.Terrain. The material's albedo
// slot holds the splat map, whose four channels are the weights of the
// four tiling layers in image0 to image3. Each layer repeats every
// scale world units across the ground plane, so a layer's own texture
// resolution is independent of the terrain's size.
UNIFORMS uniform Params {
    vec4 scale; // world units per repeat of each layer
    vec4 rough; // roughness of each layer
} u;

void surface(inout Surface s) {
    vec4 w = texture(albedoTex, s.uv);
    float total = w.r + w.g + w.b + w.a;
    // A splat map that names no layer anywhere falls back to the first,
    // which is what a terrain built without one gets.
    if (total < 1e-4) {
        w = vec4(1.0, 0.0, 0.0, 0.0);
        total = 1.0;
    }
    vec2 p = s.worldPos.xz;
    vec3 albedo = texture(image0, p / max(u.scale.x, 1e-3)).rgb * w.r
                + texture(image1, p / max(u.scale.y, 1e-3)).rgb * w.g
                + texture(image2, p / max(u.scale.z, 1e-3)).rgb * w.b
                + texture(image3, p / max(u.scale.w, 1e-3)).rgb * w.a;
    s.roughness = clamp(dot(u.rough, w) / total, 0.04, 1.0);
    // The albedo slot is the splat map, so the material's base colour and
    // the mesh's vertex colour are applied here instead: a game can still
    // tint the whole ground or shade it by height and slope.
    s.albedo = albedo / total * vBaseColor.rgb * s.color.rgb;
    s.alpha = 1.0;
}
