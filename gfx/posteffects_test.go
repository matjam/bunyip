package gfx

import (
	"image"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/lin"
)

// TestVelocityLayoutOffsets pins the velocity programs' vertex input to
// the instance stream's actual field offsets. The layout is written as
// byte offsets, so a field added to meshInstance moves them silently and
// the motion vectors come out as noise rather than as an error.
func TestVelocityLayoutOffsets(t *testing.T) {
	var in meshInstance
	if got := unsafe.Sizeof(in); got != meshInstanceSize {
		t.Fatalf("meshInstance is %d bytes, meshInstanceSize says %d", got, meshInstanceSize)
	}
	want := map[uint32]uintptr{
		1: unsafe.Offsetof(in.model),
		2: unsafe.Offsetof(in.model) + 16,
		3: unsafe.Offsetof(in.model) + 32,
		4: unsafe.Offsetof(in.prevModel),
		5: unsafe.Offsetof(in.prevModel) + 16,
		6: unsafe.Offsetof(in.prevModel) + 32,
		7: unsafe.Offsetof(in.extra),
	}
	bindings, attrs := velocityVertexLayout(true)
	if bindings[1].Stride != meshInstanceSize {
		t.Errorf("the instance stride is %d, want %d", bindings[1].Stride, meshInstanceSize)
	}
	for _, a := range attrs {
		if a.Binding != 1 {
			continue
		}
		w, ok := want[a.Location]
		if !ok {
			t.Errorf("location %d reads the instance stream but is not pinned here", a.Location)
			continue
		}
		if uintptr(a.Offset) != w {
			t.Errorf("location %d reads offset %d; the field is at %d", a.Location, a.Offset, w)
		}
		delete(want, a.Location)
	}
	for loc := range want {
		t.Errorf("location %d is pinned here but the layout does not declare it", loc)
	}
	// The skinned variant's own vertex attributes, against the packed vertex.
	var sv gpuSkinVertex
	for _, c := range []struct {
		loc  uint32
		want uintptr
	}{{8, unsafe.Offsetof(sv.joints)}, {9, unsafe.Offsetof(sv.weights)}} {
		found := false
		for _, a := range attrs {
			if a.Location == c.loc && a.Binding == 0 {
				found = true
				if uintptr(a.Offset) != c.want {
					t.Errorf("location %d reads offset %d; the field is at %d", c.loc, a.Offset, c.want)
				}
			}
		}
		if !found {
			t.Errorf("the skinned layout does not declare location %d", c.loc)
		}
	}
	if bindings[0].Stride != skinVertexSize {
		t.Errorf("the skinned vertex stride is %d, want %d", bindings[0].Stride, skinVertexSize)
	}
}

// lum is the 8-bit luminance of a pixel, for the post-processing tests.
func lum(img *image.RGBA, x, y int) int {
	c := img.RGBAAt(x, y)
	return (int(c.R)*54 + int(c.G)*183 + int(c.B)*19) / 256
}

// postShot renders frames of one scene and returns the last one. Several
// frames matter to temporal anti-aliasing, which needs a history.
func postShot(t *testing.T, g *Graphics, p PostSettings, frames int, draw func(frame int)) *image.RGBA {
	t.Helper()
	g.SetPost(p)
	var img *image.RGBA
	for i := range frames {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		draw(i)
		if img, err = g.end(true); err != nil {
			t.Fatal(err)
		}
	}
	if img == nil {
		t.Fatal("no frame was captured")
	}
	return img
}

// unlitPost is the settings the effect tests start from: no bloom, no
// occlusion, no grade, so only the effect under test changes anything.
func unlitPost() PostSettings {
	return PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true}
}

