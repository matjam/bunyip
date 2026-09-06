package gfx

import (
	"context"
	"testing"
)

func TestShaderPrivateZeroInitialization(t *testing.T) {
	g := newHeadless(t, 16, 16)
	s, err := g.CompileShader(context.Background(), `
struct State { flag: bool, count: i32, weights: array<vec4f,2>, matrix: mat3x3f, }
var<private> state: State;
var<private> total: f32;
fn fragment(uv: vec2f, color: vec4f) -> vec4f {
    total += 1.0;
    state.count += 1;
    let good = total == 1.0 && state.count == 1 && !state.flag
        && state.weights[0].x == 0.0 && state.weights[1].w == 0.0 && state.matrix[2][1] == 0.0;
    state.flag = true;
    state.weights[1].w = 9.0;
    state.matrix[2][1] = 8.0;
    return select(vec4f(1,0,0,1), vec4f(0,1,0,1), good);
}`)
	if err != nil {
		t.Fatal(err)
	}
	// Every fragment of every invocation starts from zero, regardless of
	// writes by other fragments or earlier frames.
	for frame := range 3 {
		img := frame2D(t, g, func() { g.Shaded(s, func() { g.FillRect(0, 0, 16, 16, White) }) })
		for y := range 16 {
			for x := range 16 {
				if p := img.RGBAAt(x, y); p.R != 0 || p.G != 255 || p.B != 0 || p.A != 255 {
					t.Fatalf("frame %d pixel %d,%d: %v", frame, x, y, p)
				}
			}
		}
	}
}
