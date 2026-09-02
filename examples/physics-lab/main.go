// Command physics-lab shows the newer parts of the phys package in one
// scene: capsules, convex hulls, spheres and boxes tumbling onto a
// heightfield terrain mesh, a chain of hinge joints with a ball on the
// end, a paddle wheel turned by a hinge motor, a ragdoll dropped from
// the sky, and a character controller walking back and forth over a
// staircase on a timer. Every collider is drawn as a wire outline on top
// of the scene; sleeping bodies turn grey and contacts draw their
// normals. Drag to orbit, scroll to zoom, R drops the bodies again,
// Escape quits.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/ui"
)

// terrainSize is the half extent of the heightfield; terrainCells its
// resolution along each side.
const (
	terrainSize  = 20
	terrainCells = 40
)

// terrainHeight is the rolling ground the bodies land on.
func terrainHeight(x, z float32) float32 {
	return 1.2*float32(math.Sin(float64(x)*0.35)*math.Cos(float64(z)*0.3)) + 0.4*float32(math.Sin(float64(x+z)*0.9))
}

// heightfield builds the terrain as render vertices plus the positions
// and indices the physics mesh shares.
func heightfield() ([]gfx.Vertex, []lin.Vec3, []uint32) {
	n := terrainCells + 1
	verts := make([]gfx.Vertex, 0, n*n)
	pts := make([]lin.Vec3, 0, n*n)
	for j := range n {
		for i := range n {
			x := -terrainSize + 2*terrainSize*float32(i)/terrainCells
			z := -terrainSize + 2*terrainSize*float32(j)/terrainCells
			y := terrainHeight(x, z)
			const d = 0.1
			nx := terrainHeight(x-d, z) - terrainHeight(x+d, z)
			nz := terrainHeight(x, z-d) - terrainHeight(x, z+d)
			normal := lin.V3(nx, 2*d, nz).Norm()
			p := lin.V3(x, y, z)
			verts = append(verts, gfx.Vertex{Pos: p, Normal: normal, UV: lin.V2(float32(i)/4, float32(j)/4)})
			pts = append(pts, p)
		}
	}
	var idx []uint32
	for j := range terrainCells {
		for i := range terrainCells {
			a := uint32(j*n + i)
			b := a + uint32(n)
			idx = append(idx, a, b, a+1, a+1, b, b+1)
		}
	}
	return verts, pts, idx
}

// octahedron is a convex hull with six points.
func octahedron(r float32) phys.ConvexHull {
	return phys.ConvexHull{Points: []lin.Vec3{{X: r}, {X: -r}, {Y: r}, {Y: -r}, {Z: r}, {Z: -r}}}
}

// wedge is a convex hull shaped like a doorstop.
func wedge(w, h, d float32) phys.ConvexHull {
	return phys.ConvexHull{Points: []lin.Vec3{
		{X: -w, Y: -h, Z: -d}, {X: w, Y: -h, Z: -d}, {X: w, Y: -h, Z: d}, {X: -w, Y: -h, Z: d},
		{X: -w, Y: h, Z: -d}, {X: -w, Y: h, Z: d},
	}}
}

// debris marks a body dropped by the lab, with its colour.
type debris struct{ Color gfx.Color }

// link marks one link of the hinge chain.
type link struct{}

// paddle marks the motorised wheel.
type paddle struct{}

