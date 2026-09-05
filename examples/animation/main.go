// Command animation shows the anim package on 2D and 3D entities alike:
// keyframe clips drive sprite positions, sizes, rotations and tints and
// 3D transforms; a flipbook plays sprite-sheet frames; buttons
// crossfade the hero cube between clips, with a Finished event sending
// it back to idle; and three robot arms from a generated glTF model show
// a skeletal clip with an animation event, two-bone IK reaching for a
// moving target, and a 1D blend space mixing a slow swing into a fast
// one by a slider. A sphere above them carries three morph targets
// blended in the vertex shader, driven by two sliders and a sine, which
// costs no upload however often the weights change. Escape quits.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/anim"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tween"
	"github.com/matjam/bunyip/ui"
)

// Components that say how to draw an entity.
type sprite2D struct{ Tex *gfx.Texture }
type mesh3D struct {
	Mesh *gfx.Mesh
	Mat  gfx.Material
}

type game struct {
	seconds float64
	shot    string

	font   *gfx.Font
	ui     *ui.Context
	world  *ecs.World
	dot    *gfx.Texture
	walker *gfx.Texture
	cube   *gfx.Mesh
	sphere *gfx.Mesh
	hero   ecs.Entity
	idle   *anim.Clip
	jump   *anim.Clip
	spin   *anim.Clip
	speed  float32
	log    []string
	yaw    float32

	// Three arms of one skeletal model: one swings a clip with an
	// event, one reaches for a target by IK, and one blends the swing
	// into a faster stride by a pace parameter.
	arms   *gfx.Model
	swing  *gfx.AnimPlayer
	reach  *gfx.AnimPlayer
	ikOn   bool
	target lin.Vec3 // the reaching arm's goal, relative to its base
	stride *gfx.AnimPlayer
	blend  *anim.Blend
	pace   float32

	// A sphere with three morph targets, driven straight from sliders and
	// a sine. Up to gfx.MaxGPUMorphTargets open at once blend in the
	// vertex shader, so changing them every frame uploads nothing.
	face  *gfx.Model
	faceW [3]float32

	sprites  *ecs.Query2[gfx.Sprite, sprite2D]
	meshes   *ecs.Query2[gfx.Transform, mesh3D]
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	if g.dot, err = ctx.Gfx.NewTexture(circle(32), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	if g.walker, err = ctx.Gfx.NewTexture(walkerSheet(), gfx.TextureOptions{}); err != nil {
		return err
	}
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(16, 32)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	g.speed = 1
	w := ecs.NewWorld()
	g.world = w
	g.sprites = ecs.NewQuery2[gfx.Sprite, sprite2D](w)
	g.meshes = ecs.NewQuery2[gfx.Transform, mesh3D](w)

	// 2D: dots that bounce, pulse and fade, each offset in time.
	bounce := anim.NewClip("bounce", anim.Loop,
		anim.Position2(anim.Vec2s(anim.At(0, lin.V2(0, 0)), anim.AtEased(0.6, lin.V2(0, -120), tween.OutQuad), anim.AtEased(1.2, lin.V2(0, 0), tween.OutBounce))),
		anim.Tint(anim.Colors(anim.At(0, gfx.RGB(255, 120, 80)), anim.At(0.6, gfx.RGB(255, 230, 120)), anim.At(1.2, gfx.RGB(255, 120, 80)))),
	)
	for i := range 6 {
		e := w.SpawnWith(gfx.Sprite{Size: lin.V2(40, 40), Color: gfx.White}, sprite2D{g.dot}, anim.Player{})
		p := anim.PlayerOf(w, e)
		p.Play(bounce)
		p.Time = float32(i) * 0.2
		ecs.Add(w, e, offset{lin.V2(60+float32(i)*60, 200)})
	}
	pulse := anim.NewClip("pulse", anim.PingPong,
		anim.Size2(anim.Vec2s(anim.At(0, lin.V2(30, 30)), anim.AtEased(0.8, lin.V2(90, 90), tween.InOutSine))),
		anim.Rotation2(anim.Floats(anim.Num(0, 0), anim.Num(0.8, math.Pi/2))),
	)
	e := w.SpawnWith(gfx.Sprite{Size: lin.V2(30, 30), Color: gfx.RGB(120, 200, 255), Origin: lin.V2(0.5, 0.5)}, sprite2D{g.dot}, anim.Player{}, offset{lin.V2(480, 200)})
	anim.PlayerOf(w, e).Play(pulse)

	// A flipbook walker from a generated four-frame sheet.
	sheet := gfx.NewSheet(g.walker, 16, 16)
	w.SpawnWith(gfx.Sprite{Size: lin.V2(64, 64), Color: gfx.White}, sprite2D{g.walker}, offset{lin.V2(560, 180)},
		anim.Flipbook{Sheet: sheet, Frames: []int{0, 1, 2, 3}, FPS: 8, Loop: true})

	// 3D: a ring of cubes orbiting and tumbling, and a hero cube with
	// clips to crossfade between.
	for i := range 8 {
		a := float32(i) / 8 * 2 * math.Pi
		orbit := anim.NewClip("orbit", anim.Loop,
			anim.Position(anim.Vec3s(
				anim.At(0, lin.V3(3*float32(math.Cos(float64(a))), 0, 3*float32(math.Sin(float64(a))))),
				anim.At(2, lin.V3(3*float32(math.Cos(float64(a)+math.Pi)), 1, 3*float32(math.Sin(float64(a)+math.Pi)))),
				anim.At(4, lin.V3(3*float32(math.Cos(float64(a))), 0, 3*float32(math.Sin(float64(a))))),
			)),
			anim.Rotation(anim.Quats(anim.At(0, lin.QuatIdentity()), anim.At(2, lin.AxisAngle(lin.V3(1, 1, 0).Norm(), math.Pi)), anim.At(4, lin.AxisAngle(lin.V3(1, 1, 0).Norm(), 2*math.Pi)))),
		)
		e := w.SpawnWith(gfx.Transform{Scale: lin.V3(0.4, 0.4, 0.4)}, mesh3D{g.cube, gfx.Material{BaseColor: gfx.RGB(uint8(120+15*i), 160, uint8(220-15*i)), Roughness: 0.4}}, anim.Player{})
		p := anim.PlayerOf(w, e)
		p.Play(orbit)
		p.Time = float32(i) * 0.5
	}
	g.idle = anim.NewClip("idle", anim.Loop,
		anim.Position(anim.Vec3s(anim.At(0, lin.V3(0, 0.5, 0)), anim.AtEased(1, lin.V3(0, 0.8, 0), tween.InOutSine), anim.AtEased(2, lin.V3(0, 0.5, 0), tween.InOutSine))),
		anim.Scale(anim.Vec3s(anim.At(0, lin.V3(1, 1, 1)), anim.At(1, lin.V3(1.05, 0.95, 1.05)), anim.At(2, lin.V3(1, 1, 1)))),
		anim.Rotation(anim.Quats(anim.At(0, lin.QuatIdentity()), anim.At(2, lin.QuatIdentity()))),
	)
	g.jump = anim.NewClip("jump", anim.Once,
		anim.Position(anim.Vec3s(anim.At(0, lin.V3(0, 0.5, 0)), anim.AtEased(0.4, lin.V3(0, 3, 0), tween.OutQuad), anim.AtEased(0.8, lin.V3(0, 0.5, 0), tween.InQuad))),
		anim.Scale(anim.Vec3s(anim.At(0, lin.V3(1.3, 0.7, 1.3)), anim.At(0.2, lin.V3(0.8, 1.4, 0.8)), anim.At(0.8, lin.V3(1.2, 0.8, 1.2)), anim.At(1, lin.V3(1, 1, 1)))),
	)
	g.spin = anim.NewClip("spin", anim.Once,
		anim.Rotation(anim.Quats(anim.At(0, lin.QuatIdentity()), anim.At(0.5, lin.AxisAngle(lin.V3(0, 1, 0), math.Pi)), anim.AtEased(1, lin.AxisAngle(lin.V3(0, 1, 0), 2*math.Pi), tween.OutBack))),
		anim.Position(anim.Vec3s(anim.At(0, lin.V3(0, 0.5, 0)), anim.At(1, lin.V3(0, 0.5, 0)))),
	)
	g.hero = w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, mesh3D{g.sphere, gfx.Material{BaseColor: gfx.RGB(255, 200, 90), Metallic: 0.6, Roughness: 0.3}}, anim.Player{})
	anim.PlayerOf(w, g.hero).Play(g.idle)

	// Skeletal: the arms come from a glTF document built in memory; a
	// file loads the same way through gltf.Load. The left arm plays the
	// swing clip and logs its "hit" event; the right arm's PostPose
	// solves two-bone IK towards an orbiting target.
	if g.arms, err = ctx.Gfx.LoadModel(armDocument()); err != nil {
		return err
	}
	g.arms.Parts[0].Material = gfx.Material{BaseColor: gfx.RGB(200, 90, 80), Roughness: 0.5}
	g.arms.Parts[1].Material = gfx.Material{BaseColor: gfx.RGB(240, 180, 90), Roughness: 0.5}
	g.swing = g.arms.NewAnimPlayer()
	g.swing.AddEvent("swing", 1, "hit")
	g.swing.OnEvent = func(e gfx.AnimEvent) { g.say(fmt.Sprintf("event %q at %.1fs of %s", e.Name, e.Time, e.Clip)) }
	g.swing.Play("swing", true)
	g.reach = g.arms.NewAnimPlayer()
	g.reach.Play("swing", true)
	g.ikOn = true
	shoulder, elbow, hand := g.arms.NodeIndex("shoulder"), g.arms.NodeIndex("elbow"), g.arms.NodeIndex("hand")
	g.reach.PostPose = func(p *gfx.AnimPlayer) {
		if g.ikOn {
			anim.SolveTwoBoneIK(p, shoulder, elbow, hand, g.target, lin.V3(0, 0.8, 2))
		}
	}
	// The third arm plays a 1D blend space: the two-second swing at pace
	// 0, the one-second stride at pace 1. In between, both clips run at
	// one shared phase, so the arm neither stutters nor doubles back.
	g.stride = g.arms.NewAnimPlayer()
	g.blend = anim.NewBlend(&anim.BlendSpace1D{Parameter: "pace", Clips: []anim.BlendPoint1D{
		{Clip: "swing", At: 0}, {Clip: "stride", At: 1},
	}})
	g.pace = 0.5

	// Morph targets: a sphere with three blend shapes, driven straight
	// from the sliders below and a sine. Three is well inside
	// gfx.MaxGPUMorphTargets, so the blend happens in the vertex shader
	// and moving a slider uploads nothing at all.
	if g.face, err = ctx.Gfx.LoadModel(faceDocument()); err != nil {
		return err
	}
	g.face.Parts[0].Material = gfx.Material{BaseColor: gfx.RGB(230, 180, 150), Roughness: 0.55}
	g.faceW = [3]float32{0.4, 0, 0.3}

	w.AddSystem("anim", anim.System)
	// When a one-shot clip finishes, fade the hero back to idle.
	w.AddSystem("return", func(w *ecs.World, dt float64) {
		for _, ev := range ecs.Events[anim.Finished](w) {
			if ev.Entity == g.hero {
				anim.PlayerOf(w, g.hero).CrossFade(g.idle, 0.3)
				g.say("finished " + ev.Clip.Name + ", back to idle")
			}
		}
	})
	return nil
}

