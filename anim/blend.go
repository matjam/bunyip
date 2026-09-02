package anim

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// ClipWeight is a clip's share of a blended pose.
type ClipWeight struct {
	Clip   string
	Weight float32
}

// Blender turns parameters into clip weights: a blend space, a blend
// tree or a single clip. Weights appends the clips to play to out and
// returns it; the weights sum to 1 unless there is nothing to play.
type Blender interface {
	Weights(params map[string]float32, out []ClipWeight) []ClipWeight
}

// BlendSpace1D places clips along one parameter and blends the two on
// either side of its value: idle at 0, walk at 1, run at 2, with speed
// 1.5 half walk and half run. Outside the clips' range the nearest clip
// plays alone. It is plain data, buildable in code or from JSON.
type BlendSpace1D struct {
	// Parameter names the value the space reads, as set on a Blend.
	Parameter string `json:"parameter"`
	// Clips are the placed clips, in any order.
	Clips []BlendPoint1D `json:"clips"`
}

// BlendPoint1D is a clip placed at a parameter value.
type BlendPoint1D struct {
	Clip string  `json:"clip"`
	At   float32 `json:"at"`
}

// Weights blends the two clips around the parameter's value.
func (s *BlendSpace1D) Weights(params map[string]float32, out []ClipWeight) []ClipWeight {
	if len(s.Clips) == 0 {
		return out
	}
	lo, hi, t := neighbours(params[s.Parameter], len(s.Clips), func(i int) float32 { return s.Clips[i].At })
	start := len(out)
	out = addWeight(out, start, s.Clips[lo].Clip, 1-t)
	return addWeight(out, start, s.Clips[hi].Clip, t)
}

// neighbours finds the placed points on either side of v among n
// points, at(i) giving each one's position, and how far v is from lo
// to hi. Beyond the ends both are the end point.
func neighbours(v float32, n int, at func(int) float32) (lo, hi int, t float32) {
	lo, hi = -1, -1
	for i := range n {
		a := at(i)
		if a <= v && (lo < 0 || a > at(lo)) {
			lo = i
		}
		if a >= v && (hi < 0 || a < at(hi)) {
			hi = i
		}
	}
	switch {
	case lo < 0:
		lo = hi
	case hi < 0:
		hi = lo
	}
	if span := at(hi) - at(lo); span > 0 {
		t = (v - at(lo)) / span
	}
	return lo, hi, t
}

// addWeight adds weight to a clip's entry among those of out from start
// on, appending one when the clip is new there; nothing is added for a
// zero weight.
func addWeight(out []ClipWeight, start int, clip string, weight float32) []ClipWeight {
	if weight <= 0 {
		return out
	}
	if i := indexOf(out[start:], clip); i >= 0 {
		out[start+i].Weight += weight
		return out
	}
	return append(out, ClipWeight{Clip: clip, Weight: weight})
}

// indexOf finds a clip's entry, or -1.
func indexOf(ws []ClipWeight, clip string) int {
	for i := range ws {
		if ws[i].Clip == clip {
			return i
		}
	}
	return -1
}

// mergeInto scales the entries of out from start on and folds them into
// the ones before start, adding weights where a clip is already there.
func mergeInto(out []ClipWeight, start int, scale float32) []ClipWeight {
	n := start
	for i := start; i < len(out); i++ {
		w := out[i]
		w.Weight *= scale
		if j := indexOf(out[:n], w.Clip); j >= 0 {
			out[j].Weight += w.Weight
			continue
		}
		out[n] = w
		n++
	}
	return out[:n]
}

// BlendSpace2D places clips at points in a plane of two parameters and
// blends the ones around the current point: a strafe set with forward,
// back, left and right around an idle at the centre, read from the
// velocity's x and y. Weights are gradient bands: a clip at the current
// point plays alone, points on a line between two clips blend them
// linearly, and clips fall out as the point moves past them. It is
// plain data, buildable in code or from JSON.
type BlendSpace2D struct {
	// X and Y name the parameters the space reads, as set on a Blend.
	X string `json:"x"`
	Y string `json:"y"`
	// Clips are the placed clips, in any order.
	Clips []BlendPoint2D `json:"clips"`
}

// BlendPoint2D is a clip placed at a point in a 2D space.
type BlendPoint2D struct {
	Clip string   `json:"clip"`
	At   lin.Vec2 `json:"at"`
}

// Weights blends the clips around the point the two parameters make.
func (s *BlendSpace2D) Weights(params map[string]float32, out []ClipWeight) []ClipWeight {
	if len(s.Clips) == 0 {
		return out
	}
	p := lin.V2(params[s.X], params[s.Y])
	start := len(out)
	var total float32
	for i, ci := range s.Clips {
		// The clip's weight is its smallest gradient band against every
		// other clip: 1 at its own point, 0 at or beyond the other's.
		w := float32(1)
		for j, cj := range s.Clips {
			if i == j {
				continue
			}
			d := cj.At.Sub(ci.At)
			l2 := d.Dot(d)
			if l2 == 0 {
				continue
			}
			w = min(w, lin.Clamp(1-p.Sub(ci.At).Dot(d)/l2, 0, 1))
		}
		out = addWeight(out, start, ci.Clip, w)
	}
	for i := start; i < len(out); i++ {
		total += out[i].Weight
	}
	if total > 0 {
		for i := start; i < len(out); i++ {
			out[i].Weight /= total
		}
	}
	return out
}

