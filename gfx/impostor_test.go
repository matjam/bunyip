package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// redCubeModel builds a one-unit cube of a plain red material as a glTF
// document in memory, which is the smallest thing an impostor can be
// baked from.
func redCubeModel(t *testing.T, g *Graphics) *Model {
	t.Helper()
	cv, ci := CubeMesh()
	prim := gltf.Primitive{Indices: ci, Material: 0}
	for _, v := range cv {
		prim.Positions = append(prim.Positions, v.Pos)
		prim.Normals = append(prim.Normals, v.Normal)
		prim.UVs = append(prim.UVs, v.UV)
	}
	doc := &gltf.Document{
		Meshes:    []gltf.Mesh{{Name: "cube", Primitives: []gltf.Primitive{prim}}},
		Materials: []gltf.Material{{Name: "red", BaseColor: [4]float32{1, 0, 0, 1}, Roughness: 1, Image: -1, MetalRoughImage: -1, NormalImage: -1, EmissiveImage: -1, OcclusionImage: -1, TransmissionImage: -1, ThicknessImage: -1, UVScale: [2]float32{1, 1}}},
		Nodes:     []gltf.Node{{Name: "cube", Parent: -1, Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1), Mesh: 0, Skin: -1}},
		Instances: []gltf.Instance{{Name: "cube", Mesh: 0, Node: 0, Skin: -1, World: lin.Identity()}},
	}
	m, err := g.LoadModel(doc)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// reddish reports whether a pixel is dominated by its red channel, which
// is what a lit red cube and a baked impostor of one have in common
// whatever the exposure.
func reddish(p *image.RGBA, x, y int) bool {
	i := p.PixOffset(x, y)
	r, gc, b := int(p.Pix[i]), int(p.Pix[i+1]), int(p.Pix[i+2])
	return r > 40 && r > gc*2 && r > b*2
}

// TestImpostor bakes a red cube and draws the impostor where the model
// would stand. The impostor must be red over the cube's silhouette and
// leave the background alone outside it.
func TestImpostor(t *testing.T) {
	g := newHeadless(t, 96, 96)
	model := redCubeModel(t, g)
	defer model.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	im, err := g.BakeImpostor(model, ImpostorOptions{Views: 8, Resolution: 64, Pitch: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer im.Destroy()
	if im.Views() != 8 {
		t.Errorf("the impostor holds %d views, want 8", im.Views())
	}
	if im.Texture() == nil {
		t.Fatal("the impostor has no atlas")
	}
	at := lin.V3(0, 0, 0)
	scene := func(draw func()) *image.RGBA {
		return frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 9), Target: at})
			g.SetLight(Light{Direction: lin.V3(-0.45, -0.75, -0.5), Color: Color{1.6, 1.55, 1.45, 1},
				Sky: Sky{Zenith: Color{0.28, 0.34, 0.45, 1}, Ground: Color{0.16, 0.15, 0.13, 1}}})
			draw()
		})
	}
	cube := scene(func() { g.DrawModel(model, lin.Identity()) })
	fake := scene(func() { g.DrawImpostor(im, at, 0, White) })

	// The middle of the frame is the middle of the cube in both.
	if !reddish(cube, 48, 48) {
		t.Fatal("the model itself is not red in the middle of the frame")
	}
	if !reddish(fake, 48, 48) {
		i := fake.PixOffset(48, 48)
		t.Errorf("the impostor is %v in the middle of the frame, want red", fake.Pix[i:i+4])
	}
	// Both must be red over roughly the same area: the impostor is one
	// quad of the cube's own size seen from the same direction.
	cubeRed, fakeRed, both := 0, 0, 0
	for y := range 96 {
		for x := range 96 {
			c, f := reddish(cube, x, y), reddish(fake, x, y)
			if c {
				cubeRed++
			}
			if f {
				fakeRed++
			}
			if c && f {
				both++
			}
		}
	}
	if cubeRed == 0 {
		t.Fatal("the model covered no pixels")
	}
	if both*10 < cubeRed*7 {
		t.Errorf("the impostor covers %d of the model's %d red pixels, want most of them", both, cubeRed)
	}
	if fakeRed > cubeRed*2 {
		t.Errorf("the impostor covers %d pixels, the model %d: the cutout is leaking", fakeRed, cubeRed)
	}
	// A corner of the frame is background in both, so the atlas is not
	// dragging an opaque black square around with it.
	if got, want := fake.Pix[fake.PixOffset(2, 2):fake.PixOffset(2, 2)+3], cube.Pix[cube.PixOffset(2, 2):cube.PixOffset(2, 2)+3]; string(got) != string(want) {
		t.Errorf("the impostor's background is %v, the model's %v", got, want)
	}
}

