#version 450

// Projects a texture down the box's y axis onto the scene: the depth
// buffer gives the world position under each pixel, the inverse box
// matrix says whether it lies inside, and the box's x and z become the
// texture coordinates. Surfaces facing away from the projection fade out.
layout(set = 0, binding = 0) uniform sampler2D depthTex;
layout(set = 2, binding = 0) uniform sampler2D decalTex;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3];
    vec4 camPos;
    vec4 lightDir;
    vec4 lightColor;
    vec4 sky;
    vec4 ground;
    vec4 params;
    vec4 splits;
    vec4 radii;
    vec4 pointPos[32];
    vec4 pointColor[32];
    vec4 spotDir[32];
    vec4 sh[9];
    vec4 env;
    mat4 invViewProj;
} frame;

layout(push_constant) uniform PC {
    mat4 box;
    vec4 invBox0;
    vec4 invBox1;
    vec4 invBox2;
    vec4 tint;
} pc;

layout(location = 0) out vec4 outColor;

vec3 worldAt(vec2 uv) {
    float d = texture(depthTex, uv).r;
    vec4 p = frame.invViewProj * vec4(uv * 2.0 - 1.0, d, 1.0);
    return p.xyz / p.w;
}

void main() {
    vec2 size = vec2(textureSize(depthTex, 0));
    vec2 uv = gl_FragCoord.xy / size;
    if (texture(depthTex, uv).r >= 1.0) discard; // sky
    vec3 world = worldAt(uv);
    vec3 local = vec3(dot(pc.invBox0, vec4(world, 1.0)), dot(pc.invBox1, vec4(world, 1.0)), dot(pc.invBox2, vec4(world, 1.0)));
    if (any(greaterThan(abs(local), vec3(0.5)))) discard;
    // The surface normal from neighbouring depths, against the box's
    // projection axis (world y of the box).
    vec3 dx = worldAt(uv + vec2(1.0 / size.x, 0.0)) - world;
    vec3 dy = worldAt(uv + vec2(0.0, 1.0 / size.y)) - world;
    vec3 n = normalize(cross(dx, dy));
    vec3 axis = normalize(vec3(pc.invBox1.xyz));
    float facing = abs(dot(n, axis));
    float fade = smoothstep(0.2, 0.5, facing);
    vec2 duv = vec2(local.x + 0.5, 0.5 - local.z);
    outColor = texture(decalTex, duv) * pc.tint * fade;
}
