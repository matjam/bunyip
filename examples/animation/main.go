// Command animation shows the anim package on 2D and 3D entities alike:
// keyframe clips drive sprite positions, sizes, rotations and tints and
// 3D transforms; a flipbook plays sprite-sheet frames; and buttons
// crossfade the hero cube between clips, with a Finished event sending
// it back to idle. Escape quits.
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
	g.yaw += float32(ctx.Delta) * 0.2
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 0.8, 0), g.yaw, 0.45, 9))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.5), Color: gfx.Color{R: 2.2, G: 2.1, B: 1.9, A: 1},
		Sky: gfx.Color{R: 0.25, G: 0.3, B: 0.45, A: 1}, Ground: gfx.Color{R: 0.1, G: 0.1, B: 0.08, A: 1}, Shadows: true, ShadowDistance: 25})
	gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(150, 150, 160), Roughness: 0.9}, lin.Translate(lin.V3(0, -0.6, 0)).Mul(lin.Scale(lin.V3(9, 0.2, 9))))
	g.meshes.Each(func(e ecs.Entity, t *gfx.Transform, m *mesh3D) {
		gr.DrawMeshAt(m.Mesh, m.Mat, *t)
	})
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
		u.Panel("Animation", ui.Rect{X: 12, Y: ctx.Height - 232, W: 300, H: 220}, func() {
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

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip animation", Width: 960, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "animation:", err)
		os.Exit(1)
	}
}