// TestImpostorMatchesTheModel bakes under the same light the frame draws
// under and compares the two: an impostor that stands in for a model must
// be about the colour the model is, or it will pop when the distance
// swaps them.
func TestImpostorMatchesTheModel(t *testing.T) {
	g := newHeadless(t, 64, 64)
	model := redCubeModel(t, g)
	defer model.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	light := Light{Direction: lin.V3(-0.45, -0.75, -0.5), Color: Color{1, 0.98, 0.92, 1},
		Sky: Sky{Zenith: Color{0.28, 0.34, 0.45, 1}, Ground: Color{0.16, 0.15, 0.13, 1}}}
	im, err := g.BakeImpostor(model, ImpostorOptions{Views: 8, Resolution: 64, Pitch: 0, Light: light})
	if err != nil {
		t.Fatal(err)
	}
	defer im.Destroy()
	scene := func(draw func()) *image.RGBA {
		return frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.Vec3{}})
			g.SetLight(light)
			draw()
		})
	}
	cube := scene(func() { g.DrawModel(model, lin.Identity()) })
	fake := scene(func() { g.DrawImpostor(im, lin.Vec3{}, 0, White) })
	// Average the pixels the two have in common, so the comparison is of
	// the model's face rather than of its silhouette.
	var sum [2][3]int
	n := 0
	for y := range 64 {
		for x := range 64 {
			if !reddish(cube, x, y) || !reddish(fake, x, y) {
				continue
			}
			i := cube.PixOffset(x, y)
			for c := range 3 {
				sum[0][c] += int(cube.Pix[i+c])
				sum[1][c] += int(fake.Pix[i+c])
			}
			n++
		}
	}
	if n < 50 {
		t.Fatalf("only %d pixels are red in both frames", n)
	}
	for c := range 3 {
		a, b := sum[0][c]/n, sum[1][c]/n
		if a-b > 24 || b-a > 24 {
			t.Errorf("channel %d averages %d on the model and %d on the impostor", c, a, b)
		}
	}
}

// TestDrawModelImpostor checks the swap: inside Distance the call draws
// exactly what DrawModel draws, beyond it exactly what DrawImpostor
// draws.
func TestDrawModelImpostor(t *testing.T) {
	g := newHeadless(t, 64, 64)
	model := redCubeModel(t, g)
	defer model.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	im, err := g.BakeImpostor(model, ImpostorOptions{Views: 8, Resolution: 64, Pitch: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer im.Destroy()
	at := Transform{Position: lin.V3(0, 0, 0)}
	scene := func(draw func()) *image.RGBA {
		return frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 9), Target: at.Position})
			g.SetLight(Light{Direction: lin.V3(-0.45, -0.75, -0.5), Color: White})
			draw()
		})
	}
	wantNear := scene(func() { g.DrawModel(model, at.Matrix()) })
	wantFar := scene(func() { g.DrawImpostor(im, at.Position, 0, White) })
	im.Distance = 100 // the camera is nine units away, so the model wins
	if got := scene(func() { g.DrawModelImpostor(model, im, at) }); imageDiff(got, wantNear) != 0 {
		t.Error("inside Distance the swap did not draw the model")
	}
	im.Distance = 1 // now the impostor wins
	if got := scene(func() { g.DrawModelImpostor(model, im, at) }); imageDiff(got, wantFar) != 0 {
		t.Error("beyond Distance the swap did not draw the impostor")
	}
	if got := scene(func() { g.DrawModelImpostor(model, nil, at) }); imageDiff(got, wantNear) != 0 {
		t.Error("without an impostor the swap did not draw the model")
	}
}

// TestImpostorViewChoice checks the ring maps a camera direction to the
// cell baked from it, at the seam as well as in the middle of a step.
func TestImpostorViewChoice(t *testing.T) {
	im := &Impostor{views: 8, cols: 3, rows: 3}
	cases := []struct {
		name string
		dir  lin.Vec3
		yaw  float32
		want int
	}{
		{"straight down +z", lin.V3(0, 0, 1), 0, 0},
		{"a quarter turn", lin.V3(1, 0, 0), 0, 2},
		{"the far side", lin.V3(0, 0, -1), 0, 4},
		{"just past the last step wraps to the first", lin.V3(-0.05, 0, 1), 0, 0},
		{"the model's own yaw turns the choice back", lin.V3(1, 0, 0), lin.Radians(90), 0},
		{"height does not matter", lin.V3(1, 9, 0), 0, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := im.view(c.dir, c.yaw); got != c.want {
				t.Errorf("view = %d, want %d", got, c.want)
			}
		})
	}
	// Every view must map to a distinct cell inside the atlas.
	seen := map[lin.Vec2]bool{}
	for k := range im.views {
		uv0, uv1 := im.region(k)
		if seen[uv0] {
			t.Errorf("view %d repeats a cell at %v", k, uv0)
		}
		seen[uv0] = true
		if uv1.X > 1.0001 || uv1.Y > 1.0001 {
			t.Errorf("view %d runs off the atlas at %v", k, uv1)
		}
	}
}

// TestBakeImpostorRefusesInsideAFrame checks the one rule the bake has:
// it runs a frame of its own, so it cannot be called from Draw.
func TestBakeImpostorRefusesInsideAFrame(t *testing.T) {
	g := newHeadless(t, 32, 32)
	model := redCubeModel(t, g)
	defer model.Destroy()
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skip("the frame was skipped")
	}
	_, bakeErr := g.BakeImpostor(model, ImpostorOptions{Views: 4, Resolution: 32})
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
	if bakeErr == nil {
		t.Error("baking inside a frame was allowed")
	}
}
