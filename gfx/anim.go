package gfx

import (
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// AnimPlayer plays a model's animation clips over its node hierarchy. One
// player per animated instance; it holds the current pose.
type AnimPlayer struct {
	model *Model
	clip  int
	time  float64
	loop  bool
	speed float64
	pose  []lin.Mat4 // world matrix per node
	local []nodeTRS
}

type nodeTRS struct {
	t lin.Vec3
	r lin.Quat
	s lin.Vec3
}

// NewAnimPlayer makes a player for the model in its rest pose.
func (m *Model) NewAnimPlayer() *AnimPlayer {
	p := &AnimPlayer{model: m, clip: -1, speed: 1}
	p.local = make([]nodeTRS, len(m.nodes))
	p.pose = make([]lin.Mat4, len(m.nodes))
	p.rest()
	p.evaluate()
	return p
}

// Clips lists the model's animation names.
func (m *Model) Clips() []string {
	names := make([]string, len(m.clips))
	for i, c := range m.clips {
		names[i] = c.Name
	}
	return names
}

// Play starts a clip by name; unknown names return false.
func (p *AnimPlayer) Play(name string, loop bool) bool {
	for i, c := range p.model.clips {
		if c.Name == name {
			p.clip, p.time, p.loop = i, 0, loop
			p.Advance(0)
			return true
		}
	}
	return false
}

// PlayIndex starts a clip by index.
func (p *AnimPlayer) PlayIndex(i int, loop bool) {
	if i >= 0 && i < len(p.model.clips) {
		p.clip, p.time, p.loop = i, 0, loop
		p.Advance(0)
	}
}

// SetSpeed scales playback; 1 is normal.
func (p *AnimPlayer) SetSpeed(s float64) { p.speed = s }

// Time is the current clip time in seconds.
func (p *AnimPlayer) Time() float64 { return p.time }

// Advance moves the clip forward by dt seconds and updates the pose.
func (p *AnimPlayer) Advance(dt float64) {
	if p.clip < 0 {
		return
	}
	clip := p.model.clips[p.clip]
	p.time += dt * p.speed
	d := float64(clip.Duration)
	if d > 0 {
		if p.loop {
			for p.time >= d {
				p.time -= d
			}
			for p.time < 0 {
				p.time += d
			}
		} else {
			p.time = max(0, min(p.time, d))
		}
	}
	p.rest()
	for _, ch := range clip.Channels {
		if ch.Node < 0 || ch.Node >= len(p.local) || len(ch.Times) == 0 {
			continue
		}
		v := sampleChannel(ch, float32(p.time))
		n := &p.local[ch.Node]
		switch ch.Path {
		case gltf.PathTranslation:
			n.t = v.Vec3()
		case gltf.PathRotation:
			n.r = lin.Quat{X: v.X, Y: v.Y, Z: v.Z, W: v.W}.Norm()
		case gltf.PathScale:
			n.s = v.Vec3()
		}
	}
	p.evaluate()
}

func (p *AnimPlayer) rest() {
	for i, n := range p.model.nodes {
		p.local[i] = nodeTRS{n.Translation, n.Rotation, n.Scale}
	}
}

// evaluate composes world matrices parent-first; nodes are visited in
// index order after a topological pass computed at load.
func (p *AnimPlayer) evaluate() {
	for _, i := range p.model.order {
		n := p.model.nodes[i]
		local := lin.TRS(p.local[i].t, p.local[i].r, p.local[i].s)
		if n.Parent >= 0 {
			p.pose[i] = p.pose[n.Parent].Mul(local)
		} else {
			p.pose[i] = local
		}
	}
}

// NodeMatrix returns a node's current world matrix (in model space).
func (p *AnimPlayer) NodeMatrix(node int) lin.Mat4 {
	if node < 0 || node >= len(p.pose) {
		return lin.Identity()
	}
	return p.pose[node]
}

// jointMatrices returns the skin's joint matrices for the current pose.
func (p *AnimPlayer) jointMatrices(skin int, out []lin.Mat4) []lin.Mat4 {
	if skin < 0 || skin >= len(p.model.skins) {
		return out
	}
	s := p.model.skins[skin]
	for i, j := range s.Joints {
		out = append(out, p.pose[j].Mul(s.InverseBind[i]))
	}
	return out
}

// sampleChannel interpolates a channel at time t.
func sampleChannel(ch gltf.Channel, t float32) lin.Vec4 {
	times := ch.Times
	if t <= times[0] {
		return ch.Values[0]
	}
	last := len(times) - 1
	if t >= times[last] {
		return ch.Values[last]
	}
	// Binary search for the keyframe pair.
	lo, hi := 0, last
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if ch.Step {
		return ch.Values[lo]
	}
	span := times[hi] - times[lo]
	f := float32(0)
	if span > 0 {
		f = (t - times[lo]) / span
	}
	a, b := ch.Values[lo], ch.Values[hi]
	if ch.Path == gltf.PathRotation {
		q := lin.Quat{X: a.X, Y: a.Y, Z: a.Z, W: a.W}.Slerp(lin.Quat{X: b.X, Y: b.Y, Z: b.Z, W: b.W}, f)
		return lin.V4(q.X, q.Y, q.Z, q.W)
	}
	return a.Add(b.Sub(a).Mul(f))
}

// DrawModelAnimated draws a model under a transform with the player's
// pose: node-animated parts move rigidly and skinned parts deform.
func (g *Graphics) DrawModelAnimated(m *Model, t Transform, p *AnimPlayer) {
	world := t.Matrix()
	var joints []lin.Mat4
	for _, part := range m.Parts {
		if part.Mesh.skinned && part.skin >= 0 {
			joints = p.jointMatrices(part.skin, joints[:0])
			g.DrawSkinned(part.Mesh, part.Material, world, joints)
			continue
		}
		g.DrawMesh(part.Mesh, part.Material, world.Mul(p.NodeMatrix(part.node)))
	}
}