// TestTemporalAA checks the two things temporal anti-aliasing has to get
// right: a still scene converges, so a diagonal edge grows the
// intermediate values a single sample per pixel cannot have; and a moving
// box does not leave its history behind it.
func TestTemporalAA(t *testing.T) {
	g := newHeadless(t, 128, 128)
	qv, qi := QuadMesh()
	quad, err := g.NewMesh(qv, qi)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	white := Material{BaseColor: White, Unlit: true}
	// A quad turned off the pixel grid, so its edges are diagonal.
	still := func(int) {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0)})
		g.DrawMesh(quad, white, lin.Rotate(0.5, lin.V3(0, 0, 1)).Mul(lin.Scale(lin.V3(2.4, 2.4, 1))))
	}
	edges := func(img *image.RGBA) int {
		n := 0
		for y := range 128 {
			for x := range 128 {
				if v := lum(img, x, y); v > 40 && v < 200 {
					n++
				}
			}
		}
		return n
	}
	plain := edges(postShot(t, g, unlitPost(), 8, still))

	taa := unlitPost()
	taa.TemporalAA = true
	taa.TemporalBlend = 0.2
	shaded := edges(postShot(t, g, taa, 12, still))
	if shaded <= plain+20 {
		t.Errorf("diagonal edge: %d part-covered pixels without temporal anti-aliasing, %d with; expected many more", plain, shaded)
	}

	// A box crossing the screen. Each frame says where it was, so the
	// resolve reprojects it rather than averaging it in place.
	const step = 0.5
	at := func(i int) lin.Mat4 {
		return lin.Translate(lin.V3(-1.5+step*float32(i), 0, 0)).Mul(lin.Scale(lin.V3(0.8, 0.8, 1)))
	}
	moving := func(i int) {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 6), Target: lin.V3(0, 0, 0)})
		g.DrawMeshMoved(quad, white, at(i), at(i-1))
	}
	const frames = 8
	img := postShot(t, g, taa, frames, moving)
	// Where the box is now, and where it was three frames back.
	uv := func(i int) int {
		x := -1.5 + step*float32(i)
		return int((x/(6*0.57735)*0.5 + 0.5) * 128)
	}
	if now := lum(img, uv(frames-1), 64); now < 150 {
		t.Errorf("the box is at x=%d but that pixel reads %d; expected it bright", uv(frames-1), now)
	}
	if trail := lum(img, uv(frames-4), 64); trail > 60 {
		t.Errorf("three frames behind the box, x=%d reads %d; expected the background, so no trail", uv(frames-4), trail)
	}
}

// TestDepthOfField puts one quad at the focus distance and an equally
// large one far behind it. The far quad's blur spreads it past its own
// silhouette; the one in focus stays inside its edges.
func TestDepthOfField(t *testing.T) {
	g := newHeadless(t, 128, 128)
	qv, qi := QuadMesh()
	quad, err := g.NewMesh(qv, qi)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	white := Material{BaseColor: White, Unlit: true}
	// Both quads cover the same span of screen: one at five units, one at
	// twenty-five and five times the size.
	draw := func(int) {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 0), Target: lin.V3(0, 0, -1)})
		g.DrawMesh(quad, white, lin.Translate(lin.V3(-1.6, 0, -5)).Mul(lin.Scale(lin.V3(0.8, 0.8, 1))))
		g.DrawMesh(quad, white, lin.Translate(lin.V3(8, 0, -25)).Mul(lin.Scale(lin.V3(4, 4, 1))))
	}
	off := postShot(t, g, unlitPost(), 1, draw)
	// Five pixels beyond each quad's right edge, which is background.
	const nearOutside, farOutside = 43, 114
	if v := lum(off, nearOutside, 64); v > 20 {
		t.Fatalf("without depth of field, x=%d should be background, reads %d", nearOutside, v)
	}
	if v := lum(off, farOutside, 64); v > 20 {
		t.Fatalf("without depth of field, x=%d should be background, reads %d", farOutside, v)
	}
	p := unlitPost()
	p.FocusDistance, p.FocusRange, p.BokehRadius = 5, 1.5, 120
	on := postShot(t, g, p, 1, draw)
	near, far := lum(on, nearOutside, 64), lum(on, farOutside, 64)
	if far <= near+20 {
		t.Errorf("beyond the far quad reads %d and beyond the focused quad %d; expected the far one much brighter", far, near)
	}
	if near > 20 {
		t.Errorf("the quad at the focus distance spread to x=%d, which reads %d", nearOutside, near)
	}
}

