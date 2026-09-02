package gfx

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

func nearf(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

// rigModel is a three-node chain (root, spine, head) with a walk clip
// that slides and turns the root, and a wave clip that bends the head.
func rigModel() *Model {
	id, one := lin.QuatIdentity(), lin.V3(1, 1, 1)
	m := &Model{
		nodes: []gltf.Node{
			{Name: "root", Parent: -1, Children: []int{1}, Rotation: id, Scale: one},
			{Name: "spine", Parent: 0, Children: []int{2}, Translation: lin.V3(0, 1, 0), Rotation: id, Scale: one},
			{Name: "head", Parent: 1, Translation: lin.V3(0, 1, 0), Rotation: id, Scale: one},
		},
		clips: []gltf.Animation{
			{Name: "walk", Duration: 2, Channels: []gltf.Channel{
				{Node: 0, Path: gltf.PathTranslation, Times: []float32{0, 2}, Values: []lin.Vec4{{}, {Z: 4}}},
				{Node: 0, Path: gltf.PathRotation, Times: []float32{0, 2}, Values: []lin.Vec4{{W: 1}, quatV4(lin.AxisAngle(lin.V3(0, 1, 0), lin.Radians(90)))}},
				{Node: 1, Path: gltf.PathTranslation, Times: []float32{0, 2}, Values: []lin.Vec4{{Y: 1}, {Y: 1}}},
			}},
			{Name: "wave", Duration: 1, Channels: []gltf.Channel{
				{Node: 2, Path: gltf.PathRotation, Times: []float32{0, 1}, Values: []lin.Vec4{{W: 1}, quatV4(lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(90)))}},
				{Node: 1, Path: gltf.PathTranslation, Times: []float32{0, 1}, Values: []lin.Vec4{{Y: 3}, {Y: 3}}},
			}},
		},
	}
	m.order = topoOrder(m.nodes)
	return m
}

func quatV4(q lin.Quat) lin.Vec4 { return lin.V4(q.X, q.Y, q.Z, q.W) }

func TestAnimEvents(t *testing.T) {
	p := rigModel().NewAnimPlayer()
	p.AddEvent("walk", 0, "start")
	p.AddEvent("walk", 1, "step")
	p.AddEvent("walk", 2, "end")
	var seen []string
	p.OnEvent = func(e AnimEvent) { seen = append(seen, e.Name) }
	names := func() []string {
		var out []string
		for _, e := range p.Events() {
			out = append(out, e.Name)
		}
		return out
	}
	p.Play("walk", true) // fires the event at 0
	if got := names(); len(got) != 1 || got[0] != "start" {
		t.Fatalf("on play: %v", got)
	}
	p.Advance(0.5)
	if got := names(); len(got) != 0 {
		t.Fatalf("at 0.5: %v", got)
	}
	p.Advance(0.5) // exactly on the step key
	if got := names(); len(got) != 1 || got[0] != "step" {
		t.Fatalf("at 1: %v", got)
	}
	p.Advance(1.2) // wraps: end and start (2 and 0 coincide on a loop) then nothing else before 0.2
	got := names()
	if len(got) != 2 || got[0] != "start" || got[1] != "end" {
		t.Fatalf("on wrap: %v", got)
	}
	if len(seen) != 4 {
		t.Fatalf("callback saw %v", seen)
	}
	// A crossfade keeps firing the outgoing clip's events while it fades.
	p.Play("walk", true)
	p.Advance(0.5)
	p.CrossFade("wave", true, 1)
	p.Advance(0.6)
	if got := names(); len(got) != 1 || got[0] != "step" {
		t.Fatalf("fading clip's events: %v", got)
	}
	// A non-looping clip fires its end event once and then stays quiet.
	p.Play("walk", false)
	p.Advance(3)
	p.Advance(1)
	if got := names(); len(got) != 0 || !p.Finished() {
		t.Fatalf("after the end: %v finished %v", got, p.Finished())
	}
	// Events on a layer fire too.
	p.AddEvent("wave", 0.5, "mid")
	p.Play("walk", true)
	p.Layer("wave", 1, nil)
	p.Advance(0.6)
	if got := names(); len(got) != 1 || got[0] != "mid" {
		t.Fatalf("layer events: %v", got)
	}
}

