package anim

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

func weightOf(ws []ClipWeight, clip string) float32 {
	for _, w := range ws {
		if w.Clip == clip {
			return w.Weight
		}
	}
	return 0
}

func TestBlendSpace1DWeights(t *testing.T) {
	s := &BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"run", 2}, {"idle", 0}, {"walk", 1}}}
	for _, tc := range []struct {
		name  string
		speed float32
		want  map[string]float32
	}{
		{"below the range", -1, map[string]float32{"idle": 1}},
		{"on a clip", 1, map[string]float32{"walk": 1}},
		{"between two", 1.25, map[string]float32{"walk": 0.75, "run": 0.25}},
		{"above the range", 5, map[string]float32{"run": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := s.Weights(map[string]float32{"speed": tc.speed}, nil)
			if len(ws) != len(tc.want) {
				t.Fatalf("weights %v, want %v", ws, tc.want)
			}
			for clip, w := range tc.want {
				if !near(weightOf(ws, clip), w) {
					t.Fatalf("weights %v, want %v", ws, tc.want)
				}
			}
		})
	}
	if ws := (&BlendSpace1D{}).Weights(nil, nil); len(ws) != 0 {
		t.Fatalf("empty space gave %v", ws)
	}
}

func TestBlendSpace2DWeights(t *testing.T) {
	s := &BlendSpace2D{X: "x", Y: "y", Clips: []BlendPoint2D{
		{"idle", lin.V2(0, 0)}, {"forward", lin.V2(0, 1)}, {"back", lin.V2(0, -1)}, {"left", lin.V2(-1, 0)}, {"right", lin.V2(1, 0)},
	}}
	at := func(x, y float32) []ClipWeight { return s.Weights(map[string]float32{"x": x, "y": y}, nil) }
	for _, tc := range []struct {
		name string
		x, y float32
		want map[string]float32
	}{
		{"idle's own point", 0, 0, map[string]float32{"idle": 1}},
		{"forward's own point", 0, 1, map[string]float32{"forward": 1}},
		{"half way forward", 0, 0.5, map[string]float32{"idle": 0.5, "forward": 0.5}},
		{"past right", 3, 0, map[string]float32{"right": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := at(tc.x, tc.y)
			for clip, w := range tc.want {
				if !near(weightOf(ws, clip), w) {
					t.Fatalf("weights %v, want %v", ws, tc.want)
				}
			}
			var total float32
			for _, w := range ws {
				total += w.Weight
			}
			if !near(total, 1) {
				t.Fatalf("weights %v sum to %v", ws, total)
			}
		})
	}
	// Diagonally between forward and right, neither back nor left plays.
	ws := at(0.5, 0.5)
	if weightOf(ws, "back") != 0 || weightOf(ws, "left") != 0 || weightOf(ws, "forward") <= 0 || weightOf(ws, "right") <= 0 {
		t.Fatalf("diagonal weights %v", ws)
	}
}

func TestBlendTreeWeights(t *testing.T) {
	stand := &BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"idle", 0}, {"walk", 1}}}
	crouch := &BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"crouch", 0}, {"sneak", 1}}}
	tree := &BlendTree{Parameter: "crouch", Children: []BlendChild{{0, BlendTree{Space1D: stand}}, {1, BlendTree{Space1D: crouch}}}}
	ws := tree.Weights(map[string]float32{"speed": 0.5, "crouch": 0.25}, nil)
	want := map[string]float32{"idle": 0.375, "walk": 0.375, "crouch": 0.125, "sneak": 0.125}
	if len(ws) != len(want) {
		t.Fatalf("weights %v, want %v", ws, want)
	}
	for clip, w := range want {
		if !near(weightOf(ws, clip), w) {
			t.Fatalf("weights %v, want %v", ws, want)
		}
	}
	// A clip leaf plays alone, and a clip shared by two subtrees merges.
	leaf := &BlendTree{Parameter: "p", Children: []BlendChild{{0, BlendTree{Clip: "idle"}}, {1, BlendTree{Space1D: stand}}}}
	ws = leaf.Weights(map[string]float32{"p": 0.5, "speed": 0}, nil)
	if len(ws) != 1 || !near(ws[0].Weight, 1) || ws[0].Clip != "idle" {
		t.Fatalf("merged leaf weights %v", ws)
	}
}

