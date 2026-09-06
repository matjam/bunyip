#version 450

// Independently compiled std140 fixture. Keep scalar-array, nested-struct,
// vec3+scalar and matrix-array members: they distinguish Go layout from GLSL.
struct Pair { vec2 xy; float z; };
layout(set = 1, binding = 0, std140) uniform Parameters {
    vec3 vector;
    float scalar;
    float values[2];
    Pair pairs[2];
    mat3 matrices[2];
    bool enabled;
    int signedValue;
    uint unsignedValue;
    vec4 colour;
    mat4 transform;
} u;
layout(location = 0) out vec4 outColor;
void main() {
    bool good = u.vector == vec3(1,2,3) && u.scalar == 4
        && u.values[0] == 5 && u.values[1] == 6
        && u.pairs[0].xy == vec2(7,8) && u.pairs[0].z == 9
        && u.pairs[1].xy == vec2(10,11) && u.pairs[1].z == 12
        && u.enabled && u.signedValue == -13 && u.unsignedValue == 14u
        && u.colour == vec4(0.25, 0.5, 0.75, 1);
    for (int m = 0; m < 2; ++m)
        for (int c = 0; c < 3; ++c)
            for (int r = 0; r < 3; ++r)
                good = good && u.matrices[m][c][r] == float(m*9+c*3+r+20);
    for (int c = 0; c < 4; ++c)
        for (int r = 0; r < 4; ++r)
            good = good && u.transform[c][r] == float(c*4+r+40);
    outColor = good ? vec4(0,1,0,1) : vec4(1,0,0,1);
}