func TestRootMotion(t *testing.T) {
	p := rigModel().NewAnimPlayer()
	if !p.SetRootMotion("root") || p.SetRootMotion("nope") {
		t.Fatal("SetRootMotion lookup")
	}
	p.Play("walk", true)
	p.Advance(0.5)
	delta, yaw := p.RootMotion()
	if !nearf(delta.Z, 1) || !nearf(yaw, lin.Radians(22.5)) {
		t.Fatalf("quarter clip: delta %v yaw %v", delta, yaw)
	}
	// The pose no longer carries the movement: the root stays at rest.
	if pos := p.NodePosition(0); pos != (lin.Vec3{}) {
		t.Fatalf("root should stay put, at %v", pos)
	}
	if head := p.NodePosition(2); !nearf(head.X, 0) || !nearf(head.Z, 0) {
		t.Fatalf("yaw should be stripped from the pose: head at %v", head)
	}
	// Crossing the loop point adds up the end of one cycle and the start of the next.
	p.Advance(1.75) // 0.5 -> 2.25, wraps to 0.25
	delta, yaw = p.RootMotion()
	if !nearf(delta.Z, 3.5) || !nearf(yaw, lin.Radians(90*1.75/2)) {
		t.Fatalf("across the wrap: delta %v yaw %v", delta, yaw)
	}
	// Off again, the root moves in the pose and reports nothing.
	p.SetRootMotion("")
	p.Advance(0.25)
	delta, _ = p.RootMotion()
	if delta != (lin.Vec3{}) || !nearf(p.NodePosition(0).Z, 1) {
		t.Fatalf("root motion off: delta %v root %v", delta, p.NodePosition(0))
	}
}

func TestAnimLayers(t *testing.T) {
	m := rigModel()
	p := m.NewAnimPlayer()
	p.Play("walk", true)
	p.Advance(1) // root at z=2, head straight up at y=2 from root
	base := p.NodePosition(2)
	if !nearf(base.Y, 2) {
		t.Fatalf("walk pose head %v", base)
	}
	// An override layer on the head subtree: the head bends but the spine
	// (which wave also animates, to y=3) keeps walk's placement.
	l := p.Layer("wave", 1, m.MaskSubtree("head"))
	if l == nil || p.Layer("nope", 1, nil) != nil {
		t.Fatal("Layer lookup")
	}
	p.Advance(0.5) // wave at 0.5: head turned 45 degrees about z
	if spine := p.NodePosition(1); !nearf(spine.Y, 1) {
		t.Fatalf("masked layer touched the spine: %v", spine)
	}
	// tip is where the head's +y axis points, in the spine's frame, so
	// the walk clip's turning of the root does not matter.
	tip := func() lin.Vec3 {
		local := p.NodeMatrix(1).Inverse().Mul(p.NodeMatrix(2))
		return local.MulPoint(lin.V3(0, 1, 0)).Sub(local.MulPoint(lin.Vec3{}))
	}
	if got := tip(); !nearf(got.X, -float32(math.Sqrt2/2)) || !nearf(got.Y, float32(math.Sqrt2/2)) {
		t.Fatalf("head did not bend: %v", got)
	}
	// Half weight bends half as far.
	l.Weight = 0.5
	p.Advance(0)
	half := tip()
	full := lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(22.5)).Rotate(lin.V3(0, 1, 0))
	if !nearf(half.X, full.X) || !nearf(half.Y, full.Y) {
		t.Fatalf("half-weight bend %v want %v", half, full)
	}
	// Additive on the spine adds wave's offset from rest (y+2) to walk's.
	p.RemoveLayer(l)
	add := p.Layer("wave", 1, m.MaskNodes("spine"))
	add.Additive = true
	p.Advance(0)
	if s := p.NodePosition(1); !nearf(s.Y, 3) {
		t.Fatalf("additive spine %v want y=3", s)
	}
	add.Weight = 0.5
	p.Advance(0)
	if s := p.NodePosition(1); !nearf(s.Y, 2) {
		t.Fatalf("half additive spine %v want y=2", s)
	}
	// A one-shot layer holds its last frame and reports it.
	add.Loop = false
	p.Advance(5)
	if !add.Finished() || !nearf(float32(add.Time()), 1) {
		t.Fatalf("one-shot layer finished %v time %v", add.Finished(), add.Time())
	}
	if len(p.Layers()) != 1 || p.Layers()[0].Clip() != "wave" {
		t.Fatalf("layers %v", p.Layers())
	}
}

