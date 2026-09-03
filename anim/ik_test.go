package anim

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// applyIK moves a chain by the solver's rotations the way a skeleton
// would: the lower bone turns about the middle joint, then the whole
// chain turns about the root.
func applyIK(root, mid, end lin.Vec3, upper, lower lin.Quat) (newMid, newEnd lin.Vec3) {
	newMid = root.Add(upper.Rotate(mid.Sub(root)))
	newEnd = newMid.Add(upper.Rotate(lower.Rotate(end.Sub(mid))))
	return
}

func TestTwoBoneIK(t *testing.T) {
	hip, knee, foot := lin.V3(0, 2, 0), lin.V3(0, 1, 0.3), lin.V3(0, 0, 0)
	for _, tc := range []struct {
		name   string
		target lin.Vec3
		pole   lin.Vec3
		reach  bool // the target is within the leg's length
	}{
		{"straight down", lin.V3(0, 0.2, 0), lin.V3(0, 1, 2), true},
		{"forward step", lin.V3(0.3, 0.5, 0.8), lin.V3(0, 1, 2), true},
		{"pole behind", lin.V3(0.3, 0.5, 0.8), lin.V3(0, 1, -2), true},
		{"sideways", lin.V3(1, 1, 0), lin.V3(2, 1, 1), true},
		{"out of reach", lin.V3(0, -3, 0), lin.V3(0, 1, 2), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upper, lower := TwoBoneIK(hip, knee, foot, tc.target, tc.pole)
			newKnee, newFoot := applyIK(hip, knee, foot, upper, lower)
			if tc.reach {
				if d := newFoot.Distance(tc.target); d > 1e-3 {
					t.Fatalf("foot at %v, %v from target %v", newFoot, d, tc.target)
				}
			} else {
				// Straight along the target direction, almost as long as
				// the leg: the solver clamps the reach 1e-4 short of fully
				// straight to keep its angles well conditioned, so the
				// tolerance here must sit clear of that margin.
				dir := tc.target.Sub(hip).Norm()
				length := knee.Distance(hip) + foot.Distance(knee)
				d := newFoot.Sub(hip)
				if short := length - d.Len(); short < -1e-4 || short > 1e-3 || d.Norm().Dot(dir) < 0.999 {
					t.Fatalf("out of reach: foot offset %v", d)
				}
			}
			// Bone lengths are unchanged.
			if !near(newKnee.Distance(hip), knee.Distance(hip)) || !near(newFoot.Distance(newKnee), foot.Distance(knee)) {
				t.Fatalf("bone lengths changed: %v %v", newKnee.Distance(hip), newFoot.Distance(newKnee))
			}
			// The knee leans towards the pole, measured across the hip-target line.
			n := tc.target.Sub(hip).Norm()
			k := newKnee.Sub(hip)
			k = k.Sub(n.Mul(k.Dot(n)))
			pd := tc.pole.Sub(hip)
			pd = pd.Sub(n.Mul(pd.Dot(n)))
			if k.Len() > 1e-3 && k.Norm().Dot(pd.Norm()) < 0.999 {
				t.Fatalf("knee %v does not lean towards pole %v", k, pd)
			}
		})
	}
	// Degenerate chains return identities rather than NaNs.
	u, l := TwoBoneIK(hip, hip, foot, lin.V3(1, 1, 1), lin.V3(0, 0, 1))
	if u != lin.QuatIdentity() || l != lin.QuatIdentity() {
		t.Fatalf("zero-length bone: %v %v", u, l)
	}
}

func TestLookAt(t *testing.T) {
	q := LookAt(lin.V3(0, 0, 1), lin.V3(1, 0, 0), 0)
	if f := q.Rotate(lin.V3(0, 0, 1)); !near(f.X, 1) || !near(f.Z, 0) {
		t.Fatalf("full turn faces %v", f)
	}
	q = LookAt(lin.V3(0, 0, 1), lin.V3(1, 0, 0), lin.Radians(30))
	if f := q.Rotate(lin.V3(0, 0, 1)); !near(f.X, float32(math.Sin(math.Pi/6))) || !near(f.Z, float32(math.Cos(math.Pi/6))) {
		t.Fatalf("limited turn faces %v", f)
	}
	// Opposite directions turn half way round without a NaN.
	q = LookAt(lin.V3(0, 0, 1), lin.V3(0, 0, -1), 0)
	if f := q.Rotate(lin.V3(0, 0, 1)); !near(f.Z, -1) {
		t.Fatalf("about-face faces %v", f)
	}
}

