package particle

import (
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

func TestGPUPaletteRetunePreservesBirthTint(t *testing.T) {
	red, green, blue := gfx.RGB(255, 0, 0), gfx.RGB(0, 255, 0), gfx.RGB(0, 0, 255)
	for _, tc := range []struct {
		name          string
		before, after []gfx.Color
	}{
		{"remove", []gfx.Color{red, green, blue}, nil},
		{"shrink", []gfx.Color{red, green, blue}, []gfx.Color{green}},
		{"replace", []gfx.Color{red, green, blue}, []gfx.Color{blue, red, green}},
		{"add", nil, []gfx.Color{blue}},
	} {
		for _, grow := range []bool{false, true} {
			name := tc.name
			if grow {
				name += "/grow"
			}
			t.Run(name, func(t *testing.T) {
				e := Emitter{Burst: 12, Max: 16, Lifetime: Range{Min: 1}, Palette: tc.before, Seed: 7}
				cpu, gpu := New(e), NewGPU(e)
				cpu.Update(0.25)
				gpu.Update(0.25)
				e.Palette = tc.after
				e.Color = gfx.Color{R: 0.5, G: 0.25, B: 0.75, A: 0.5}
				e.Lifetime = Range{Min: 2}
				if grow {
					e.Max = 32
				}
				cpu.SetEmitter(e)
				gpu.SetEmitter(e)
				checkGPUPaletteColors(t, gpu, cpu)
				cpu.Burst(3)
				gpu.Burst(3)
				checkGPUPaletteColors(t, gpu, cpu)
				// The original particles die, moving the new births down
				// through the compacted arrays with their assigned tints.
				cpu.Update(0.8)
				gpu.Update(0.8)
				if gpu.Alive() != 3 {
					t.Fatalf("after original particles die: alive = %d, want 3", gpu.Alive())
				}
				checkGPUPaletteColors(t, gpu, cpu)
			})
		}
	}
}

func TestGPUPaletteEditPreservesBirthTint(t *testing.T) {
	red, blue := gfx.RGB(255, 0, 0), gfx.RGB(0, 0, 255)
	e := Emitter{Burst: 1, Max: 2, Palette: []gfx.Color{red}}
	cpu, gpu := New(e), NewGPU(e)
	// An editor can mutate the palette returned by Emitter before
	// calling SetEmitter. A birth must already own its chosen tint.
	e = gpu.Emitter()
	e.Palette[0] = blue
	cpu.SetEmitter(e)
	gpu.SetEmitter(e)
	cpu.Burst(1)
	gpu.Burst(1)
	checkGPUPaletteColors(t, gpu, cpu)
	if got := gpu.Quads()[0].Color; got != red {
		t.Errorf("existing particle tint = %v, want red %v", got, red)
	}
	if got := gpu.Quads()[1].Color; got != blue {
		t.Errorf("new particle tint = %v, want blue %v", got, blue)
	}
}

func TestGPUPaletteSteadyFrameDoesNotAllocate(t *testing.T) {
	s := NewGPU(Emitter{Burst: 16, Max: 16, Lifetime: Range{Min: 100}, Palette: []gfx.Color{gfx.White}})
	e := s.Emitter()
	e.Palette = nil
	s.SetEmitter(e)
	s.buildQuads(lin.Vec2{})
	allocs := testing.AllocsPerRun(100, func() {
		s.Update(1.0 / 60)
		s.buildQuads(lin.Vec2{})
	})
	if allocs != 0 {
		t.Errorf("steady Update and quad build allocated %v times, want 0", allocs)
	}
	if s.Alive() != 16 {
		t.Fatalf("allocation check ended with %d particles, want 16", s.Alive())
	}
}

func checkGPUPaletteColors(t *testing.T, gpu *GPUSystem, cpu *System) {
	t.Helper()
	gpu.buildQuads(lin.Vec2{})
	quads, particles := gpu.Quads(), cpu.Particles()
	if len(quads) != len(particles) {
		t.Fatalf("built %d quads, want %d", len(quads), len(particles))
	}
	e := cpu.Emitter()
	for i, p := range particles {
		want := e.colorAt(p.Age / p.Life).Mul(p.Tint)
		if got := quads[i].Color; got != want {
			t.Errorf("particle %d color = %v, want CPU birth tint with current color %v", i, got, want)
		}
	}
}