// offset is a 2D entity's anchor; the clip's position is relative to it.
type offset struct{ At lin.Vec2 }

func (g *game) say(s string) {
	g.log = append(g.log, s)
	if len(g.log) > 5 {
		g.log = g.log[1:]
	}
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.face.Destroy()
	g.arms.Destroy()
	g.cube.Destroy()
	g.sphere.Destroy()
	g.dot.Destroy()
	g.walker.Destroy()
	g.font.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if g.seconds > 0 && ctx.Frame == 30 {
		anim.PlayerOf(g.world, g.hero).CrossFade(g.jump, 0.2) // something to see in a screenshot
	}
	ecs.Each(g.world, func(e ecs.Entity, p *anim.Player) { p.Speed = g.speed })
	g.world.Update(ctx.Delta)
	t := float32(ctx.Time)
	g.target = lin.V3(0.9*float32(math.Cos(float64(t)*1.3)), 0.9+0.5*float32(math.Sin(float64(t)*0.7)), 0.7*float32(math.Sin(float64(t)*1.3)))
	g.swing.Advance(ctx.Delta * float64(g.speed))
	g.reach.Advance(ctx.Delta * float64(g.speed))
	g.blend.Set("pace", g.pace)
	g.blend.Advance(g.stride, ctx.Delta*float64(g.speed))
	// The snout breathes on its own so a screenshot catches it moving.
	// New weights every update cost nothing: the shader blends them.
	g.faceW[2] = 0.3 + 0.3*float32(math.Sin(float64(t)*1.6))
	if err := g.face.SetMorphWeights(0, g.faceW[:]); err != nil {
		return err
	}
	g.yaw += float32(ctx.Delta) * 0.2
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 0.8, 0), g.yaw, 0.45, 9))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.5), Color: gfx.Color{R: 2.2, G: 2.1, B: 1.9, A: 1},
		Sky: gfx.Sky{Zenith: gfx.Color{R: 0.25, G: 0.3, B: 0.45, A: 1}, Ground: gfx.Color{R: 0.1, G: 0.1, B: 0.08, A: 1}}, Shadows: true, ShadowDistance: 25})
	gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(150, 150, 160), Roughness: 0.9}, lin.Translate(lin.V3(0, -0.6, 0)).Mul(lin.Scale(lin.V3(9, 0.2, 9))))
	g.meshes.Each(func(e ecs.Entity, t *gfx.Transform, m *mesh3D) {
		gr.DrawMeshAt(m.Mesh, m.Mat, *t)
	})
	gr.DrawModelAnimated(g.arms, gfx.At(-2.2, -0.5, 0), g.swing)
	gr.DrawModelAnimated(g.arms, gfx.At(2.2, -0.5, 0), g.reach)
	gr.DrawModelAnimated(g.arms, gfx.At(0, -0.5, -2.4), g.stride)
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(120, 220, 140), Emissive: 0.4},
		lin.Translate(g.target.Add(lin.V3(2.2, -0.5, 0))).Mul(lin.Scale(lin.V3(0.08, 0.08, 0.08))))
	// The morph target sphere, blended by the sliders every frame.
	gr.DrawModel(g.face, lin.Translate(lin.V3(0, 2.4, 0)).Mul(lin.Scale(lin.V3(0.7, 0.7, 0.7))))
	// 2D entities draw at their offset plus the animated position.
	gr.ScreenSpace()
	g.sprites.Each(func(e ecs.Entity, s *gfx.Sprite, d *sprite2D) {
		draw := *s
		if o, ok := ecs.Get[offset](w, e); ok {
			draw.Pos = draw.Pos.Add(o.At)
		}
		if draw.UV1 == (lin.Vec2{}) {
			draw.UV1 = lin.V2(1, 1)
		}
		gr.Draw(d.Tex, draw)
	})
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Animation", ui.Rect{X: 12, Y: ctx.Height - 392, W: 300, H: 380}, func() {
			u.Label("Hero clip: " + anim.PlayerOf(w, g.hero).Clip.Name)
			u.Row(3, func() {
				if u.Button("Idle") {
					anim.PlayerOf(w, g.hero).CrossFade(g.idle, 0.3)
				}
				if u.Button("Jump") {
					anim.PlayerOf(w, g.hero).CrossFade(g.jump, 0.15)
				}
				if u.Button("Spin") {
					anim.PlayerOf(w, g.hero).CrossFade(g.spin, 0.15)
				}
			})
			u.Slider("Speed", &g.speed, 0, 3)
			u.Slider("Back arm pace (swing to stride)", &g.pace, 0, 1)
			u.Checkbox("Right arm reaches by IK", &g.ikOn)
			// Three morph targets, blended in the vertex shader: the
			// sliders move every frame and upload nothing.
			names := g.face.MorphTargets(0)
			for i := range g.faceW[:2] {
				u.Slider("Morph "+names[i], &g.faceW[i], 0, 1)
			}
			for _, l := range g.log {
				u.Label(l)
			}
		})
	})
	return nil
}