func TestBlendJSON(t *testing.T) {
	tree := BlendTree{Parameter: "crouch", Children: []BlendChild{
		{At: 0, Tree: BlendTree{Space2D: &BlendSpace2D{X: "vx", Y: "vy", Clips: []BlendPoint2D{{"idle", lin.V2(0, 0)}, {"forward", lin.V2(0, 1)}}}}},
		{At: 1, Tree: BlendTree{Space1D: &BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"crouch", 0}, {"sneak", 1}}}}},
		{At: 2, Tree: BlendTree{Clip: "prone"}},
	}}
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	var back BlendTree
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tree, back) {
		t.Fatalf("round trip\n%s\ngave %+v", data, back)
	}
	var space BlendSpace1D
	if err := json.Unmarshal([]byte(`{"parameter":"speed","clips":[{"clip":"idle","at":0},{"clip":"run","at":2}]}`), &space); err != nil {
		t.Fatal(err)
	}
	if ws := space.Weights(map[string]float32{"speed": 1}, nil); !near(weightOf(ws, "idle"), 0.5) || !near(weightOf(ws, "run"), 0.5) {
		t.Fatalf("decoded space weights %v", ws)
	}
}

// strideDoc is a node with two clips that hold it at different places
// for different lengths: "short" at x=0 over 1 s and "long" at x=2 over
// 2 s. A gait's stride would differ the same way.
func strideDoc() *gltf.Document {
	id, one := lin.QuatIdentity(), lin.V3(1, 1, 1)
	hold := func(name string, x, d float32) gltf.Animation {
		return gltf.Animation{Name: name, Duration: d, Channels: []gltf.Channel{
			{Node: 0, Path: gltf.PathTranslation, Times: []float32{0, d}, Values: []lin.Vec4{lin.V4(x, 0, 0, 0), lin.V4(x, 0, 0, 0)}},
		}}
	}
	return &gltf.Document{
		Nodes:      []gltf.Node{{Name: "root", Parent: -1, Rotation: id, Scale: one, Mesh: -1, Skin: -1}},
		Animations: []gltf.Animation{hold("short", 0, 1), hold("long", 2, 2)},
	}
}

func TestBlendOnPlayer(t *testing.T) {
	g := headless(t)
	model, err := g.LoadModel(strideDoc())
	if err != nil {
		t.Fatal(err)
	}
	defer model.Destroy()
	p := model.NewAnimPlayer()
	b := NewBlend(&BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"short", 0}, {"long", 1}}})
	b.Set("speed", 0.5)
	// Half way between the clips, the translation is half way too.
	b.Advance(p, 0)
	if pos, _, _ := p.NodeLocal(0); !near(pos.X, 1) {
		t.Fatalf("blended translation %v, want x=1", pos)
	}
	// The blended cycle is 1.5 s, so 0.75 s is half a cycle of each clip:
	// 0.5 s into the short one and 1 s into the long one.
	b.Advance(p, 0.75)
	if !near(float32(b.Phase()), 0.5) {
		t.Fatalf("phase %v", b.Phase())
	}
	times := map[string]float64{}
	for _, e := range p.Blend() {
		times[e.Clip] = e.Time
	}
	if !near(float32(times["short"]), 0.5) || !near(float32(times["long"]), 1) {
		t.Fatalf("clip times %v", times)
	}
	// The phase wraps, and a clip alone at the end of the range plays
	// its own translation.
	b.Set("speed", 1)
	b.Advance(p, 1.5)
	if pos, _, _ := p.NodeLocal(0); !near(pos.X, 2) || !near(float32(b.Phase()), 0.25) {
		t.Fatalf("long alone: %v at phase %v", pos, b.Phase())
	}
	if bl := p.Blend(); len(bl) != 1 || bl[0].Clip != "long" {
		t.Fatalf("blend entries %v", bl)
	}
	// Play drops the blend.
	p.Play("short", true)
	if p.Blend() != nil || p.Clip() != "short" {
		t.Fatalf("after Play: blend %v clip %q", p.Blend(), p.Clip())
	}
	// The Skeleton component drives a blend from its parameters.
	w := ecs.NewWorld()
	w.AddSystem("anim", System)
	e := w.SpawnWith(gfx.Transform{}, Skeleton{Player: p, Blend: NewBlend(b.Tree)})
	s, _ := ecs.Get[Skeleton](w, e)
	s.SetParameter("speed", 0.5)
	w.Update(0.75)
	if pos, _, _ := p.NodeLocal(0); !near(pos.X, 1) || !near(s.Parameter("speed"), 0.5) || !near(float32(s.Blend.Phase()), 0.5) {
		t.Fatalf("skeleton blend: %v, speed %v, phase %v", pos, s.Parameter("speed"), s.Blend.Phase())
	}
}