// TestMotionBlur moves a bright quad sideways and checks the smear runs
// the way it moved: the trailing side of the quad picks up the background
// it came from, the leading side does not.
func TestMotionBlur(t *testing.T) {
	g := newHeadless(t, 128, 128)
	qv, qi := QuadMesh()
	quad, err := g.NewMesh(qv, qi)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	white := Material{BaseColor: White, Unlit: true}
	draw := func(int) {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0)})
		g.DrawMeshMoved(quad, white, lin.Identity(), lin.Translate(lin.V3(-1.5, 0, 0)))
	}
	off := postShot(t, g, unlitPost(), 1, draw)
	// Inside the quad, which spans about x = 53 to 75.
	const trailing, leading = 56, 72
	if a, b := lum(off, trailing, 64), lum(off, leading, 64); a < 150 || b < 150 || abs(a-b) > 12 {
		t.Fatalf("without motion blur the quad should be flat: x=%d reads %d, x=%d reads %d", trailing, a, leading, b)
	}
	p := unlitPost()
	p.MotionBlur = 1
	on := postShot(t, g, p, 1, draw)
	a, b := lum(on, trailing, 64), lum(on, leading, 64)
	if b <= a+20 {
		t.Errorf("with motion blur the trailing side reads %d and the leading side %d; expected the smear to fall off backwards", a, b)
	}
}

// TestLensEffects checks the two lens settings a pixel can be asserted
// on: aberration pulls the channels apart towards the edge of the frame,
// and distortion moves a known edge.
func TestLensEffects(t *testing.T) {
	g := newHeadless(t, 128, 128)
	// A white block over the left three quarters of a 2D frame, so its
	// edge sits away from the centre where both effects do nothing.
	draw := func(int) { g.FillRect(0, 0, 96, 128, White) }
	base := unlitPost()
	base.Post2D = true

	off := postShot(t, g, base, 1, draw)
	if r, b := int(off.RGBAAt(100, 64).R), int(off.RGBAAt(100, 64).B); r != b {
		t.Fatalf("without aberration the channels should agree: red %d, blue %d", r, b)
	}
	ab := base
	ab.Aberration = 30 // far past anything a game would want, so the split is unmistakable
	on := postShot(t, g, ab, 1, draw)
	c := on.RGBAAt(100, 64)
	// Red is sampled further out, into the background; blue further in,
	// still inside the block.
	if int(c.B) <= int(c.R)+80 {
		t.Errorf("with aberration at the edge the channels should split: %v", c)
	}

	// Distortion pushes the image outwards, so the block's edge on screen
	// moves towards the centre.
	edge := func(img *image.RGBA) int {
		for x := 60; x < 128; x++ {
			if lum(img, x, 64) < 128 {
				return x
			}
		}
		return -1
	}
	plain := edge(off)
	if plain < 90 || plain > 102 {
		t.Fatalf("the block's edge should be near x=96, found %d", plain)
	}
	dist := base
	dist.Distortion = 2
	bent := edge(postShot(t, g, dist, 1, draw))
	if bent >= plain-2 {
		t.Errorf("with barrel distortion the edge moved from %d to %d; expected it further in", plain, bent)
	}
}