func circle(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			a := math.Max(0, math.Min(1, r-d))
			img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(255 * a)})
		}
	}
	return img
}

// walkerSheet draws four 16×16 frames of a little figure whose legs
// alternate.
func walkerSheet() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 16))
	for f := range 4 {
		set := func(x, y int, c color.RGBA) { img.SetRGBA(f*16+x, y, c) }
		for y := 2; y < 7; y++ {
			for x := 5; x < 11; x++ {
				set(x, y, color.RGBA{250, 220, 180, 255})
			}
		}
		for y := 7; y < 12; y++ {
			for x := 4; x < 12; x++ {
				set(x, y, color.RGBA{80, 160, 220, 255})
			}
		}
		stride := []int{0, 1, 0, -1}[f]
		for y := 12; y < 16; y++ {
			for _, x := range []int{5 + stride, 6 + stride, 9 - stride, 10 - stride} {
				set(x, y, color.RGBA{40, 40, 90, 255})
			}
		}
	}
	return img
}

// faceDocument builds a sphere with three morph targets as a glTF
// document in memory: one pulls its crown into a point, one squashes it
// wide and one pushes a snout out of the front. A file's blend shapes
// arrive the same way through gltf.Load.
func faceDocument() *gltf.Document {
	sv, si := gfx.SphereMesh(16, 32)
	prim := gltf.Primitive{Indices: si, Material: -1}
	for _, v := range sv {
		prim.Positions = append(prim.Positions, v.Pos)
		prim.Normals = append(prim.Normals, v.Normal)
		prim.UVs = append(prim.UVs, v.UV)
	}
	// Each target is a delta per vertex, weighted by how much of the
	// shape it belongs to, so the three blend smoothly against each other.
	shape := func(delta func(p lin.Vec3) lin.Vec3) gltf.MorphTarget {
		t := gltf.MorphTarget{Positions: make([]lin.Vec3, len(sv)), Normals: make([]lin.Vec3, len(sv))}
		for i, v := range sv {
			t.Positions[i] = delta(v.Pos)
			// The normal follows the stretch: a rough approximation, which
			// is all a blend shape's normals ever are.
			t.Normals[i] = delta(v.Normal).Mul(0.5)
		}
		return t
	}
	prim.Targets = []gltf.MorphTarget{
		shape(func(p lin.Vec3) lin.Vec3 { return lin.V3(-p.X*0.6, max(p.Y, 0)*1.2, -p.Z*0.6) }),
		shape(func(p lin.Vec3) lin.Vec3 { return lin.V3(p.X*0.5, -p.Y*0.45, p.Z*0.5) }),
		shape(func(p lin.Vec3) lin.Vec3 { return lin.V3(0, 0, max(p.Z, 0)*0.9) }),
	}
	return &gltf.Document{
		Meshes: []gltf.Mesh{{Name: "face", TargetNames: []string{"point", "squash", "snout"},
			Primitives: []gltf.Primitive{prim}}},
		Nodes:     []gltf.Node{{Name: "face", Parent: -1, Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1), Mesh: 0, Skin: -1}},
		Instances: []gltf.Instance{{Name: "face", Mesh: 0, Node: 0, Skin: -1, World: lin.Identity()}},
	}
}

