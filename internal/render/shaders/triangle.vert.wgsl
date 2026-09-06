struct Output { @builtin(position) position: vec4f, @location(0) color: vec3f, };
@vertex fn main(@builtin(vertex_index) index: u32) -> Output {
    let positions = array<vec2f,3>(vec2f(0.0,-0.6),vec2f(0.6,0.6),vec2f(-0.6,0.6));
    let colors = array<vec3f,3>(vec3f(1.0,0.0,0.0),vec3f(0.0,1.0,0.0),vec3f(0.0,0.0,1.0));
    return Output(vec4f(positions[index],0.0,1.0),colors[index]);
}