// TestGodRays puts an occluder between part of the frame and the sun. A
// patch of sky whose line to the sun is clear brightens more than one
// whose line crosses the occluder.
func TestGodRays(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	// The camera looks straight at the sun, so it lands in the middle of
	// the frame. The block covers the right of it.
	draw := func(int) {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, 0, 1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.1, 0.1, 0.1, 1}})
		g.DrawMesh(cube, Material{BaseColor: Color{0.02, 0.02, 0.02, 1}, Unlit: true},
			lin.Translate(lin.V3(1.65, 0, 0)).Mul(lin.Scale(lin.V3(1.7, 6, 0.2))))
	}
	off := postShot(t, g, unlitPost(), 1, draw)
	p := unlitPost()
	p.GodRays = 1
	on := postShot(t, g, p, 1, draw)
	// x = 45 is sky with a clear line to the middle; x = 124 is sky whose
	// line to the middle crosses the block.
	clear := lum(on, 45, 64) - lum(off, 45, 64)
	blocked := lum(on, 124, 64) - lum(off, 124, 64)
	if clear <= blocked+15 {
		t.Errorf("god rays lifted the clear sky by %d and the blocked sky by %d; expected the shaft to show", clear, blocked)
	}
}

// TestPostChainTogether turns every effect on at once, on the screen and
// in a render texture, so the passes are exercised against the validation
// layers in the order and the combinations a game can ask for.
func TestPostChainTogether(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	rt, err := g.NewRenderTexture(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	lut, err := g.NewLUT(NeutralLUT(16))
	if err != nil {
		t.Fatal(err)
	}
	defer lut.Destroy()
	p := DefaultPost()
	p.TemporalAA = true
	p.FocusDistance, p.FocusRange = 6, 2
	p.MotionBlur = 0.6
	p.Aberration, p.Distortion, p.Grain, p.Ghosts = 2, 0.4, 0.05, 0.3
	p.GodRays = 0.8
	p.LUT, p.LUTStrength = lut, 0.5
	scene := func(i int) {
		g.SetCamera(Camera{Position: lin.V3(0, 2, 6), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0.2, -0.4, -1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}, Shadows: true})
		x := 0.3 * float32(i)
		g.DrawMeshMoved(cube, Material{BaseColor: White, Roughness: 0.4},
			lin.Translate(lin.V3(x, 0, 0)), lin.Translate(lin.V3(x-0.3, 0, 0)))
	}
	img := postShot(t, g, p, 4, func(i int) {
		g.DrawTo(rt, Black, func() { scene(i) })
		scene(i)
		g.DrawTexture(rt.Texture(), 0, 0)
	})
	lit := 0
	for y := range 128 {
		for x := range 128 {
			if lum(img, x, y) > 30 {
				lit++
			}
		}
	}
	if lit < 200 {
		t.Errorf("only %d pixels came out lit with every post effect on", lit)
	}
}

// TestPost2D checks that a frame with no 3D in it reaches the composite
// when Post2D is on: the vignette darkens the corners and leaves the
// middle alone, and the direct path is untouched.
func TestPost2D(t *testing.T) {
	g := newHeadless(t, 64, 64)
	draw := func(int) { g.FillRect(0, 0, 64, 64, RGB(200, 200, 200)) }
	p := unlitPost()
	p.Vignette = 1
	direct := postShot(t, g, p, 1, draw)
	if c, e := lum(direct, 32, 32), lum(direct, 2, 2); abs(c-e) > 4 {
		t.Errorf("without Post2D the frame goes straight to the screen: middle %d, corner %d", c, e)
	}
	p.Post2D = true
	posted := postShot(t, g, p, 1, draw)
	middle, corner := lum(posted, 32, 32), lum(posted, 2, 2)
	if corner >= middle-30 {
		t.Errorf("with Post2D and a vignette the corner should be darker: middle %d, corner %d", middle, corner)
	}
	// The middle of the frame keeps the colour the game drew, because a
	// 2D frame is not tone mapped.
	if plain := lum(direct, 32, 32); abs(middle-plain) > 6 {
		t.Errorf("Post2D changed the middle of the frame from %d to %d with nothing but a vignette on", plain, middle)
	}
}