// chainDoc is a glTF with a leg of three nodes and no geometry.
func chainDoc(t *testing.T) *gltf.Document {
	t.Helper()
	doc, err := gltf.Parse([]byte(`{"asset":{"version":"2.0"},
"nodes":[{"name":"hip","translation":[0,2,0],"children":[1]},{"name":"knee","translation":[0,-1,0.3],"children":[2]},
{"name":"foot","translation":[0,-1,-0.3],"children":[3]},{"name":"toe","translation":[0,0,0.2]}],
"scenes":[{"nodes":[0]}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// headless makes a Graphics without a window, or skips without Vulkan.
func headless(t *testing.T) *gfx.Graphics {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "anim_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	gd, err := hook.NewGraphics(r) // the engine builds the context; a test does it the same way
	if err != nil {
		r.Destroy()
		t.Fatalf("new graphics: %v", err)
	}
	t.Cleanup(func() { gd.Destroy(); r.Destroy() })
	return gd.Game().(*gfx.Graphics)
}

func TestSolveOnPlayer(t *testing.T) {
	g := headless(t)
	model, err := g.LoadModel(chainDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	defer model.Destroy()
	hip, knee, foot, toe := model.NodeIndex("hip"), model.NodeIndex("knee"), model.NodeIndex("foot"), model.NodeIndex("toe")
	p := model.NewAnimPlayer()
	target := lin.V3(0.4, 0.6, 0.5)
	p.PostPose = func(p *gfx.AnimPlayer) {
		SolveTwoBoneIK(p, hip, knee, foot, target, lin.V3(0, 1, 3))
	}
	p.Advance(0)
	if d := p.NodePosition(foot).Distance(target); d > 1e-3 {
		t.Fatalf("foot %v is %v from target", p.NodePosition(foot), d)
	}
	if k := p.NodePosition(knee); k.Z <= 0 {
		t.Fatalf("knee %v should bend forward, towards the pole", k)
	}
	// The toe, a child of the foot, followed the chain.
	if d := p.NodePosition(toe).Distance(p.NodePosition(foot)); !near(d, 0.2) {
		t.Fatalf("toe %v from foot", d)
	}
	// LookAtNode turns the foot's +Z towards a point, within a limit.
	p.PostPose = nil
	p.Advance(0)
	LookAtNode(p, foot, lin.V3(0, 0, 1), p.NodePosition(foot).Add(lin.V3(1, 0, 0)), 0)
	if f := p.NodeRotation(foot).Rotate(lin.V3(0, 0, 1)); !near(f.X, 1) {
		t.Fatalf("foot faces %v", f)
	}
}

// walkDoc is a root node with a clip that moves it 2 units along +z per second.
func walkDoc(t *testing.T) *gltf.Document {
	t.Helper()
	var bin []byte
	for _, f := range []float32{0, 1, 0, 0, 0, 0, 0, 2} {
		bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(f))
	}
	doc, err := gltf.Parse([]byte(fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteLength":8},{"buffer":0,"byteOffset":8,"byteLength":24}],
"accessors":[{"bufferView":0,"componentType":5126,"count":2,"type":"SCALAR"},{"bufferView":1,"componentType":5126,"count":2,"type":"VEC3"}],
"nodes":[{"name":"root"}],"scenes":[{"nodes":[0]}],
"animations":[{"name":"walk","channels":[{"sampler":0,"target":{"node":0,"path":"translation"}}],"samplers":[{"input":0,"output":1}]}]}`,
		len(bin), base64.StdEncoding.EncodeToString(bin))), nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSkeletonEventsAndRootMotion(t *testing.T) {
	g := headless(t)
	model, err := g.LoadModel(walkDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	defer model.Destroy()
	w := ecs.NewWorld()
	w.AddSystem("anim", System)
	p := model.NewAnimPlayer()
	p.AddEvent("walk", 0.5, "step")
	p.SetRootMotion("root")
	p.Play("walk", true)
	// Facing +x (a quarter turn about y), the clip's +z motion moves the entity along -x... no: +z turned by +90 about y is +x.
	e := w.SpawnWith(gfx.Transform{Rotation: lin.AxisAngle(lin.V3(0, 1, 0), lin.Radians(90))}, Skeleton{Player: p})
	w.Update(0.5)
	tr, _ := ecs.Get[gfx.Transform](w, e)
	if !near(tr.Position.X, 1) || !near(tr.Position.Z, 0) {
		t.Fatalf("root motion moved the entity to %v, want (1,0,0)", tr.Position)
	}
	if ev := ecs.Events[SkeletonEvent](w); len(ev) != 1 || ev[0].Entity != e || ev[0].Event.Name != "step" {
		t.Fatalf("skeleton events %v", ev)
	}
	// KeepRootMotion leaves the transform to the game.
	s, _ := ecs.Get[Skeleton](w, e)
	s.KeepRootMotion = true
	w.Update(0.25)
	tr, _ = ecs.Get[gfx.Transform](w, e)
	if delta, _ := p.RootMotion(); !near(tr.Position.X, 1) || !near(delta.Z, 0.5) {
		t.Fatalf("kept root motion: entity %v delta %v", tr.Position, delta)
	}
}