type game struct {
	seconds float64
	shot    string
	ragdoll bool

	font      *gfx.Font
	ui        *ui.Context
	world     *ecs.World
	bodies    *ecs.Query3[gfx.Transform, phys.Body3, phys.Collider3]
	debris    *ecs.Query2[gfx.Transform, debris]
	terrain   *gfx.Mesh
	cube      *gfx.Mesh
	sphere    *gfx.Mesh
	cylinder  *gfx.Mesh
	doll      *phys.Ragdoll3
	wheel     ecs.Entity
	random    *rng.Rand
	hero      ecs.Entity
	ctrl      phys.CharacterController3
	heroDir   float32
	heroTimer float64
	contacts  []phys.Collision3
	yaw       float32
	pitch     float32
	dist      float32
	lastX     float32
	lastY     float32
	dragging  bool
	shotDone  bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(12, 18)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	yv, yi := gfx.CylinderMesh(18)
	if g.cylinder, err = ctx.Gfx.NewMesh(yv, yi); err != nil {
		return err
	}
	tv, pts, idx := heightfield()
	if g.terrain, err = ctx.Gfx.NewMesh(tv, idx); err != nil {
		return err
	}
	g.random = rng.New(5)
	g.yaw, g.pitch, g.dist = 0.6, 0.45, 34
	g.heroDir = 1

	w := ecs.NewWorld()
	g.world = w
	g.bodies = ecs.NewQuery3[gfx.Transform, phys.Body3, phys.Collider3](w)
	g.debris = ecs.NewQuery2[gfx.Transform, debris](w)
	ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0), Substeps: 4, Iterations: 8, SleepTime: 0.5})
	// The terrain is a static mesh collider sharing the render mesh's data.
	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.NewMeshShape(pts, idx)})
	// A staircase for the character, on a flat slab so the steps line up.
	w.SpawnWith(gfx.At(8, 1.5, -8), phys.Collider3{Shape: phys.Box3{Half: lin.V3(8, 0.5, 3)}})
	for i := range 5 {
		w.SpawnWith(gfx.At(6+float32(i)*1.2, 2+0.15+0.3*float32(i), -8), phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.6, 0.15+0.3*float32(i), 3)}})
	}
	g.ctrl = phys.CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.45, MaxSlope: 50}
	g.hero = w.SpawnWith(gfx.At(1, 3.5, -8))
	// A chain of hinged links hanging from a point in the sky with a ball on the end.
	prev := ecs.None
	anchor := lin.V3(-9, 12, 4)
	for i := range 7 {
		body := phys.Dynamic3(1)
		body.LinearDamping, body.AngularDamping = 0.5, 0.5
		e := w.SpawnWith(gfx.At(-9+0.5+float32(i), 12, 4), body,
			phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.5, 0.12, 0.12)}, Layers: phys.Layers{Layer: 2, Mask: 1}}, link{})
		w.SpawnWith(phys.HingeJoint3{A: prev, AnchorA: anchor, B: e, AnchorB: lin.V3(-0.5, 0, 0), AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1)})
		anchor = lin.V3(0.5, 0, 0)
		prev = e
	}
	ball := phys.Dynamic3(3)
	ball.LinearDamping = 0.2
	b := w.SpawnWith(gfx.At(-1, 12, 4), ball, phys.Collider3{Shape: phys.Sphere{Radius: 0.6}, Layers: phys.Layers{Layer: 2, Mask: 1}}, link{})
	w.SpawnWith(phys.HingeJoint3{A: prev, AnchorA: lin.V3(0.5, 0, 0), B: b, AnchorB: lin.V3(-0.6, 0, 0), AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1)})
	// A paddle wheel on a world hinge, turned by the hinge's motor; it
	// bats the debris that lands in it.
	wheelAt := lin.V3(-2, 2.4, 8)
	wheel := phys.Dynamic3(8)
	wheel.AngularDamping = 0.2
	g.wheel = w.SpawnWith(gfx.Transform{Position: wheelAt}, wheel, phys.Collider3{Shape: phys.Compound3{Parts: []phys.Part3{
		{Shape: phys.Box3{Half: lin.V3(1.6, 0.12, 0.5)}},
		{Shape: phys.Box3{Half: lin.V3(0.12, 1.6, 0.5)}},
	}}}, paddle{})
	w.SpawnWith(phys.HingeJoint3{A: ecs.None, AnchorA: wheelAt, B: g.wheel, AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1), MotorSpeed: 1.5, MaxMotorTorque: 400})
	w.AddSystem("physics", phys.System3)
	g.drop()
	return nil
}

