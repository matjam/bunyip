// Four tiling layers blended by a splat map.
struct Params { scale: vec4f, rough: vec4f, }
@group(4) @binding(0) var<uniform> u: Params;
fn surface(_s: Surface) -> Surface {
var s = _s;
var w: vec4f = albedoTex(s.uv);
var total: f32 = (((w.r + w.g) + w.b) + w.a);
if (total < 1e-4) {
w = vec4f(1.0, 0.0, 0.0, 0.0);
total = 1.0;
}
var p: vec2f = s.worldPos.xz;
var albedo: vec3f = ((((image0((p / vec2f(max(u.scale.x, 1e-3)))).rgb * w.r) + (image1((p / vec2f(max(u.scale.y, 1e-3)))).rgb * w.g)) + (image2((p / vec2f(max(u.scale.z, 1e-3)))).rgb * w.b)) + (image3((p / vec2f(max(u.scale.w, 1e-3)))).rgb * w.a));
s.roughness = clamp((dot(u.rough, w) / total), 0.04, 1.0);
s.albedo = (((albedo / vec3f(total)) * vBaseColor.rgb) * s.color.rgb);
s.alpha = 1.0;
return s;
}