func TestNodeOverrides(t *testing.T) {
	p := rigModel().NewAnimPlayer()
	calls := 0
	p.PostPose = func(p *AnimPlayer) {
		calls++
		// Turn the spine a quarter turn about z in model space: the head
		// (one unit up the spine) swings to -x.
		p.RotateNode(1, lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(90)))
	}
	p.Play("walk", true)
	p.Advance(0)
	if calls != 2 { // Play's own Advance(0) and ours
		t.Fatalf("PostPose ran %d times", calls)
	}
	head := p.NodePosition(2)
	if !nearf(head.X, -1) || !nearf(head.Y, 1) {
		t.Fatalf("rotated head at %v, want (-1,1,0)", head)
	}
	// RotateNode in model space accounts for the parent's rotation.
	p.PostPose = nil
	p.Advance(1) // root has turned 45 degrees about y
	p.RotateNode(1, lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(90)))
	head = p.NodePosition(2).Sub(p.NodePosition(1))
	if !nearf(head.X, -1) || !nearf(head.Y, 0) || !nearf(head.Z, 0) {
		t.Fatalf("model-space turn under a rotated parent: %v, want (-1,0,0)", head)
	}
	// SetNodeLocal and NodeLocal round-trip, and the pose is rebuilt.
	p.SetNodeLocal(2, lin.V3(0, 5, 0), lin.QuatIdentity(), lin.V3(1, 1, 1))
	if tr, _, _ := p.NodeLocal(2); tr.Y != 5 {
		t.Fatalf("NodeLocal %v", tr)
	}
	if d := p.NodePosition(2).Sub(p.NodePosition(1)).Len(); !nearf(d, 5) {
		t.Fatalf("head distance %v want 5", d)
	}
	// The next Advance samples the clip again and drops the override.
	p.Advance(0)
	if d := p.NodePosition(2).Sub(p.NodePosition(1)).Len(); !nearf(d, 1) {
		t.Fatalf("override should not persist: %v", d)
	}
	// Overrides persist while nothing plays; the root is still turned 45
	// degrees about y, so the local -x offset lands at (-0.7, 0, 0.7).
	p.Stop()
	p.SetNodeRotation(1, lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(90)))
	p.Advance(1)
	if head := p.NodePosition(2).Sub(p.NodePosition(1)); !nearf(head.X, -float32(math.Sqrt2/2)) || !nearf(head.Y, 0) {
		t.Fatalf("override while stopped: %v", head)
	}
}

// morphDoc is a triangle with one morph target that lifts its apex by
// one unit, and a clip that animates the weight from 0 to 1 over a
// second.
func morphDoc(t *testing.T, skinned bool) *gltf.Document {
	t.Helper()
	var bin []byte
	f32 := func(vals ...float32) {
		for _, v := range vals {
			bin = binary.LittleEndian.AppendUint32(bin, math.Float32bits(v))
		}
	}
	f32(0, 0, 0, 1, 0, 0, 0, 1, 0)        // positions, 36 bytes at 0
	f32(0, 0, 0, 0, 0, 0, 0, 1, 0)        // target position deltas, 36 bytes at 36
	f32(0, 1)                             // times, 8 bytes at 72
	f32(0, 1)                             // weights, 8 bytes at 80
	for _, i := range []uint16{0, 1, 2} { // indices, 6 bytes at 88
		bin = binary.LittleEndian.AppendUint16(bin, i)
	}
	bin = append(bin, 0, 0) // pad to 96
	for range 3 {           // joints as bytes, 12 bytes at 96
		bin = append(bin, 0, 0, 0, 0)
	}
	f32(1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0) // weights, 48 bytes at 108
	attrs, skins, nodeSkin := "", "", ""
	if skinned {
		attrs = `,"JOINTS_0":5,"WEIGHTS_0":6`
		skins = `,"skins":[{"joints":[0]}]`
		nodeSkin = `,"skin":0`
	}
	src := fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":36},
 {"buffer":0,"byteOffset":72,"byteLength":8},{"buffer":0,"byteOffset":80,"byteLength":8},
 {"buffer":0,"byteOffset":88,"byteLength":6},{"buffer":0,"byteOffset":96,"byteLength":12},{"buffer":0,"byteOffset":108,"byteLength":48}],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5126,"count":3,"type":"VEC3"},
 {"bufferView":2,"componentType":5126,"count":2,"type":"SCALAR"},{"bufferView":3,"componentType":5126,"count":2,"type":"SCALAR"},
 {"bufferView":4,"componentType":5123,"count":3,"type":"SCALAR"},{"bufferView":5,"componentType":5121,"count":3,"type":"VEC4"},
 {"bufferView":6,"componentType":5126,"count":3,"type":"VEC4"}],