// drop respawns the tumbling bodies above the terrain, and a ragdoll
// twice life size in front of the camera, tipped over so it lands in a
// heap.
func (g *game) drop() {
	w := g.world
	g.debris.Each(func(e ecs.Entity, _ *gfx.Transform, _ *debris) { w.Despawn(e) })
	if g.doll != nil {
		g.doll.Despawn(w)
		g.doll = nil
	}
	if g.ragdoll {
		tilt := lin.AxisAngle(lin.V3(1, 0, 0), 0.7).Mul(lin.AxisAngle(lin.V3(0, 0, 1), 0.4))
		g.doll = phys.NewRagdoll3(w, phys.RagdollSpec{Position: lin.V3(5, 6, 10), Rotation: tilt, Height: 3.6})
	}
	palette := []gfx.Color{gfx.RGB(240, 140, 30), gfx.RGB(60, 190, 80), gfx.RGB(50, 110, 240), gfx.RGB(240, 100, 180), gfx.RGB(240, 220, 60)}
	for i := range 48 {
		x := g.random.Between(-12, 4)
		z := g.random.Between(-4, 12)
		y := 6 + float32(i%6)*2 + g.random.Between(0, 1)
		body := phys.Dynamic3(1)
		body.Friction, body.Restitution = 0.6, 0.1
		body.AngVel = lin.V3(g.random.Between(-2, 2), g.random.Between(-2, 2), g.random.Between(-2, 2))
		var shape phys.Shape3
		switch i % 5 {
		case 0:
			shape = phys.Capsule{Radius: 0.3, HalfHeight: 0.45}
		case 1:
			shape = octahedron(0.7)
		case 2:
			shape = wedge(0.6, 0.35, 0.5)
		case 3:
			shape = phys.Box3{Half: lin.V3(0.4, 0.4, 0.4)}
		default:
			shape = phys.Sphere{Radius: 0.4}
			body.CCD = true
		}
		w.SpawnWith(
			gfx.Transform{Position: lin.V3(x, y, z), Rotation: lin.AxisAngle(lin.V3(g.random.Float(), g.random.Float(), g.random.Float()).Norm(), g.random.Float()*3)},
			body, phys.Collider3{Shape: shape}, debris{Color: palette[i%len(palette)]},
		)
	}
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.terrain.Destroy()
	g.cube.Destroy()
	g.sphere.Destroy()
	g.cylinder.Destroy()
	g.font.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if in.KeyPressed(input.KeyR) {
		g.drop()
	}
	x, y := in.Mouse()
	if in.MousePressed(input.MouseLeft) && !g.ui.WantsMouse() {
		g.dragging = true
	}
	if in.MouseReleased(input.MouseLeft) {
		g.dragging = false
	}
	if g.dragging {
		g.yaw += (x - g.lastX) * 0.01
		g.pitch = lin.Clamp(g.pitch+(y-g.lastY)*0.01, 0.05, 1.5)
	}
	g.lastX, g.lastY = x, y
	_, dy := in.Scroll()
	g.dist = lin.Clamp(g.dist-float32(dy)*2, 8, 120)
	// The character walks the staircase one way, then the other, on a timer.
	g.heroTimer += ctx.Delta
	if g.heroTimer > 5 {
		g.heroTimer, g.heroDir = 0, -g.heroDir
	}
	step := ctx.Profile("physics")
	g.ctrl.Move(g.world, g.hero, lin.V3(2.5*g.heroDir, -6, 0), float32(ctx.Delta))
	g.world.Update(ctx.Delta)
	step.End()
	g.contacts = append(g.contacts[:0], ecs.Events[phys.Collision3](g.world)...)
	return nil
}