// armDocument builds a two-bone arm as a glTF document in memory: a box
// per bone, nodes shoulder, elbow and hand, a "swing" clip that rocks
// both joints over two seconds and a "stride" clip that rocks them
// wider in one. Straight up is the rest pose.
func armDocument() *gltf.Document {
	cv, ci := gfx.CubeMesh()
	prim := gltf.Primitive{Indices: ci, Material: -1}
	for _, v := range cv {
		prim.Positions = append(prim.Positions, lin.V3(v.Pos.X*0.18, (v.Pos.Y+0.5)*0.8, v.Pos.Z*0.18))
		prim.Normals = append(prim.Normals, v.Normal)
		prim.UVs = append(prim.UVs, v.UV)
	}
	id, one := lin.QuatIdentity(), lin.V3(1, 1, 1)
	doc := &gltf.Document{
		Meshes: []gltf.Mesh{{Name: "bone", Primitives: []gltf.Primitive{prim}}},
		Nodes: []gltf.Node{
			{Name: "shoulder", Parent: -1, Children: []int{1}, Rotation: id, Scale: one, Mesh: 0, Skin: -1},
			{Name: "elbow", Parent: 0, Children: []int{2}, Translation: lin.V3(0, 0.8, 0), Rotation: id, Scale: one, Mesh: 0, Skin: -1},
			{Name: "hand", Parent: 1, Translation: lin.V3(0, 0.8, 0), Rotation: id, Scale: one, Mesh: -1, Skin: -1},
		},
	}
	doc.Instances = []gltf.Instance{
		{Name: "shoulder", Mesh: 0, Node: 0, Skin: -1, World: doc.Nodes[0].Local()},
		{Name: "elbow", Mesh: 0, Node: 1, Skin: -1, World: doc.Nodes[0].Local().Mul(doc.Nodes[1].Local())},
	}
	aboutZ := func(deg float32) lin.Vec4 {
		q := lin.AxisAngle(lin.V3(0, 0, 1), lin.Radians(deg))
		return lin.V4(q.X, q.Y, q.Z, q.W)
	}
	doc.Animations = []gltf.Animation{
		{Name: "swing", Duration: 2, Channels: []gltf.Channel{
			{Node: 0, Path: gltf.PathRotation, Times: []float32{0, 1, 2}, Values: []lin.Vec4{aboutZ(-35), aboutZ(35), aboutZ(-35)}},
			{Node: 1, Path: gltf.PathRotation, Times: []float32{0, 1, 2}, Values: []lin.Vec4{aboutZ(20), aboutZ(-50), aboutZ(20)}},
		}},
		{Name: "stride", Duration: 1, Channels: []gltf.Channel{
			{Node: 0, Path: gltf.PathRotation, Times: []float32{0, 0.5, 1}, Values: []lin.Vec4{aboutZ(-70), aboutZ(60), aboutZ(-70)}},
			{Node: 1, Path: gltf.PathRotation, Times: []float32{0, 0.5, 1}, Values: []lin.Vec4{aboutZ(45), aboutZ(-90), aboutZ(45)}},
		}},
	}
	return doc
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	headless := flag.Bool("headless", false, "render without a window, for screenshots")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip animation", Width: 960, Height: 640, Resizable: true, Validation: true, Headless: *headless},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "animation:", err)
		os.Exit(1)
	}
}