// BlendTree is a node in a tree of blends. Exactly one part is used,
// checked in this order: a Clip plays alone, a Space1D or Space2D plays
// its blend, and Children are subtrees placed along Parameter and mixed
// like a 1D space, so a crouch amount can fade a standing locomotion
// space into a crouched one. The whole tree shares one phase, so the
// clips it mixes stay in step. It is plain data, buildable in code or
// from JSON.
type BlendTree struct {
	Clip      string        `json:"clip,omitempty"`
	Space1D   *BlendSpace1D `json:"space1d,omitempty"`
	Space2D   *BlendSpace2D `json:"space2d,omitempty"`
	Parameter string        `json:"parameter,omitempty"`
	Children  []BlendChild  `json:"children,omitempty"`
}

// BlendChild is a subtree placed at a parameter value in its parent.
type BlendChild struct {
	At   float32   `json:"at"`
	Tree BlendTree `json:"tree"`
}

// Weights evaluates the node for the parameters.
func (t *BlendTree) Weights(params map[string]float32, out []ClipWeight) []ClipWeight {
	return t.weigh(params, out, 1)
}

// weigh appends the node's clips scaled by scale, merging them with the
// clips already in out.
func (t *BlendTree) weigh(params map[string]float32, out []ClipWeight, scale float32) []ClipWeight {
	if scale <= 0 {
		return out
	}
	start := len(out)
	switch {
	case t.Clip != "":
		out = append(out, ClipWeight{Clip: t.Clip, Weight: 1})
	case t.Space1D != nil:
		out = t.Space1D.Weights(params, out)
	case t.Space2D != nil:
		out = t.Space2D.Weights(params, out)
	case len(t.Children) > 0:
		// The children merge their own clips in, already scaled.
		lo, hi, f := neighbours(params[t.Parameter], len(t.Children), func(i int) float32 { return t.Children[i].At })
		out = t.Children[lo].Tree.weigh(params, out, scale*(1-f))
		return t.Children[hi].Tree.weigh(params, out, scale*f)
	}
	return mergeInto(out, start, scale)
}

// Blend drives a gfx.AnimPlayer from a blend space or tree: it holds the
// parameters the game sets, evaluates the tree every Advance and keeps
// the mixed clips in step by playing them all at one phase of their own
// length, so a walk's and a run's feet land together. Clips in a blend
// loop. Make one with NewBlend; a Skeleton with a Blend set drives it
// from the ECS.
type Blend struct {
	// Tree is what is evaluated: a BlendSpace1D, BlendSpace2D or
	// BlendTree.
	Tree Blender
	// Params holds the parameter values by name; Set writes them.
	Params map[string]float32

	phase   float64
	weights []ClipWeight
	entries []gfx.AnimBlend
}

// NewBlend makes a Blend over a space or tree with every parameter at 0.
func NewBlend(tree Blender) *Blend {
	return &Blend{Tree: tree, Params: map[string]float32{}}
}

// Set gives a parameter a value.
func (b *Blend) Set(name string, v float32) {
	if b.Params == nil {
		b.Params = map[string]float32{}
	}
	b.Params[name] = v
}

// Get is a parameter's value; unset parameters are 0.
func (b *Blend) Get(name string) float32 { return b.Params[name] }

// Phase is how far through their cycle the blended clips are, 0 to 1.
func (b *Blend) Phase() float64 { return b.phase }

// SetPhase moves the blended clips to a point in their cycle, 0 to 1.
func (b *Blend) SetPhase(phase float64) { b.phase = wrapPhase(phase) }

// Weights evaluates the tree for the current parameters; the slice is
// reused by the next call.
func (b *Blend) Weights() []ClipWeight {
	b.weights = b.weights[:0]
	if b.Tree != nil {
		b.weights = b.Tree.Weights(b.Params, b.weights)
	}
	return b.weights
}

// Advance moves the blend on by dt seconds, scaled by the player's
// speed, sets the player's clips and advances the player. The phase
// moves at the blended cycle's rate: with walk at 1 s and run at 0.5 s
// mixed evenly, one cycle takes 0.75 s and each clip is sampled at the
// same fraction of its own length.
func (b *Blend) Advance(p *gfx.AnimPlayer, dt float64) {
	weights := b.Weights()
	model := p.Model()
	var cycle float64
	for _, w := range weights {
		cycle += float64(w.Weight) * float64(model.ClipDuration(w.Clip))
	}
	if cycle > 0 {
		b.phase = wrapPhase(b.phase + dt*p.Speed()/cycle)
	}
	b.entries = b.entries[:0]
	for _, w := range weights {
		b.entries = append(b.entries, gfx.AnimBlend{Clip: w.Clip, Weight: w.Weight, Time: b.phase * float64(model.ClipDuration(w.Clip))})
	}
	p.SetBlend(b.entries)
	p.Advance(dt)
}

// wrapPhase brings a phase into [0, 1).
func wrapPhase(phase float64) float64 {
	phase = math.Mod(phase, 1)
	if phase < 0 {
		phase++
	}
	return phase
}
