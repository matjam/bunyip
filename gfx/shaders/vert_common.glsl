#version 450

// Vertex prelude for mesh shaders. bunyip-shader puts this (plus the
// skinning piece for skinned meshes) before the game's code and a main
// after it. The game may define
//
//     void vertex(inout VertexData v)
//
// to move vertices in object space before the model matrix: wind,
// waves, displacement, morphing. The same hook runs in the shadow pass
// so displaced geometry casts the right shadow.
layout(location = 0) in vec3 iPos;
layout(location = 1) in vec3 iNormal;
layout(location = 2) in vec2 iUV;
layout(location = 3) in vec2 iUV2;
layout(location = 4) in vec4 iColor;

layout(set = 1, binding = 0) uniform Frame {
    mat4 viewProj;
    mat4 view;
    mat4 lightViewProj[3]; // shadow cascades
    vec4 camPos;
    vec4 lightDir;     // direction the light travels
    vec4 lightColor;   // rgb, w = shadow strength
    vec4 sky;          // rgb the procedural sky's zenith
    vec4 ground;       // rgb light from below
    vec4 params;       // x = shadow map size, y = shadows enabled, z = point light count, w = time
    vec4 splits;       // view-space distances where cascades end
    vec4 radii;        // half-size of each cascade's orthographic box
    vec4 pointPos[32];   // xyz, w = range
    vec4 pointColor[32]; // rgb, w = cos of a spot light's inner cone (2 for a point light)
    vec4 spotDir[32];    // xyz a spot light's direction, w = cos of its outer cone (-2 for a point light)
    vec4 sh[9];        // environment irradiance as spherical harmonics
    vec4 env;          // x intensity, y mip count, z = 1 image environment, 2 procedural sky
    mat4 invViewProj;
    vec4 horizon;      // rgb the sky at the horizon, w = air (1 - vacuum)
    vec4 skyUp;        // xyz up, w = stars
    vec4 sun;          // xyz towards the sun, w = angular radius
    vec4 sunColor;     // rgb the drawn disc's radiance
    vec4 fog;          // rgb the fog colour, w = exponential density
    vec4 fogRange;     // x start, y end of linear fog; z height, w falloff of ground fog
    mat4 spotViewProj[4];  // shadowed spot lights' projections
    vec4 spotInfo[32];     // x = a light's spot map index or -1, y = range, z = its cube map slot or -1
    mat4 pointViewProj[24]; // four shadowed point lights, six faces each
} frame;

// Per-instance stream: the model matrix's rows, base colour, material
// parameters, texture transform, clearcoat and sheen.
layout(location = 5) in vec4 iModel0;
layout(location = 6) in vec4 iModel1;
layout(location = 7) in vec4 iModel2;
layout(location = 8) in vec4 iBaseColor;
layout(location = 9) in vec4 iMaterial; // x metallic, y roughness, z emissive strength, w flags
layout(location = 10) in vec4 iExtra;   // x joint base, y alpha cutoff, z occlusion strength, w subsurface
layout(location = 11) in vec4 iUVT0;    // texture transform a, b, c, d
layout(location = 12) in vec4 iUVT1;    // texture transform e, f; z clearcoat, w clearcoat roughness
layout(location = 13) in vec4 iSheen;   // sheen colour, w sheen roughness
layout(location = 14) in vec4 iVolume;  // x transmission, y ior, z thickness, w attenuation distance
layout(location = 15) in vec4 iAtten;   // attenuation colour, w = packed sampler indices

