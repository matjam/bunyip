package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// TestSkinning builds a two-joint strip: the lower half follows joint 0,
// the upper half joint 1. Bending joint 1 by 90 degrees must move the
// upper half's pixels sideways while the lower half stays put.
func TestSkinning(t *testing.T) {
	g := newHeadless(t, 128, 128)
	// A vertical quad strip from y=-1 to y=1, x in [-0.2, 0.2], facing +Z.
	var verts []SkinVertex
	for i := range 3 {
		y := float32(i) - 1
		joint := uint8(0)
		if i >= 1 {
			joint = 1
		}
		for _, x := range []float32{-0.2, 0.2} {
			verts = append(verts, SkinVertex{Pos: lin.V3(x, y, 0), Normal: lin.V3(0, 0, 1), Joints: [4]uint8{joint}, Weights: [4]float32{1}})
		}
	}
	indices := []uint32{0, 1, 3, 0, 3, 2, 2, 3, 5, 2, 5, 4}
	mesh, err := g.NewSkinnedMesh(verts, indices)
	if err != nil {
		t.Fatal(err)
	}
	defer mesh.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1})
	render := func(joints []lin.Mat4) *image.RGBA {
		var img *image.RGBA
		for range 2 {
			ok, err := g.Begin(Black)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				continue
			}
			g.SetCamera(Camera{Position: lin.V3(0, 0, 4)})
			g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.3, 0.3, 0.3, 1}})
			g.DrawSkinned(mesh, Material{BaseColor: White, Roughness: 1, DoubleSided: true}, lin.Identity(), joints)
			if img, err = g.End(true); err != nil {
				t.Fatal(err)
			}
		}
		return img
	}
	lit := func(img *image.RGBA, x, y int) bool { return img.RGBAAt(x, y).R > 60 }
	rest := render([]lin.Mat4{lin.Identity(), lin.Identity()})
	// At z=4 with 60 degrees, one unit is about 27 px; the strip is 11 px wide.
	if !lit(rest, 64, 40) || !lit(rest, 64, 88) {
		t.Fatalf("rest pose should show the strip above and below centre: %v %v", rest.RGBAAt(64, 40), rest.RGBAAt(64, 88))
	}
	// Joint 1 rotates 90 degrees about Z at its base (y=0): the upper half
	// swings to the left (+Y maps to -X).
	bend := lin.Rotate(lin.Radians(90), lin.V3(0, 0, 1))
	bent := render([]lin.Mat4{lin.Identity(), bend})
	if lit(bent, 64, 40) {
		t.Errorf("upper half should have left the centre column: %v", bent.RGBAAt(64, 40))
	}
	if !lit(bent, 40, 64) {
		t.Errorf("upper half should now lie to the left: %v", bent.RGBAAt(40, 64))
	}
	if !lit(bent, 64, 88) {
		t.Errorf("lower half should be unchanged: %v", bent.RGBAAt(64, 88))
	}
}

// TestAnimPlayer checks clip sampling and hierarchy composition on a
// two-node chain without touching the GPU.
func TestAnimPlayer(t *testing.T) {
	m := &Model{
		nodes: []gltf.Node{
			{Name: "root", Parent: -1, Children: []int{1}, Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1)},
			{Name: "child", Parent: 0, Translation: lin.V3(0, 1, 0), Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1)},
		},
		clips: []gltf.Animation{{
			Name: "slide", Duration: 2,
			Channels: []gltf.Channel{{Node: 0, Path: gltf.PathTranslation, Times: []float32{0, 2}, Values: []lin.Vec4{{X: 0}, {X: 4}}}},
		}},
	}
	m.order = topoOrder(m.nodes)
	p := m.NewAnimPlayer()
	if !p.Play("slide", true) {
		t.Fatal("clip not found")
	}
	p.Advance(1)
	if got := p.NodeMatrix(1).MulPoint(lin.V3(0, 0, 0)); got.X != 2 || got.Y != 1 {
		t.Errorf("child at t=1: %v, want (2,1,0)", got)
	}
	p.Advance(1.5) // wraps to 0.5 when looping
	if got := p.NodeMatrix(0).MulPoint(lin.V3(0, 0, 0)); got.X != 1 {
		t.Errorf("root at wrapped t=0.5: %v, want x=1", got)
	}
}
