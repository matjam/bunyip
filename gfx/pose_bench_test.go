package gfx

import (
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// benchSkeleton is a 60-node tree (a spine with limbs hanging off it)
// with three one-second clips keyed at 30 frames per second on every
// node's translation, rotation and scale: the shape of a game character.
func benchSkeleton() *Model {
	const bones = 60
	id, one := lin.QuatIdentity(), lin.V3(1, 1, 1)
	m := &Model{}
	for i := range bones {
		parent := -1
		if i > 0 {
			parent = (i - 1) / 2 // a binary tree about six levels deep
		}
		m.nodes = append(m.nodes, gltf.Node{Name: fmt.Sprintf("b%d", i), Parent: parent,
			Translation: lin.V3(0, 0.1, 0), Rotation: id, Scale: one, Mesh: -1, Skin: -1})
	}
	for i := range m.nodes {
		if p := m.nodes[i].Parent; p >= 0 {
			m.nodes[p].Children = append(m.nodes[p].Children, i)
		}
	}
	for c, name := range []string{"idle", "walk", "run"} {
		clip := gltf.Animation{Name: name, Duration: 1}
		for n := range bones {
			times := make([]float32, 31)
			tv := make([]lin.Vec4, 31)
			rv := make([]lin.Vec4, 31)
			sv := make([]lin.Vec4, 31)
			for k := range times {
				t := float32(k) / 30
				times[k] = t
				a := float64(t)*2*math.Pi + float64(n+c)
				tv[k] = lin.V4(0, 0.1+0.01*float32(math.Sin(a)), 0, 0)
				q := lin.AxisAngle(lin.V3(1, 0, 0), 0.3*float32(math.Sin(a)))
				rv[k] = lin.V4(q.X, q.Y, q.Z, q.W)
				sv[k] = lin.V4(1, 1, 1, 0)
			}
			clip.Channels = append(clip.Channels,
				gltf.Channel{Node: n, Path: gltf.PathTranslation, Times: times, Values: tv},
				gltf.Channel{Node: n, Path: gltf.PathRotation, Times: times, Values: rv},
				gltf.Channel{Node: n, Path: gltf.PathScale, Times: times, Values: sv})
		}
		m.clips = append(m.clips, clip)
	}
	m.order = topoOrder(m.nodes)
	return m
}

// BenchmarkPose200 advances 200 players of a 60-bone skeleton, one frame
// of a crowd, in the ways a game drives them.
func BenchmarkPose200(b *testing.B) {
	m := benchSkeleton()
	cases := []struct {
		name  string
		setup func(p *AnimPlayer)
		frame func(p *AnimPlayer, i int)
	}{
		{"clip", func(p *AnimPlayer) { p.Play("walk", true) }, nil},
		{"crossfade", func(p *AnimPlayer) { p.Play("walk", true); p.CrossFade("run", true, 1e9) }, nil},
		{"rootmotion", func(p *AnimPlayer) { p.Play("walk", true); p.SetRootMotion("b0") }, nil},
		{"layer", func(p *AnimPlayer) {
			p.Play("walk", true)
			mask := make(AnimMask, 60)
			for i := 30; i < 60; i++ {
				mask[i] = true
			}
			l := p.Layer("idle", 0.5, mask)
			l.Loop = true
		}, nil},
		// The blend space path: anim.Blend calls SetBlend then Advance
		// every update.
		{"blend3", func(p *AnimPlayer) {}, func(p *AnimPlayer, i int) {
			t := float64(i%60) / 60
			p.SetBlend([]AnimBlend{{Clip: "idle", Weight: 0.2, Time: t}, {Clip: "walk", Weight: 0.5, Time: t}, {Clip: "run", Weight: 0.3, Time: t}})
		}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			players := make([]*AnimPlayer, 200)
			for i := range players {
				players[i] = m.NewAnimPlayer()
				c.setup(players[i])
			}
			var joints []lin.Mat4
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				for _, p := range players {
					if c.frame != nil {
						c.frame(p, i)
					}
					p.Advance(1.0 / 60)
					// The draw's per-character matrix read.
					joints = joints[:0]
					for n := range 60 {
						joints = append(joints, p.NodeMatrix(n))
					}
				}
			}
		})
	}
}