// The material's textures and the shader's images are visible here too,
// for displacement maps. Set 0 keeps images and samplers apart, and the
// names are macros pairing each image with the sampler its texture asked
// for; see prelude_mesh.glsl.
layout(set = 0, binding = 0) uniform texture2D tAlbedo;
layout(set = 0, binding = 1) uniform texture2D tMetalRough;
layout(set = 0, binding = 2) uniform texture2D tNormal;
layout(set = 0, binding = 3) uniform texture2D tEmissive;
layout(set = 0, binding = 4) uniform texture2D tOcclusion;
layout(set = 0, binding = 5) uniform texture2D tImage0;
layout(set = 0, binding = 6) uniform texture2D tImage1;
layout(set = 0, binding = 7) uniform texture2D tImage2;
layout(set = 0, binding = 8) uniform texture2D tImage3;
layout(set = 0, binding = 9) uniform textureCube tEnv;
layout(set = 0, binding = 10) uniform texture2D tThickness;
layout(set = 0, binding = 11) uniform texture2D tScene;
layout(set = 0, binding = 12) uniform texture2D tTransmission;
layout(set = 0, binding = 13) uniform sampler samplers[4];

const int LINEAR_CLAMP = 1;

// texSampler is the sampler for one of the material set's texture slots,
// two bits apiece in this instance's packed index.
int texSampler(int slot) { return (int(iAtten.w) >> (2 * slot)) & 3; }

#define albedoTex sampler2D(tAlbedo, samplers[texSampler(0)])
#define metalRoughTex sampler2D(tMetalRough, samplers[texSampler(1)])
#define normalTex sampler2D(tNormal, samplers[texSampler(2)])
#define emissiveTex sampler2D(tEmissive, samplers[texSampler(3)])
#define occlusionTex sampler2D(tOcclusion, samplers[texSampler(4)])
#define image0 sampler2D(tImage0, samplers[texSampler(5)])
#define image1 sampler2D(tImage1, samplers[texSampler(6)])
#define image2 sampler2D(tImage2, samplers[texSampler(7)])
#define image3 sampler2D(tImage3, samplers[texSampler(8)])
#define thicknessTex sampler2D(tThickness, samplers[texSampler(9)])
#define transmissionTex sampler2D(tTransmission, samplers[texSampler(10)])
#define envMap samplerCube(tEnv, samplers[LINEAR_CLAMP])
#define sceneTex sampler2D(tScene, samplers[LINEAR_CLAMP])

#define UNIFORMS layout(set = 4, binding = 0)

// VertexData is one vertex in object space, after skinning.
struct VertexData {
    vec3 position;
    vec3 normal;
    vec2 uv;
    vec2 uv2;
    vec4 color;
};

// Surface is declared so a shader's fragment code compiles in this
// stage; it is only filled in by the fragment stage.
struct Surface {
    vec3 albedo;
    float alpha;
    vec3 normal;
    float metallic;
    float roughness;
    vec3 emissive;
    float occlusion;
    bool unlit;
    vec2 uv;
    vec2 uv2;
    vec4 color;
    vec3 worldPos;
    vec3 viewDir;
    float clearcoat;
    float clearcoatRoughness;
    vec3 sheen;
    float sheenRoughness;
    float subsurface;
    float thickness;
    float transmission;
    float ior;
    float volume;
    vec3 attenuation;
    float attenuationDistance;
};

// time is seconds since the game started.
float time() { return frame.params.w; }
// model is this instance's model matrix.
mat4 model() {
    return mat4(vec4(iModel0.x, iModel1.x, iModel2.x, 0.0), vec4(iModel0.y, iModel1.y, iModel2.y, 0.0),
                vec4(iModel0.z, iModel1.z, iModel2.z, 0.0), vec4(iModel0.w, iModel1.w, iModel2.w, 1.0));
}
// uvTransform maps a texture coordinate through the material's transform.
vec2 uvTransform(vec2 uv) {
    return vec2(iUVT0.x * uv.x + iUVT0.y * uv.y + iUVT0.z, iUVT0.w * uv.x + iUVT1.x * uv.y + iUVT1.y);
}

// Fragment-stage helpers, stubbed so shader code that calls them still
// compiles here; they are never called from the vertex stage.
vec3 perturbNormal(vec3 n, vec3 pos, vec2 uv) { return n; }
float shadowFactor(vec3 n, vec3 l) { return 1.0; }
vec3 shade(vec3 n, vec3 v, vec3 l, vec3 radiance, vec3 albedo, float metallic, float roughness) { return vec3(0.0); }
vec3 light(Surface s) { return vec3(0.0); }

void vertex(inout VertexData v);
