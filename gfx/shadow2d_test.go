package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// The polar map's angular scan must give what a ray cast along every
// direction would, which is the definition it is an optimisation of.
func TestShadowDistancesMatchBruteForce(t *testing.T) {
	const radius = 120
	light := lin.V2(50, 60)
	cases := []struct {
		name   string
		points []lin.Vec2
		runs   []int32
	}{
		{"a box beside the light", []lin.Vec2{{X: 80, Y: 20}, {X: 110, Y: 20}, {X: 110, Y: 90}, {X: 80, Y: 90}}, []int32{4}},
		{"a wall through the light's row", []lin.Vec2{{X: 20, Y: 60}, {X: 20, Y: 61}}, []int32{2}},
		{"a box around the light", []lin.Vec2{{X: 10, Y: 10}, {X: 90, Y: 10}, {X: 90, Y: 110}, {X: 10, Y: 110}}, []int32{4}},
		{"two occluders", []lin.Vec2{
			{X: 70, Y: 40}, {X: 75, Y: 80},
			{X: 10, Y: 30}, {X: 30, Y: 30}, {X: 30, Y: 45},
		}, []int32{2, 3}},
		{"far away", []lin.Vec2{{X: 900, Y: 900}, {X: 950, Y: 950}}, []int32{2}},
		{"a degenerate edge", []lin.Vec2{{X: 60, Y: 60}, {X: 60, Y: 60}}, []int32{2}},
	}
	got := make([]float32, shadowAngles2D)
	want := make([]float32, shadowAngles2D)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shadowDistances(got, light, radius, c.points, c.runs)
			bruteShadow(want, light, radius, c.points, c.runs)
			for i := range want {
				if math.Abs(float64(got[i]-want[i])) > 1e-3 {
					t.Fatalf("direction %d: %v, want %v", i, got[i], want[i])
				}
			}
		})
	}
}

// bruteShadow is the reference: every direction against every edge.
func bruteShadow(dist []float32, light lin.Vec2, radius float32, points []lin.Vec2, runs []int32) {
	for i := range dist {
		dist[i] = radius
		angle := 2 * math.Pi * ((float64(i)+0.5)/shadowAngles2D - 0.5)
		d := lin.V2(float32(math.Cos(angle)), float32(math.Sin(angle)))
		at := 0
		for _, n := range runs {
			poly := points[at : at+int(n)]
			at += int(n)
			edges := len(poly)
			if edges == 2 {
				edges = 1
			}
			for e := range edges {
				if t, ok := raySegment(light, d, poly[e], poly[(e+1)%len(poly)]); ok && t < dist[i] {
					dist[i] = t
				}
			}
		}
	}
}

// A lit sprite behind an occluder is darker than the same distance from
// the light in the open, and only when the light casts shadows.
func TestLitShadows(t *testing.T) {
	g := newHeadless(t, 96, 96)
	flat := image.NewRGBA(image.Rect(0, 0, 1, 1))
	flat.SetRGBA(0, 0, color.RGBA{R: 128, G: 128, B: 255, A: 255}) // straight out of the screen
	normal, err := g.NewTexture(flat, TextureOptions{Data: true})
	if err != nil {
		t.Fatal(err)
	}
	defer normal.Destroy()
	// A light in the middle of a lit quad, with a wall to its right.
	scene := func(shadows, occlude bool) *image.RGBA {
		return frame2D(t, g, func() {
			g.SetLights2D(Color{0.02, 0.02, 0.02, 1},
				Light2D{Pos: lin.V2(48, 48), Height: 16, Radius: 90, Shadows: shadows, Softness: 1})
			if occlude {
				g.AddOccluder2D(lin.V2(60, 36), lin.V2(60, 60))
			}
			g.DrawLit(nil, normal, Sprite{Size: lin.V2(96, 96)})
		})
	}
	open := scene(true, false)
	shadowed := scene(true, true)
	unlit := scene(false, true)
	// (80,48) sits behind the wall; (16,48) is the same distance from the
	// light on the other side, with nothing in the way.
	behind, beside := shadowed.RGBAAt(80, 48), shadowed.RGBAAt(16, 48)
	if int(behind.R)+20 > int(beside.R) {
		t.Errorf("behind the occluder %v, in the open at the same distance %v", behind, beside)
	}
	if int(behind.R)+20 > int(open.RGBAAt(80, 48).R) {
		t.Errorf("the same pixel is %v with the occluder and %v without it", behind, open.RGBAAt(80, 48))
	}
	// The occluder only shadows what is behind it.
	if a, b := shadowed.RGBAAt(16, 48), open.RGBAAt(16, 48); absDiff(a.R, b.R) > 2 {
		t.Errorf("a pixel the occluder cannot reach changed from %v to %v", b, a)
	}
	// A light without Shadows ignores occluders.
	if a, b := unlit.RGBAAt(80, 48), open.RGBAAt(80, 48); absDiff(a.R, b.R) > 2 {
		t.Errorf("an unshadowed light was blocked: %v against %v", a, b)
	}
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
