package gfx

import (
	"fmt"
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestSphereUploadPathsRendered covers the same geometry through setup
// uploads and uploads recorded inside a frame, at both sides of 64 KiB
// of vertex data. Unlit draws separate missing geometry from lighting.
func TestSphereUploadPathsRendered(t *testing.T) {
	for _, rings := range []int{16, 32} {
		for _, inFrame := range []bool{false, true} {
			for _, unlit := range []bool{false, true} {
				t.Run(fmt.Sprintf("rings=%d/in_frame=%t/unlit=%t", rings, inFrame, unlit), func(t *testing.T) {
					g := newHeadless(t, 96, 96)
					verts, indices := SphereMesh(rings, rings*2)
					var sphere *Mesh
					create := func() {
						var err error
						sphere, err = g.NewMesh(verts, indices)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(sphere.Destroy)
					}
					if !inFrame {
						create()
					}
					for frame := range 2 {
						img := renderMaterial(t, g, func() {
							if sphere == nil {
								create()
							}
							g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.05, 0.05, 0.05, 1}})
							g.DrawMesh(sphere, Material{BaseColor: RGB(180, 140, 60), Roughness: 0.8, Unlit: unlit}, lin.Scale(lin.V3(1.3, 1.3, 1.3)))
						})
						covered := 0
						for y := range 96 {
							for x := range 96 {
								if bright(img, x, y) {
									covered++
								}
							}
						}
						if covered < 1000 || !bright(img, 48, 48) {
							t.Errorf("frame %d: sphere covers %d pixels, centre %v, culled %d; vertices %d (%d bytes), indices %d; want a visible sphere", frame, covered, img.RGBAAt(48, 48), g.stats.Culled, len(verts), len(verts)*vertexSize, len(indices))
						}
					}
				})
			}
		}
	}
}

// litSphere draws one sphere under a bright light straight down the view
// and returns the frame, for the tests that compare a material against
// the plain one.
func litSphere(t *testing.T, g *Graphics, sphere *Mesh, m Material) *image.RGBA {
	t.Helper()
	return renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.05, 0.05, 0.05, 1}})
		g.DrawMesh(sphere, m, lin.Scale(lin.V3(1.3, 1.3, 1.3)))
	})
}

// newSphere uploads a sphere for a test and frees it afterwards.
func newSphere(t *testing.T, g *Graphics) *Mesh {
	t.Helper()
	sv, si := SphereMesh(32, 64)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sphere.Destroy)
	return sphere
}

// channels returns a pixel's red, green and blue as floats.
func rgbOf(img *image.RGBA, x, y int) (r, g, b float64) {
	c := img.RGBAAt(x, y)
	return float64(c.R), float64(c.G), float64(c.B)
}

// hueSpread is how far a pixel's colour is from grey, as the largest
// difference between two of its channels over their sum. A white
// highlight is near zero however bright it is.
func hueSpread(img *image.RGBA, x, y int) float64 {
	r, g, b := rgbOf(img, x, y)
	sum := r + g + b
	if sum < 24 {
		return 0
	}
	return (max(r, g, b) - min(r, g, b)) / sum
}

// TestIridescence checks that a thin film shifts the reflection's hue
// across the sphere, where the plain material stays grey.
func TestIridescence(t *testing.T) {
	g := newHeadless(t, 96, 96)
	sphere := newSphere(t, g)
	base := Material{BaseColor: RGB(200, 200, 200), Roughness: 0.25, Metallic: 1}
	film := base
	film.Iridescence = 1
	film.IridescenceThickness = 550
	plain, iridescent := litSphere(t, g, sphere, base), litSphere(t, g, sphere, film)
	// Points from the middle out towards the rim; the film's colour turns
	// with the angle, so somewhere along the way it must leave grey.
	worst, best := 0.0, 0.0
	for _, dx := range []int{0, 8, 16, 22, 28} {
		worst = max(worst, hueSpread(plain, 48+dx, 48))
		best = max(best, hueSpread(iridescent, 48+dx, 48))
	}
	if best < 0.15 || best < worst*2 {
		t.Errorf("iridescent sphere strays %.3f from grey, plain one %.3f; want a clear hue across the rim", best, worst)
	}
}

