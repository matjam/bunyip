package gfx

import (
	"testing"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/lin"
)

// limbMesh is a skinned mesh in two lumps: a body box on joint 0 around
// the origin and a limb box on joint 1 four units above it, so the bind
// pose is much larger than either lump.
func limbMesh(t *testing.T, g *Graphics) *Mesh {
	t.Helper()
	var verts []SkinVertex
	for j, centre := range []lin.Vec3{{}, {Y: 4}} {
		for _, c := range [8][3]float32{{-1, -1, -1}, {1, -1, -1}, {1, 1, -1}, {-1, 1, -1}, {-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1}} {
			p := centre.Add(lin.V3(c[0], c[1], c[2]).Mul(0.5))
			verts = append(verts, SkinVertex{Pos: p, Normal: lin.V3(0, 0, 1), Joints: [4]uint8{uint8(j)}, Weights: [4]float32{1}})
		}
	}
	// Two triangles a lump, enough to draw and to carry every vertex.
	indices := []uint32{0, 1, 2, 0, 2, 3, 4, 5, 6, 4, 6, 7, 8, 9, 10, 8, 10, 11, 12, 13, 14, 12, 14, 15}
	m, err := g.NewSkinnedMesh(verts, indices)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestSkinBoundsFollowThePose draws a skinned mesh whose bind pose sits
// far above the view and whose joints bring it back in front of the
// camera. The bind pose's bounds, even doubled, are outside the frustum,
// so only bounds that follow the pose keep the draw.
func TestSkinBoundsFollowThePose(t *testing.T) {
	g := newHeadless(t, 64, 64)
	mesh := limbMesh(t, g)
	defer mesh.Destroy()
	draw := func(model lin.Mat4, joints []lin.Mat4) FrameStats {
		frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 12), Target: lin.V3(0, 0, 0)})
			g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: White, Ambient: Color{0.3, 0.3, 0.3, 1}})
			g.DrawSkinned(mesh, Material{Roughness: 1, DoubleSided: true}, model, joints)
		})
		return g.Stats()
	}
	up := lin.Translate(lin.V3(0, 40, 0))
	down := lin.Translate(lin.V3(0, -40, 0))
	// The mesh is placed forty units up and the pose brings it back.
	s := draw(up, []lin.Mat4{down, down})
	if s.Culled != 0 || s.Instances != 1 {
		t.Errorf("a posed mesh in front of the camera: culled %d, instances %d, want 0 and 1", s.Culled, s.Instances)
	}
	// The same mesh posed where the camera cannot see it is still culled.
	s = draw(lin.Translate(lin.V3(0, 400, 0)), []lin.Mat4{lin.Identity(), lin.Identity()})
	if s.Culled != 1 || s.Instances != 0 {
		t.Errorf("a posed mesh out of view: culled %d, instances %d, want 1 and 0", s.Culled, s.Instances)
	}
}

// TestDefaultCameraCulling draws a scene that never called SetCamera:
// culling has to use the same default camera the frame is rendered with,
// or it culls what the frame then draws.
func TestDefaultCameraCulling(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	cube, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	// One frame, since the queue keeps the camera it was given last.
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: White, Ambient: Color{0.3, 0.3, 0.3, 1}})
	g.DrawMesh(cube, Material{Roughness: 1}, lin.Identity())
	g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(50, 0, 0)))
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Culled != 1 || s.Instances != 1 {
		t.Errorf("without SetCamera: culled %d, instances %d, want 1 and 1", s.Culled, s.Instances)
	}
	if !bright(img, 32, 32) {
		t.Errorf("the cube is missing from the default camera's view: %v", img.RGBAAt(32, 32))
	}
}

// TestJointBounds checks the per-joint boxes a skinned mesh keeps and the
// sphere they give under a pose.
func TestJointBounds(t *testing.T) {
	g := newHeadless(t, 64, 64)
	mesh := limbMesh(t, g)
	defer mesh.Destroy()
	if n := len(mesh.jointMin); n != 2 {
		t.Fatalf("%d joint boxes, want 2", n)
	}
	if mesh.jointMin[1].Y != 3.5 || mesh.jointMax[1].Y != 4.5 {
		t.Errorf("joint 1's box is %v..%v, want y 3.5..4.5", mesh.jointMin[1], mesh.jointMax[1])
	}
	// Both joints held at the origin: the bounds shrink to one lump.
	fold := []lin.Mat4{lin.Identity(), lin.Translate(lin.V3(0, -4, 0))}
	centre, radius, ok := skinBounds(mesh, lin.Identity(), fold)
	if !ok || radius > 1 || centre.Len() > 0.01 {
		t.Errorf("folded pose: centre %v radius %v ok %v, want the origin and under 1", centre, radius, ok)
	}
	// A mesh whose bounds were set by hand keeps them instead.
	mesh.SetBounds(lin.V3(-9, -9, -9), lin.V3(9, 9, 9))
	if _, _, ok := skinBounds(mesh, lin.Identity(), fold); ok {
		t.Error("SetBounds should turn off the per-joint bounds")
	}
	if lo, hi := mesh.Bounds(); lo.X != -9 || hi.Z != 9 {
		t.Errorf("Bounds returned %v..%v after SetBounds", lo, hi)
	}
}

// TestSetBoundsSurvivesUpdate checks that bounds given by hand outlast
// new geometry, which a mesh rebuilt every frame depends on.
func TestSetBoundsSurvivesUpdate(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	m, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	m.SetBounds(lin.V3(-4, -4, -4), lin.V3(4, 4, 4))
	if err := m.Update(TransformVertices(v, lin.Scale(lin.V3(3, 3, 3))), i); err != nil {
		t.Fatal(err)
	}
	if lo, hi := m.Bounds(); lo.X != -4 || hi.X != 4 {
		t.Errorf("Update replaced the bounds set by hand: %v..%v", lo, hi)
	}
}

// TestVertexBounds checks how far a shader with a vertex program lets
// culling grow a draw's radius, and that a zero keeps such a draw.
func TestVertexBounds(t *testing.T) {
	hooked := &Shader{mesh: true, stages: map[shaders.Stage][]byte{shaders.StageVert: {}}}
	mesh := &Mesh{Min: lin.V3(-1, -1, -1), Max: lin.V3(1, 1, 1)}
	var q drawQueue
	d := meshDraw{mesh: mesh, model: lin.Identity(), shader: &Shader{}}
	_, plain, cullable := q.drawBounds(&d)
	if !cullable {
		t.Error("a draw with no vertex program should be cullable")
	}
	d.shader = hooked
	if _, r, cullable := q.drawBounds(&d); cullable || r != plain {
		t.Errorf("a zero VertexBounds gives radius %v cullable %v, want %v and false", r, cullable, plain)
	}
	hooked.VertexBounds = 2
	if _, r, cullable := q.drawBounds(&d); !cullable || r != plain*3 {
		t.Errorf("VertexBounds 2 gives radius %v cullable %v, want %v and true", r, cullable, plain*3)
	}
}
