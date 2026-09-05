package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestStencilMask marks a small quad in the stencil buffer and draws a
// large one only where the mark is, which is what a portal or a cutaway
// does. The masked quad is queued first, so this also covers the rule
// that a material writing the stencil buffer draws before one testing
// it.
func TestStencilMask(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	mask := Material{BaseColor: RGB(30, 30, 30), Unlit: true, StencilWrite: StencilReplace, StencilRef: 1}
	// The mask sits behind, so the masked quad in front passes the depth
	// test where the two overlap.
	maskAt := lin.Translate(lin.V3(0, 0, -1)).Mul(lin.Scale(lin.V3(0.35, 0.35, 1)))
	through := Material{BaseColor: RGB(0, 220, 0), Unlit: true, Stencil: StencilEqual, StencilRef: 1}
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, through, lin.Scale(lin.V3(1.5, 1.5, 1)))
		g.DrawMesh(quad, mask, maskAt)
	})
	if c := img.RGBAAt(32, 32); c.G < 150 {
		t.Errorf("the masked quad is %v in the middle, want the green showing through the mark", c)
	}
	if c := img.RGBAAt(4, 32); c.G > 60 {
		t.Errorf("the masked quad is %v away from the mark, want nothing drawn", c)
	}
	// The same pair without a stencil test: the big quad covers everything.
	plain := through
	plain.Stencil, plain.StencilRef = StencilAlways, 0
	img = renderMaterial(t, g, func() {
		g.DrawMesh(quad, plain, lin.Scale(lin.V3(1.5, 1.5, 1)))
		g.DrawMesh(quad, mask, maskAt)
	})
	if c := img.RGBAAt(4, 32); c.G < 150 {
		t.Errorf("without a stencil test the quad is %v at its edge, want green everywhere", c)
	}
	// StencilNotEqual is the other half of the mask.
	outside := through
	outside.Stencil = StencilNotEqual
	img = renderMaterial(t, g, func() {
		g.DrawMesh(quad, mask, maskAt)
		g.DrawMesh(quad, outside, lin.Scale(lin.V3(1.5, 1.5, 1)))
	})
	if c := img.RGBAAt(32, 32); c.G > 60 {
		t.Errorf("an inverted mask drew %v inside the mark, want nothing", c)
	}
	if c := img.RGBAAt(4, 32); c.G < 150 {
		t.Errorf("an inverted mask is %v outside the mark, want green", c)
	}
}
