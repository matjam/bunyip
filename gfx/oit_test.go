package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// crossedQuads draws two translucent quads turned sixty degrees either
// way about the up axis, so they cross in the middle of the frame: the
// red one is nearer on the left of the crossing and the blue one is
// nearer on the right. Sorting can only pick one order for both halves.
func crossedQuads(t *testing.T, g *Graphics, quad *Mesh, independent bool) *image.RGBA {
	t.Helper()
	return renderMaterial(t, g, func() {
		p := g.Post()
		p.OrderIndependent = independent
		g.SetPost(p)
		g.SetCamera(Camera{Position: lin.V3(0, 0, 2.2), Target: lin.V3(0, 0, 0)})
		red := Material{BaseColor: Color{1, 0, 0, 0.5}, Blend: true, Unlit: true}
		blue := Material{BaseColor: Color{0, 0, 1, 0.5}, Blend: true, Unlit: true}
		g.DrawMesh(quad, red, lin.Rotate(lin.Radians(60), lin.V3(0, 1, 0)))
		g.DrawMesh(quad, blue, lin.Rotate(lin.Radians(-60), lin.V3(0, 1, 0)))
	})
}

// TestOrderIndependentTransparency checks what the pass exists for: two
// translucent meshes that cross show the nearer one on each side of the
// crossing, which one order for the whole draw cannot do.
func TestOrderIndependentTransparency(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	// The quads reach x = 0.5 either way and cross at the middle, so half
	// way out from the middle both cover the pixel and one is in front.
	lean := func(img *image.RGBA, x int) int {
		p := img.RGBAAt(x, 32)
		return int(p.R) - int(p.B)
	}
	const left, right = 26, 38
	sorted := crossedQuads(t, g, quad, false)
	independent := crossedQuads(t, g, quad, true)
	sl, sr := lean(sorted, left), lean(sorted, right)
	il, ir := lean(independent, left), lean(independent, right)
	if (sl > 0) != (sr > 0) {
		t.Errorf("sorted: left leans %d and right %d, want one order over the whole crossing", sl, sr)
	}
	if il <= 20 {
		t.Errorf("order-independent: the left of the crossing leans %d, want the near red quad by a clear margin", il)
	}
	if ir >= -20 {
		t.Errorf("order-independent: the right of the crossing leans %d, want the near blue quad by a clear margin", ir)
	}
	// Both quads are still there: neither side is one colour alone.
	for _, p := range []image.Point{{X: left, Y: 32}, {X: right, Y: 32}} {
		c := independent.RGBAAt(p.X, p.Y)
		if c.R < 30 || c.B < 30 {
			t.Errorf("order-independent: %v is %v, want both quads in the mix", p, c)
		}
	}
}

// TestOrderIndependentMatchesOneLayer checks the algebra of the
// accumulation and its resolve: where only one translucent surface
// covers a pixel the two paths must agree, because there is no order to
// disagree about.
func TestOrderIndependentMatchesOneLayer(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	shot := func(independent bool) *image.RGBA {
		return renderMaterial(t, g, func() {
			p := g.Post()
			p.OrderIndependent = independent
			g.SetPost(p)
			g.DrawMesh(quad, Material{BaseColor: Color{0.8, 0.3, 0.1, 0.4}, Blend: true, Unlit: true}, lin.Scale(lin.V3(0.6, 0.6, 1)))
		})
	}
	sorted, independent := shot(false), shot(true)
	for _, p := range []image.Point{{X: 32, Y: 32}, {X: 24, Y: 40}, {X: 40, Y: 24}} {
		a, b := sorted.RGBAAt(p.X, p.Y), independent.RGBAAt(p.X, p.Y)
		if a.R == 0 && a.G == 0 && a.B == 0 {
			t.Fatalf("the sorted quad is missing at %v", p)
		}
		for _, c := range []struct {
			name string
			a, b uint8
		}{{"red", a.R, b.R}, {"green", a.G, b.G}, {"blue", a.B, b.B}} {
			if d := int(c.a) - int(c.b); d > 3 || d < -3 {
				t.Errorf("at %v the %s is %d sorted and %d order-independent", p, c.name, c.a, c.b)
			}
		}
	}
}
