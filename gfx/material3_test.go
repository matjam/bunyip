package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestOutlineAndXRay(t *testing.T) {
	g := newHeadless(t, 96, 96)
	sv, si := SphereMesh(16, 32)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	quad := facingQuad(t, g)
	// A small sphere with a fat red outline: the ring just outside the
	// sphere's silhouette is red, the sphere itself is not.
	img := renderMaterial(t, g, func() {
		g.DrawMesh(sphere, Material{BaseColor: RGB(255, 255, 255), Outline: 6, OutlineColor: RGB(255, 0, 0)}, lin.Scale(lin.V3(0.6, 0.6, 0.6)))
	})
	// The sphere of radius 0.6 at distance 3 covers about 17 pixels; its
	// edge sits near x = 48 ± 17.
	ring, inside := img.RGBAAt(48+20, 48), img.RGBAAt(48, 48)
	if ring.R < 150 || ring.G > 80 {
		t.Errorf("outline ring is %v, want red", ring)
	}
	if inside.R < 150 || inside.G < 150 {
		t.Errorf("sphere interior is %v, want the lit white sphere, not the outline", inside)
	}
	// X-ray: a sphere behind a wall shows its tint through the wall.
	img = renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(40, 40, 40)}, lin.Translate(lin.V3(0, 0, 1)).Mul(lin.Scale(lin.V3(2, 2, 1))))
		g.DrawMesh(sphere, Material{BaseColor: RGB(255, 255, 255), XRay: RGBA(0, 255, 0, 180)}, lin.Scale(lin.V3(0.6, 0.6, 0.6)))
	})
	centre, wall := img.RGBAAt(48, 48), img.RGBAAt(8, 8)
	if centre.G < 120 || centre.G < centre.R+40 {
		t.Errorf("hidden sphere should show a green x-ray tint through the wall: %v", centre)
	}
	if wall.G > 80 {
		t.Errorf("the wall away from the sphere is tinted: %v", wall)
	}
}

func TestDecal(t *testing.T) {
	g := newHeadless(t, 96, 96)
	quad := facingQuad(t, g)
	// A red decal square projected onto a white wall facing the camera:
	// the box's y axis must point along the view (z), so rotate it.
	tex := image.NewRGBA(image.Rect(0, 0, 1, 1))
	tex.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	red, err := g.NewTexture(tex, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer red.Destroy()
	box := lin.Rotate(lin.Radians(90), lin.V3(1, 0, 0)).Mul(lin.Scale(lin.V3(0.6, 1, 0.6)))
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{Unlit: true}, lin.Scale(lin.V3(2, 2, 1)))
		g.DrawDecal(red, box, White)
	})
	centre, edge := img.RGBAAt(48, 48), img.RGBAAt(8, 48)
	if centre.R < 150 || centre.G > 80 {
		t.Errorf("decal centre is %v, want red on the wall", centre)
	}
	if edge.G < 150 {
		t.Errorf("wall outside the decal box is %v, want white", edge)
	}
}

func TestEmissiveWithoutTexture(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	// A dark red quad that glows: brighter, and still red, without an
	// emissive texture.
	plain := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(120, 0, 0), Roughness: 0.8}, lin.Identity())
	}).RGBAAt(32, 32)
	glow := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(120, 0, 0), Roughness: 0.8, Emissive: 2}, lin.Identity())
	}).RGBAAt(32, 32)
	if glow.R < plain.R+40 || glow.B > 60 {
		t.Errorf("emissive quad is %v against %v plain, want a brighter red", glow, plain)
	}
}

func TestTransmission(t *testing.T) {
	g := newHeadless(t, 96, 96)
	quad := facingQuad(t, g)
	// A red wall above the middle and a blue one below, behind a pane of
	// glass with an index of 1 so the view passes straight through: the
	// pane shows red in its top half and blue in its bottom half, which
	// also proves the scene copy is sampled the right way up.
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(255, 0, 0), Unlit: true}, lin.Translate(lin.V3(0, 1, -1)))
		g.DrawMesh(quad, Material{BaseColor: RGB(0, 0, 255), Unlit: true}, lin.Translate(lin.V3(0, -1, -1)))
		g.DrawMesh(quad, Material{Transmission: 1, IOR: 1, Roughness: 0.5}, lin.Scale(lin.V3(0.5, 0.5, 1)))
	})
	above, below := img.RGBAAt(48, 38), img.RGBAAt(48, 58)
	if above.R < 150 || above.B > 80 {
		t.Errorf("glass over the red wall is %v, want red", above)
	}
	if below.B < 150 || below.R > 80 {
		t.Errorf("glass over the blue wall is %v, want blue", below)
	}
	// Opaque glass (no transmission) is lit grey, not the wall's colour.
	img = renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(255, 0, 0), Unlit: true}, lin.Translate(lin.V3(0, 1, -1)))
		g.DrawMesh(quad, Material{Roughness: 0.5}, lin.Scale(lin.V3(0.5, 0.5, 1)))
	})
	if c := img.RGBAAt(48, 38); c.R > c.B+40 {
		t.Errorf("an opaque pane picked up the wall's colour: %v", c)
	}
}
