#version 450

// Screen-space ambient occlusion from the scene depth: view positions are
// reconstructed with the inverse projection, normals from their
// derivatives, and a rotated hemisphere kernel tests nearby depth.
layout(set = 0, binding = 0) uniform sampler2D depthTex;
layout(push_constant) uniform PC {
    mat4 proj;
    mat4 invProj;
} pc;

layout(location = 0) in vec2 vUV;
layout(location = 0) out vec4 outColor;

// proj[3][3] carries the kernel radius (it is 0 in a perspective matrix);
// the projection used here has it restored.
vec3 viewPos(vec2 uv) {
    float d = texture(depthTex, uv).r;
    vec4 p = pc.invProj * vec4(uv * 2.0 - 1.0, d, 1.0);
    return p.xyz / p.w;
}

float hash(vec2 p) { return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453); }

void main() {
    float d0 = texture(depthTex, vUV).r;
    if (d0 >= 1.0) { outColor = vec4(1.0); return; }
    vec3 p = viewPos(vUV);
    vec3 n = normalize(cross(dFdx(p), dFdy(p)));
    if (dot(n, -p) < 0.0) n = -n; // face the camera regardless of winding
    float radius = pc.proj[3][3];
    mat4 proj = pc.proj;
    proj[3][3] = 0.0;
    float angle = hash(gl_FragCoord.xy) * 6.2831853;
    float occlusion = 0.0;
    const int N = 16;
    vec3 up = abs(n.z) < 0.999 ? vec3(0, 0, 1) : vec3(1, 0, 0);
    vec3 t = normalize(cross(up, n));
    vec3 b = cross(n, t);
    for (int i = 0; i < N; i++) {
        // Spiral of directions on the hemisphere around n, with sample
        // distances clustered towards the point so nearby creases count.
        float a = angle + float(i) * 2.399963;
        float r = sqrt((float(i) + 0.5) / float(N));
        vec3 dir = vec3(cos(a) * r, sin(a) * r, sqrt(1.0 - r * r));
        float k = float(i + 1) / float(N);
        float dist = mix(0.1, 1.0, k * k) * radius;
        vec3 s = p + (t * dir.x + b * dir.y + n * dir.z) * dist;
        vec4 c = proj * vec4(s, 1.0);
        vec2 uv = (c.xy / c.w) * 0.5 + 0.5;
        if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) continue;
        float sceneZ = viewPos(uv).z;
        float rangeCheck = smoothstep(0.0, 1.0, radius / abs(p.z - sceneZ));
        occlusion += (sceneZ >= s.z + 0.02 ? 1.0 : 0.0) * rangeCheck;
    }
    outColor = vec4(vec3(1.0 - occlusion / float(N)), 1.0);
}