// TestAnisotropy checks that the highlight is stretched along the
// surface's tangent, which on a sphere runs around its equator.
func TestAnisotropy(t *testing.T) {
	g := newHeadless(t, 96, 96)
	sphere := newSphere(t, g)
	base := Material{BaseColor: RGB(15, 15, 15), Roughness: 0.4, Metallic: 1}
	stretched := base
	stretched.Anisotropy = 0.95
	round, along := litSphere(t, g, sphere, base), litSphere(t, g, sphere, stretched)
	spread := func(img *image.RGBA) float64 {
		var across, up float64
		for _, d := range []int{10, 14, 18} {
			r, _, _ := rgbOf(img, 48+d, 48)
			across += r
			r, _, _ = rgbOf(img, 48, 48+d)
			up += r
		}
		return across / max(up, 1)
	}
	if spread(along) <= spread(round)*1.1 {
		t.Errorf("anisotropic highlight spreads %.2f across against %.2f for the round one; want it stretched along the tangent",
			spread(along), spread(round))
	}
}

// TestSpecularColor checks that a specular tint colours a dielectric's
// reflection without touching its base colour.
func TestSpecularColor(t *testing.T) {
	g := newHeadless(t, 96, 96)
	sphere := newSphere(t, g)
	// A black dielectric: everything seen is the reflection.
	base := Material{BaseColor: Color{0, 0, 0, 1}, Roughness: 0.2}
	tinted := base
	tinted.SpecularColor = RGB(255, 40, 40)
	plain, red := litSphere(t, g, sphere, base), litSphere(t, g, sphere, tinted)
	pr, _, pb := rgbOf(plain, 48, 48)
	rr, _, rb := rgbOf(red, 48, 48)
	if pr < 20 {
		t.Fatalf("the plain sphere has no highlight to tint: %v", plain.RGBAAt(48, 48))
	}
	if rr/max(rb, 1) <= pr/max(pb, 1)*1.5 {
		t.Errorf("tinted reflection is %v against %v plain; want it redder", red.RGBAAt(48, 48), plain.RGBAAt(48, 48))
	}
	// A weak specular strength dims the reflection.
	dim := base
	dim.Specular = 0.02
	if d := litSphere(t, g, sphere, dim); d.RGBAAt(48, 48).R >= plain.RGBAAt(48, 48).R {
		t.Errorf("a specular strength of 0.02 left the highlight at %v, plain is %v", d.RGBAAt(48, 48), plain.RGBAAt(48, 48))
	}
}

// TestFurShells checks that shells stand off the surface, so a furry
// sphere covers more of the frame than the same sphere without them.
func TestFurShells(t *testing.T) {
	g := newHeadless(t, 96, 96)
	sphere := newSphere(t, g)
	base := Material{BaseColor: RGB(180, 140, 60), Roughness: 0.8}
	furry := base
	furry.Shells, furry.ShellLength = 12, 0.25
	covered := func(img *image.RGBA) int {
		n := 0
		for y := range 96 {
			for x := range 96 {
				if bright(img, x, y) {
					n++
				}
			}
		}
		return n
	}
	bare := covered(litSphere(t, g, sphere, base))
	fur := covered(litSphere(t, g, sphere, furry))
	if fur <= bare {
		t.Errorf("the furry sphere covers %d pixels and the bare one %d; want the shells to stand off the surface", fur, bare)
	}
	// The shells reach about ShellLength further out: at 1.3 across and
	// 3 units away the silhouette should grow by several pixels.
	if fur < bare+120 {
		t.Errorf("the fur only added %d pixels of silhouette", fur-bare)
	}
}