// wireShape outlines a collider in place.
func wireShape(gr *gfx.Graphics, s phys.Shape3, t gfx.Transform, c gfx.Color) {
	rot := t.Rotation
	if rot == (lin.Quat{}) {
		rot = lin.QuatIdentity()
	}
	switch sh := s.(type) {
	case phys.Sphere:
		gr.DrawWireSphere(t.Position, sh.Radius, c)
	case phys.Box3:
		gr.DrawWireCube(lin.TRS(t.Position, rot, sh.Half.Mul(2)), c)
	case phys.Capsule:
		up := rot.Rotate(lin.V3(0, sh.HalfHeight, 0))
		a, b := t.Position.Sub(up), t.Position.Add(up)
		gr.DrawWireSphere(a, sh.Radius, c)
		gr.DrawWireSphere(b, sh.Radius, c)
		for _, d := range []lin.Vec3{rot.Rotate(lin.V3(sh.Radius, 0, 0)), rot.Rotate(lin.V3(0, 0, sh.Radius))} {
			gr.DrawLine3D(a.Add(d), b.Add(d), c)
			gr.DrawLine3D(a.Sub(d), b.Sub(d), c)
		}
	case phys.ConvexHull:
		// Every pair of points: a dense outline that shows the volume.
		pts := make([]lin.Vec3, len(sh.Points))
		for i, p := range sh.Points {
			pts[i] = t.Position.Add(rot.Rotate(p))
		}
		for i := range pts {
			for j := i + 1; j < len(pts); j++ {
				gr.DrawLine3D(pts[i], pts[j], c)
			}
		}
	case phys.Compound3:
		for _, p := range sh.Parts {
			pt := gfx.Transform{Position: t.Position.Add(rot.Rotate(p.Offset)), Rotation: rot.Mul(p.Rotation)}
			if p.Rotation == (lin.Quat{}) {
				pt.Rotation = rot
			}
			wireShape(gr, p.Shape, pt, c)
		}
	}
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 2, 0), g.yaw, g.pitch, g.dist))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.5, -1, -0.3), Color: gfx.Color{R: 2.4, G: 2.3, B: 2.1, A: 1},
		Sky: gfx.Sky{Zenith: gfx.Color{R: 0.3, G: 0.35, B: 0.5, A: 1}, Ground: gfx.Color{R: 0.15, G: 0.12, B: 0.1, A: 1}}, Shadows: true, ShadowDistance: 60})
	gr.DrawMesh(g.terrain, gfx.Material{BaseColor: gfx.RGB(110, 140, 90), Roughness: 0.9}, lin.Identity())
	// Solid meshes for the debris, chain and stairs; wire outlines over all colliders.
	g.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *phys.Body3, c *phys.Collider3) {
		col := gfx.RGB(255, 170, 40)
		if b.Asleep() {
			col = gfx.RGB(150, 150, 160)
		}
		if d, ok := ecs.Get[debris](w, e); ok {
			mat := gfx.Material{BaseColor: d.Color, Roughness: 0.6}
			switch sh := c.Shape.(type) {
			case phys.Sphere:
				gr.DrawMesh(g.sphere, mat, lin.TRS(t.Position, t.Rotation, lin.V3(sh.Radius, sh.Radius, sh.Radius)))
			case phys.Box3:
				gr.DrawMesh(g.cube, mat, lin.TRS(t.Position, t.Rotation, sh.Half.Mul(2)))
			}
		}
		if _, ok := ecs.Get[link](w, e); ok {
			if sh, ok := c.Shape.(phys.Box3); ok {
				gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(200, 200, 210), Metallic: 1, Roughness: 0.3}, lin.TRS(t.Position, t.Rotation, sh.Half.Mul(2)))
			}
			if sh, ok := c.Shape.(phys.Sphere); ok {
				gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(220, 60, 60), Roughness: 0.4}, lin.TRS(t.Position, t.Rotation, lin.V3(sh.Radius, sh.Radius, sh.Radius)))
			}
		}
		if _, ok := ecs.Get[paddle](w, e); ok {
			if sh, ok := c.Shape.(phys.Compound3); ok {
				for _, p := range sh.Parts {
					if box, ok := p.Shape.(phys.Box3); ok {
						gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(180, 120, 60), Roughness: 0.7}, lin.TRS(t.Position.Add(t.Rotation.Rotate(p.Offset)), t.Rotation, box.Half.Mul(2)))
					}
				}
			}
		}
		wireShape(gr, c.Shape, *t, col)
	})
	// The ragdoll's capsules as a cylinder with a sphere at each end.
	if g.doll != nil {
		skin := gfx.Material{BaseColor: gfx.RGB(230, 180, 140), Roughness: 0.8}
		for _, e := range g.doll.Entities() {
			t, ok := ecs.Get[gfx.Transform](w, e)
			if !ok {
				continue
			}
			c, ok := ecs.Get[phys.Collider3](w, e)
			if !ok {
				continue
			}
			if cap, ok := c.Shape.(phys.Capsule); ok {
				rot := t.Rotation
				if rot == (lin.Quat{}) {
					rot = lin.QuatIdentity()
				}
				r := lin.V3(cap.Radius, cap.Radius, cap.Radius)
				up := rot.Rotate(lin.V3(0, cap.HalfHeight, 0))
				gr.DrawMesh(g.sphere, skin, lin.TRS(t.Position.Add(up), rot, r))
				gr.DrawMesh(g.sphere, skin, lin.TRS(t.Position.Sub(up), rot, r))
				if cap.HalfHeight > 0 {
					gr.DrawMesh(g.cylinder, skin, lin.TRS(t.Position, rot, lin.V3(cap.Radius, cap.HalfHeight, cap.Radius)))
				}
			}
		}
	}
	ecs.Each2(w, func(e ecs.Entity, t *gfx.Transform, c *phys.Collider3) {
		if ecs.Has[phys.Body3](w, e) {
			return
		}
		if sh, ok := c.Shape.(phys.Box3); ok {
			gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(120, 120, 130), Roughness: 0.9}, lin.TRS(t.Position, t.Rotation, sh.Half.Mul(2)))
			wireShape(gr, c.Shape, *t, gfx.RGB(90, 200, 255))
		}
	})
	// Joint anchors as short lines between the bodies they join.
	drawJoint := func(ea, eb ecs.Entity, anchorA, anchorB lin.Vec3) {
		var a lin.Vec3
		if ea == ecs.None {
			a = anchorA
		} else if ta, ok := ecs.Get[gfx.Transform](w, ea); ok {
			a = ta.Position.Add(ta.Rotation.Rotate(anchorA))
		}
		if tb, ok := ecs.Get[gfx.Transform](w, eb); ok {
			b := tb.Position.Add(tb.Rotation.Rotate(anchorB))
			gr.DrawLine3D(a, b, gfx.RGB(255, 255, 255))
			gr.DrawWireSphere(b, 0.08, gfx.RGB(255, 80, 80))
		}
	}
	ecs.Each(w, func(e ecs.Entity, j *phys.HingeJoint3) { drawJoint(j.A, j.B, j.AnchorA, j.AnchorB) })
	ecs.Each(w, func(e ecs.Entity, j *phys.BallJoint3) { drawJoint(j.A, j.B, j.AnchorA, j.AnchorB) })
	// The character: a green capsule with its ground normal.
	if ht, ok := ecs.Get[gfx.Transform](w, g.hero); ok {
		col := gfx.RGB(80, 255, 120)
		if !g.ctrl.Grounded {
			col = gfx.RGB(255, 80, 80)
		}
		wireShape(gr, phys.Capsule{Radius: g.ctrl.Radius, HalfHeight: g.ctrl.HalfHeight}, *ht, col)
		if g.ctrl.Grounded {
			foot := ht.Position.Sub(lin.V3(0, g.ctrl.HalfHeight+g.ctrl.Radius, 0))
			gr.DrawLine3D(foot, foot.Add(g.ctrl.GroundNormal), gfx.RGB(255, 255, 255))
		}
	}
	// Contacts from this frame's collision events.
	for _, c := range g.contacts {
		gr.DrawLine3D(c.Point, c.Point.Add(c.Normal.Mul(0.5)), gfx.RGB(255, 60, 60))
	}
	gr.DrawAxes(lin.Identity(), 2)
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("physics lab", ui.Rect{X: 12, Y: ctx.Height - 150, W: 380, H: 138}, func() {
			ms := 0.0
			if len(ctx.Stats.Scopes) > 0 {
				ms = ctx.Stats.Scopes[0].MS
			}
			asleep := 0
			g.bodies.Each(func(_ ecs.Entity, _ *gfx.Transform, b *phys.Body3, _ *phys.Collider3) {
				if b.Asleep() {
					asleep++
				}
			})
			var spin float32
			if wb, ok := ecs.Get[phys.Body3](w, g.wheel); ok {
				spin = wb.AngVel.Z
			}
			u.Label(fmt.Sprintf("physics %.2f ms/frame, %d bodies asleep; capsules, hulls and spheres on a mesh terrain, a hinge chain, a motorised paddle wheel (%.1f rad/s), a ragdoll, and a character climbing stairs (grounded: %v)", ms, asleep, spin, g.ctrl.Grounded))
			if u.Button("Drop again (R)") {
				g.drop()
			}
		})
	})
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	ragdoll := flag.Bool("ragdoll", true, "drop a ragdoll with the debris")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip physics lab", Width: 1024, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, ragdoll: *ragdoll})
	if err != nil {
		fmt.Fprintln(os.Stderr, "physics-lab:", err)
		os.Exit(1)
	}
}