"meshes":[{"name":"tri","weights":[0],"extras":{"targetNames":["lift"]},"primitives":[{"attributes":{"POSITION":0%s},"indices":4,"targets":[{"POSITION":1}]}]}],
"nodes":[{"name":"tri","mesh":0%s}],"scenes":[{"nodes":[0]}]%s,
"animations":[{"name":"lift","channels":[{"sampler":0,"target":{"node":0,"path":"weights"}}],"samplers":[{"input":2,"output":3}]}]}`,
		len(bin), base64.StdEncoding.EncodeToString(bin), attrs, nodeSkin, skins)
	doc, err := gltf.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestMorphTargets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		skinned bool
	}{{"plain", false}, {"skinned", true}} {
		t.Run(tc.name, func(t *testing.T) {
			g := newHeadless(t, 32, 32)
			doc := morphDoc(t, tc.skinned)
			model, err := g.LoadModel(doc)
			if err != nil {
				t.Fatal(err)
			}
			defer model.Destroy()
			if names := model.MorphTargets(0); len(names) != 1 || names[0] != "lift" {
				t.Fatalf("target names %v", names)
			}
			mesh := model.Parts[0].Mesh
			if mesh.skinned != tc.skinned {
				t.Fatalf("skinned %v", mesh.skinned)
			}
			apex := func() lin.Vec3 { return mesh.Vertices()[2].Pos }
			if apex().Y != 1 {
				t.Fatalf("rest apex %v", apex())
			}
			if err := model.SetMorphWeights(0, []float32{0.5}); err != nil {
				t.Fatal(err)
			}
			if !nearf(apex().Y, 1.5) || model.MorphWeights(0)[0] != 0.5 {
				t.Fatalf("half lift apex %v weights %v", apex(), model.MorphWeights(0))
			}
			if model.SetMorphWeights(3, nil) == nil {
				t.Fatal("node without targets should error")
			}
			// The player's weights channel drives the blend through DrawModelAnimated.
			p := model.NewAnimPlayer()
			if w := p.MorphWeights(0); len(w) != 1 || w[0] != 0 {
				t.Fatalf("rest weights %v", w)
			}
			p.Play("lift", false)
			p.Advance(1)
			if w := p.MorphWeights(0); !nearf(w[0], 1) {
				t.Fatalf("animated weight %v", w)
			}
			for range 2 {
				ok, err := g.begin(Black)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					continue
				}
				g.SetCamera(Camera{Position: lin.V3(0, 0, 4)})
				g.DrawModelAnimated(model, Transform{}, p)
				if _, err := g.end(false); err != nil {
					t.Fatal(err)
				}
			}
			if !nearf(apex().Y, 2) {
				t.Fatalf("drawn apex %v want y=2", apex())
			}
			// A held weight survives Advance while no clip animates it.
			p.Stop()
			p.Play("lift", false)
			p.SetTime(0)
			p.Stop()
			p.SetMorphWeights(0, []float32{0.25})
			p.Advance(0.1)
			if w := p.MorphWeights(0); !nearf(w[0], 0.25) {
				t.Fatalf("held weight %v", w)
			}
		})
	}
}
