// Command space flies a ship through a fictional star system. Planets
// and moons follow exact Kepler orbits around their primaries; the ship
// is a free body integrated under every body's gravity plus its own
// thrust, with its predicted path drawn ahead of it. The system is not
// ours: masses, distances and even the gravitational constant are made
// up, in game units, which is the point. W and S thrust prograde and
// retrograde, A and D sideways; the sliders set thrust and time warp;
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
	"github.com/matjam/bunyip/orbit"
	"github.com/matjam/bunyip/ui"
)

// look says how to draw a body.
type look struct {
	Name   string
	Radius float32
	Color  gfx.Color
	Glow   float32
}

type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	ui       *ui.Context
	world    *ecs.World
	sphere   *gfx.Mesh
	bodies   *ecs.Query2[gfx.Transform, look]
	star     ecs.Entity
	ship     ecs.Entity
	thrust   float32
	warp     float32
	yaw      float32
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	sv, si := gfx.SphereMesh(16, 32)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	g.thrust, g.warp = 0.02, 1
	w := ecs.NewWorld()
	g.world = w
	g.bodies = ecs.NewQuery2[gfx.Transform, look](w)
	// Game units: G is 1, the star has mass 5000, distances are in
	// scene units and time in seconds. Nothing here is our solar system.
	ecs.SetResource(w, orbit.Settings{G: 1, TimeScale: 1, Scale: 1, Substeps: 8})
	star := w.SpawnWith(orbit.Body{Mass: 5000}, gfx.Transform{}, look{"Kaal", 2.5, gfx.RGB(255, 210, 120), 4})
	g.star = star
	var vessa ecs.Entity
	planets := []struct {
		name  string
		a, e  float64
		incl  float64
		mass  float64
		r     float32
		col   gfx.Color
		moons int
	}{
		{"Ferro", 30, 0.05, 0.02, 4, 0.6, gfx.RGB(200, 120, 90), 0},
		{"Vessa", 55, 0.1, 0.1, 12, 1.0, gfx.RGB(90, 170, 200), 1},
		{"Grond", 95, 0.2, 0.05, 40, 1.8, gfx.RGB(190, 160, 110), 2},
	}
	for i, p := range planets {
		e := w.SpawnWith(orbit.Body{Mass: p.mass}, gfx.Transform{}, look{p.name, p.r, p.col, 0},
			orbit.Kepler{Primary: star, Elements: orbit.Elements{SemiMajorAxis: p.a, Eccentricity: p.e, Inclination: p.incl, ArgPeriapsis: float64(i), TrueAnomaly: float64(i) * 2}})
		if i == 1 {
			vessa = e
		}
		for m := range p.moons {
			w.SpawnWith(orbit.Body{Mass: p.mass * 0.01}, gfx.Transform{}, look{p.name + " moon", 0.3, gfx.RGB(200, 200, 210), 0},
				orbit.Kepler{Primary: e, Elements: orbit.Elements{SemiMajorAxis: float64(p.r)*3 + float64(m)*3, Inclination: 0.3 * float64(m), TrueAnomaly: float64(m) * 1.5}})
		}
	}
	w.AddSystem("orbits", orbit.System)
	// The ship starts in a low circular orbit around Vessa, the second
	// planet, on the far side from its moon.
	w.Update(0)
	vb, _ := ecs.Get[orbit.Body](w, vessa)
	rel := orbit.Elements{SemiMajorAxis: 1.8, TrueAnomaly: math.Pi}.State(1 * vb.Mass)
	g.ship = w.SpawnWith(orbit.Ship{}, orbit.Thrust{}, orbit.Body{Pos: vb.Pos.Add(rel.Pos), Vel: vb.Vel.Add(rel.Vel)}, gfx.Transform{}, look{"ship", 0.15, gfx.RGB(255, 255, 255), 2})
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.sphere.Destroy()
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
	w := g.world
	settings := ecs.Resource[orbit.Settings](w)
	settings.TimeScale = float64(g.warp)
	// Thrust along the ship's velocity (prograde) or across it.
	body, _ := ecs.Get[orbit.Body](w, g.ship)
	thrust, _ := ecs.Get[orbit.Thrust](w, g.ship)
	prograde := body.Vel.Norm()
	side := prograde.Cross(orbit.V3(0, 0, 1)).Norm()
	var a orbit.Vec3
	if in.KeyDown(input.KeyW) {
		a = a.Add(prograde)
	}
	if in.KeyDown(input.KeyS) {
		a = a.Sub(prograde)
	}
	if in.KeyDown(input.KeyA) {
		a = a.Sub(side)
	}
	if in.KeyDown(input.KeyD) {
		a = a.Add(side)
	}
	thrust.Accel = a.Mul(float64(g.thrust))
	// Keep the ship at the floating origin so the scene stays precise.
	settings.Origin = body.Pos
	w.Update(ctx.Delta)
	g.yaw += float32(ctx.Delta) * 0.05
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.Camera{Position: lin.V3(18*float32(math.Sin(float64(g.yaw))), 14, 18*float32(math.Cos(float64(g.yaw)))), Target: lin.V3(0, 0, 0), Far: 2000})
	gr.SetLight(gfx.Light{Direction: lin.V3(0.2, -1, 0.3), Color: gfx.Color{R: 0.8, G: 0.8, B: 0.9, A: 1}, Ambient: gfx.Color{R: 0.12, G: 0.12, B: 0.16, A: 1}})
	if st, ok := ecs.Get[gfx.Transform](w, g.star); ok {
		gr.AddPointLight(st.Position, gfx.Color{R: 60, G: 50, B: 35, A: 1}, 400)
	}
	g.bodies.Each(func(e ecs.Entity, t *gfx.Transform, l *look) {
		gr.DrawMesh(g.sphere, gfx.Material{BaseColor: l.Color, Emissive: l.Glow, Roughness: 0.8}, lin.Translate(t.Position).Mul(lin.Scale(lin.V3(l.Radius, l.Radius, l.Radius))))
	})
	body, _ := ecs.Get[orbit.Body](w, g.ship)
	primary, el, mu, ok := orbit.Around(w, g.ship)
	// Predicted path: dots along where the ship will go, drawn in the
	// frame of the body it orbits so the loop sits around that body.
	for i, p := range orbit.PredictRelative(w, g.ship, primary, 30, 120) {
		s := 0.06 - 0.04*float32(i)/120
		gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(120, 220, 255), Emissive: 1}, lin.Translate(p).Mul(lin.Scale(lin.V3(s, s, s))))
	}
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Ship", ui.Rect{X: 12, Y: 12, W: 320, H: 290}, func() {
			u.Label(fmt.Sprintf("speed %.3f u/s", body.Vel.Len()))
			if ok {
				name := "?"
				if l, ok := ecs.Get[look](w, primary); ok {
					name = l.Name
				}
				u.Label(fmt.Sprintf("orbiting %s: a %.1f, e %.2f", name, el.SemiMajorAxis, el.Eccentricity))
				if el.Eccentricity < 1 {
					u.Label(fmt.Sprintf("periapsis %.1f, apoapsis %.1f, period %.0f s", el.Periapsis(), el.Apoapsis(), el.Period(mu)))
				} else {
					u.Label("escape trajectory")
				}
			}
			u.Slider("Thrust", &g.thrust, 0, 0.1)
			u.Slider("Time warp", &g.warp, 0.1, 30)
			u.Label("W/S prograde and retrograde, A/D sideways. Fictional system, G = 1.")
		})
	})
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip space", Width: 1024, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "space:", err)
		os.Exit(1)
	}
}
