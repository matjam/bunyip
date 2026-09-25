package gfx

import (
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// BenchmarkAtmosphereWall measures whole frames of gpuScene with the
// atmospheric sky off and on, at 1280x720 and 2560x1440. Frames in
// flight are bounded, so the loop runs at the GPU's pace and a case's
// difference against "off" is what the atmosphere costs the GPU. The
// "sunMoving" case turns the sun a little every frame, so everything
// that depends on the sun is rebuilt every frame. Compare runs with
// benchstat.
func BenchmarkAtmosphereWall(b *testing.B) {
	cases := []struct {
		name string
		set  func(l *Light, i int)
	}{
		{"off", func(l *Light, i int) {}},
		{"atmosphere", func(l *Light, i int) { l.Sky.Atmosphere = Atmosphere{Height: 100} }},
		{"atmosphereFog", func(l *Light, i int) {
			l.Sky.Atmosphere = Atmosphere{Height: 100}
			l.Fog = Fog{Color: Color{0.6, 0.7, 0.8, 1}, Start: 20, End: 200}
		}},
		{"sunMoving", func(l *Light, i int) {
			l.Sky.Atmosphere = Atmosphere{Height: 100}
			a := 0.4 + 0.001*float64(i%400)
			l.Direction = lin.V3(-0.3, -float32(math.Sin(a)), -float32(math.Cos(a)))
		}},
	}
	for _, res := range [][2]int{{1280, 720}, {2560, 1440}} {
		b.Run(fmt.Sprintf("%dx%d", res[0], res[1]), func(b *testing.B) {
			g := benchHeadless(b, res[0], res[1])
			sc := newGPUScene(b, g)
			g.SetPost(gpuPostBase())
			for _, c := range cases {
				b.Run(c.name, func(b *testing.B) {
					frame := func(i int) {
						ok, err := g.begin(Black)
						if err != nil {
							b.Fatal(err)
						}
						if !ok {
							return
						}
						sc.draw(g, i)
						l := g.cur.light
						c.set(&l, i)
						g.SetLight(l)
						if _, err := g.end(false); err != nil {
							b.Fatal(err)
						}
					}
					// Warm up: pipelines, targets and anything built on first use.
					for i := range 8 {
						frame(i)
					}
					for i := 0; b.Loop(); i++ {
						frame(i)
					}
				})
			}
		})
	}
}
