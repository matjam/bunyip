// Command space flies a ship through a fictional star system: a star,
// seven planets with a dozen moons between them, an asteroid belt and a
// comet, every one on an exact Kepler orbit around its primary. The
// ship is a free body under every massive body's gravity plus its own
// thrust, with its predicted path drawn ahead in the frame of whatever
// it orbits. Nothing here is our solar system: masses, distances and
// even G are game units, which is the point.
//
// Drag orbits the camera and scroll zooms from the ship's hull out to
// the whole system. Tab cycles the camera's focus through the bodies.
// W and S thrust prograde and retrograde, A and D sideways, Q and E
// above and below the orbit. The sliders set thrust and time warp;
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
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/ui"
)

// look says how to draw a body.
type look struct {
	Name   string
	Radius float32
	Color  gfx.Color
	Glow   float32
	Orbit  bool // draw the orbit as a ring of dots
	Focus  bool // Tab can focus it
}

// planet describes one world of the system and its moons.
type planet struct {
	name       string
	a, e, incl float64
	node, arg  float64
	mass       float64
	radius     float32
	color      gfx.Color
	moons      []moon
}

type moon struct {
	a, incl float64
}

// A calm, spread-out system: with G = 1 and this star mass the inner
// planet takes two minutes to go round and the outer one an hour, so
// the time warp slider matters.
const starMass = 175

var planets = []planet{
	{name: "Ash", a: 40, e: 0.15, incl: 0.02, node: 0.5, arg: 1.0, mass: 0.3, radius: 0.45, color: gfx.RGB(190, 150, 120)},
	{name: "Ferro", a: 70, e: 0.05, incl: 0.03, node: 2.0, arg: 0.4, mass: 0.8, radius: 0.8, color: gfx.RGB(200, 110, 80), moons: []moon{{2.6, 0.1}}},
	{name: "Vessa", a: 110, e: 0.03, incl: 0.01, node: 4.0, arg: 2.2, mass: 1.2, radius: 1.0, color: gfx.RGB(80, 150, 210), moons: []moon{{5.0, 0.05}, {8.0, 0.25}}},
	{name: "Grond", a: 170, e: 0.08, incl: 0.04, node: 1.2, arg: 3.1, mass: 4, radius: 1.7, color: gfx.RGB(200, 170, 120), moons: []moon{{4.5, 0}, {7, 0.1}, {11, 0.3}}},
	{name: "Pell", a: 265, e: 0.12, incl: 0.15, node: 3.5, arg: 0.9, mass: 0.05, radius: 0.3, color: gfx.RGB(160, 160, 170)},
	{name: "Nimbus", a: 380, e: 0.05, incl: 0.02, node: 5.5, arg: 1.7, mass: 8, radius: 2.3, color: gfx.RGB(150, 190, 230), moons: []moon{{5.5, 0.02}, {8.5, 0.05}, {13, 0.1}, {21, 0.4}}},
	{name: "Thule", a: 600, e: 0.2, incl: 0.3, node: 0.3, arg: 2.5, mass: 0.2, radius: 0.45, color: gfx.RGB(200, 220, 240)},
}

const asteroids = 300

type game struct {
	seconds    float64
	shot       string
	startFocus string
	startDist  float32
	startWarp  float32

	font     *gfx.Font
	ui       *ui.Context
	world    *ecs.World
	sphere   *gfx.Mesh
	dot      *gfx.Mesh
	bodies   *ecs.Query2[gfx.Transform, look]
	orbits   *ecs.Query3[orbit.Kepler, orbit.Body, look]
	star     ecs.Entity
	ship     ecs.Entity
	focus    []ecs.Entity
	focused  int
	thrust   float32
	warp     float32
	yaw      float32
	pitch    float32
	dist     float32
	lastX    float64
	lastY    float64
	dragging bool
	shotDone bool
	stars    []lin.Vec3 // unit directions of a background starfield
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 14, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	sv, si := gfx.SphereMesh(16, 32)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	dv, di := gfx.SphereMesh(4, 6)
	if g.dot, err = ctx.Gfx.NewMesh(dv, di); err != nil {
		return err
	}
	g.thrust, g.warp = 0.01, 1
	g.yaw, g.pitch, g.dist = 0.6, 0.35, 14
	random := rng.New(11)
	for range 400 {
		g.stars = append(g.stars, lin.V3(random.Between(-1, 1), random.Between(-1, 1), random.Between(-1, 1)).Norm())
	}
	w := ecs.NewWorld()
	g.world = w
	g.bodies = ecs.NewQuery2[gfx.Transform, look](w)
	g.orbits = ecs.NewQuery3[orbit.Kepler, orbit.Body, look](w)
	ecs.SetResource(w, orbit.Settings{G: 1, TimeScale: 1, Scale: 1, Substeps: 8})
	g.star = w.SpawnWith(orbit.Body{Mass: starMass}, gfx.Transform{}, look{"Kaal", 4, gfx.RGB(255, 214, 130), 5, false, true})
	var vessa ecs.Entity
	for _, p := range planets {
		e := w.SpawnWith(orbit.Body{Mass: p.mass}, gfx.Transform{}, look{p.name, p.radius, p.color, 0, true, true},
			orbit.Kepler{Primary: g.star, Elements: orbit.Elements{SemiMajorAxis: p.a, Eccentricity: p.e, Inclination: p.incl,
				Node: p.node, ArgPeriapsis: p.arg, TrueAnomaly: float64(random.Float()) * 2 * math.Pi}})
		if p.name == "Vessa" {
			vessa = e
		}
		for i, m := range p.moons {
			w.SpawnWith(orbit.Body{Mass: p.mass * 0.008}, gfx.Transform{},
				look{fmt.Sprintf("%s %s", p.name, []string{"I", "II", "III", "IV"}[i]), 0.12 + 0.05*float32(i), gfx.RGB(190, 190, 200), 0, true, false},
				orbit.Kepler{Primary: e, Elements: orbit.Elements{SemiMajorAxis: m.a, Inclination: m.incl,
					Node: float64(random.Float()) * 6, TrueAnomaly: float64(random.Float()) * 2 * math.Pi}})
		}
	}
	// The belt: massless rocks between Grond and Nimbus. They follow
	// their orbits but pull on nothing, so they cost the ship nothing.
	for range asteroids {
		shade := uint8(110 + random.Intn(60))
		w.SpawnWith(orbit.Body{}, gfx.Transform{}, look{"", random.Between(0.04, 0.12), gfx.RGB(shade, shade, shade-10), 0, false, false},
			orbit.Kepler{Primary: g.star, Elements: orbit.Elements{SemiMajorAxis: float64(random.Between(225, 305)), Eccentricity: float64(random.Between(0, 0.12)),
				Inclination: float64(random.Between(-0.08, 0.08)), Node: float64(random.Float()) * 6, ArgPeriapsis: float64(random.Float()) * 6,
				TrueAnomaly: float64(random.Float()) * 2 * math.Pi}})
	}
	w.SpawnWith(orbit.Body{}, gfx.Transform{}, look{"Comet Iri", 0.1, gfx.RGB(210, 240, 255), 3, true, true},
		orbit.Kepler{Primary: g.star, Elements: orbit.Elements{SemiMajorAxis: 330, Eccentricity: 0.93, Inclination: 0.55, Node: 4.2, ArgPeriapsis: 1.1, TrueAnomaly: 2.4}})
	w.AddSystem("orbits", orbit.System)
	// The ship starts in a low orbit around Vessa, inside its moons.
	w.Update(0)
	vb, _ := ecs.Get[orbit.Body](w, vessa)
	rel := orbit.Elements{SemiMajorAxis: 2.4, TrueAnomaly: math.Pi}.State(1 * vb.Mass)
	g.ship = w.SpawnWith(orbit.Ship{}, orbit.Thrust{}, orbit.Body{Pos: vb.Pos.Add(rel.Pos), Vel: vb.Vel.Add(rel.Vel)}, gfx.Transform{},
		look{"Ship", 0.03, gfx.RGB(255, 255, 255), 2, false, true})
	g.focus = append(g.focus, g.ship, g.star)
	g.bodies.Each(func(e ecs.Entity, _ *gfx.Transform, l *look) {
		if l.Focus && e != g.ship && e != g.star {
			g.focus = append(g.focus, e)
		}
	})
	for i, e := range g.focus {
		if l, ok := ecs.Get[look](w, e); ok && l.Name == g.startFocus {
			g.focused = i
		}
	}
	if g.startDist > 0 {
		g.dist = g.startDist
	}
	if g.startWarp > 0 {
		g.warp = g.startWarp
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.dot.Destroy()
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
	if in.KeyPressed(input.KeyTab) {
		g.focused = (g.focused + 1) % len(g.focus)
	}
	x, y := in.Mouse()
	if in.MousePressed(input.MouseLeft) && !g.ui.WantsMouse() {
		g.dragging = true
	}
	if in.MouseReleased(input.MouseLeft) {
		g.dragging = false
	}
	if g.dragging {
		g.yaw -= float32(x-g.lastX) * 0.01
		g.pitch = lin.Clamp(g.pitch+float32(y-g.lastY)*0.01, -1.5, 1.5)
	}
	g.lastX, g.lastY = x, y
	if _, dy := in.Scroll(); dy != 0 {
		g.dist = lin.Clamp(g.dist*float32(math.Pow(0.88, dy)), 1, 3000)
	}
	w := g.world
	settings := ecs.Resource[orbit.Settings](w)
	settings.TimeScale = float64(g.warp)
	// Thrust along the ship's velocity (prograde), across it, or out of
	// its orbital plane.
	body, _ := ecs.Get[orbit.Body](w, g.ship)
	thrust, _ := ecs.Get[orbit.Thrust](w, g.ship)
	prograde := body.Vel.Norm()
	side := prograde.Cross(orbit.V3(0, 0, 1)).Norm()
	up := side.Cross(prograde)
	var a orbit.Vec3
	for _, k := range []struct {
		key input.Key
		dir orbit.Vec3
	}{{input.KeyW, prograde}, {input.KeyS, prograde.Mul(-1)}, {input.KeyD, side}, {input.KeyA, side.Mul(-1)}, {input.KeyQ, up}, {input.KeyE, up.Mul(-1)}} {
		if in.KeyDown(k.key) {
			a = a.Add(k.dir)
		}
	}
	thrust.Accel = a.Mul(float64(g.thrust))
	// The focused body sits at the floating origin, so the scene stays
	// precise wherever it is.
	if fb, ok := ecs.Get[orbit.Body](w, g.focus[g.focused]); ok {
		settings.Origin = fb.Pos
	}
	w.Update(ctx.Delta)
	return nil
}

// camera orbits the focused body, which is at the origin, with z up: the
// system's reference plane is x-y.
func (g *game) camera() gfx.Camera {
	cp, sp := float32(math.Cos(float64(g.pitch))), float32(math.Sin(float64(g.pitch)))
	cy, sy := float32(math.Cos(float64(g.yaw))), float32(math.Sin(float64(g.yaw)))
	return gfx.Camera{Position: lin.V3(g.dist*cp*cy, g.dist*cp*sy, g.dist*sp), Up: lin.V3(0, 0, 1), Near: 0.05, Far: 8000}
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	cam := g.camera()
	gr.SetCamera(cam)
	// Sunlight: at the scale of a system the star is a directional light
	// travelling from it towards the focused body, and its strength
	// falls off with distance so the outer worlds are dimmer.
	sun := lin.V3(0, 0, -1)
	strength := float32(1)
	if st, ok := ecs.Get[gfx.Transform](w, g.star); ok && st.Position.Len() > 1 {
		sun = st.Position.Mul(-1).Norm()
		strength = lin.Clamp(110*110/st.Position.Dot(st.Position), 0.25, 3)
	}
	// Planet-shine: the world that looms largest over the focused body
	// lights the scene from its side of the sky in its own colour, so the
	// night side of the ship is never pure black. The sky itself is a
	// vacuum: no air, and the starfield is drawn by hand below.
	sky := gfx.Sky{Up: lin.V3(0, 0, 1), Vacuum: 1}
	var loom float32
	for _, e := range g.focus {
		if e == g.ship || e == g.star {
			continue
		}
		t, ok := ecs.Get[gfx.Transform](w, e)
		l, ok2 := ecs.Get[look](w, e)
		if !ok || !ok2 || t.Position.Len() < 1e-3 {
			continue
		}
		if size := l.Radius / t.Position.Len(); size > loom {
			loom = size
			k := lin.Clamp(size*2, 0, 1) * 0.35 * strength
			sky.Up = t.Position.Mul(-1).Norm()
			sky.Ground = gfx.Color{R: l.Color.R * k, G: l.Color.G * k, B: l.Color.B * k, A: 1}
		}
	}
	gr.SetLight(gfx.Light{Direction: sun, Color: gfx.Color{R: 2.4 * strength, G: 2.2 * strength, B: 1.9 * strength, A: 1},
		Ambient: gfx.Color{R: 0.03, G: 0.03, B: 0.05, A: 1}, Sky: sky})
	settings := ecs.Resource[orbit.Settings](w)
	origin := settings.Origin
	// A distant starfield, fixed to the focused body so it never
	// parallaxes against the system.
	for i, d := range g.stars {
		s := 5 + float32(i%3)*2
		gr.DrawMesh(g.dot, gfx.Material{BaseColor: gfx.RGB(200, 205, 225), Emissive: 0.5 + 0.2*float32(i%4)}, lin.Translate(d.Mul(7000)).Mul(lin.Scale(lin.V3(s, s, s))))
	}
	// Bodies, drawn no smaller than a couple of pixels so a planet far
	// from the camera is still a visible dot.
	g.bodies.Each(func(e ecs.Entity, t *gfx.Transform, l *look) {
		r := max(l.Radius, g.dist*0.003)
		if l.Name == "" {
			r = max(l.Radius, g.dist*0.0012)
		}
		gr.DrawMesh(g.sphere, gfx.Material{BaseColor: l.Color, Emissive: l.Glow, Roughness: 0.9}, lin.Translate(t.Position).Mul(lin.Scale(lin.V3(r, r, r))))
	})
	// Orbit rings: each Kepler body's ellipse around where its primary
	// is now. Moons only when close enough for the rings to read.
	dotSize := lin.V3(g.dist*0.0016, g.dist*0.0016, g.dist*0.0016)
	g.orbits.Each(func(e ecs.Entity, k *orbit.Kepler, _ *orbit.Body, l *look) {
		if !l.Orbit {
			return
		}
		pb, ok := ecs.Get[orbit.Body](w, k.Primary)
		if !ok {
			return
		}
		moonOf := k.Primary != g.star
		if moonOf && g.dist > 200 {
			return
		}
		n := 128
		if moonOf {
			n = 48
		}
		mu := settings.G * pb.Mass
		el := k.Elements
		col := gfx.Color{R: l.Color.R * 0.8, G: l.Color.G * 0.8, B: l.Color.B * 0.8, A: 1}
		for i := range n {
			el.TrueAnomaly = 2 * math.Pi * float64(i) / float64(n)
			p := el.State(mu).Pos.Add(pb.Pos).Sub(origin).Lin()
			gr.DrawMesh(g.dot, gfx.Material{BaseColor: col, Emissive: 1}, lin.Translate(p).Mul(lin.Scale(dotSize)))
		}
	})
	// The ship's predicted path, in the frame of the body it orbits.
	body, _ := ecs.Get[orbit.Body](w, g.ship)
	primary, el, mu, ok := orbit.Around(w, g.ship)
	horizon := 60.0
	if ok && el.Eccentricity < 1 {
		horizon = min(1.5*el.Period(mu), 600)
	}
	pathSize := lin.V3(g.dist*0.002, g.dist*0.002, g.dist*0.002)
	for _, p := range orbit.PredictRelative(w, g.ship, primary, horizon, 90) {
		gr.DrawMesh(g.dot, gfx.Material{BaseColor: gfx.RGB(120, 220, 255), Emissive: 1.5}, lin.Translate(p).Mul(lin.Scale(pathSize)))
	}
	// Labels, projected through the camera.
	vp := cam.ViewProj(float32(ctx.Width) / float32(ctx.Height))
	g.bodies.Each(func(e ecs.Entity, t *gfx.Transform, l *look) {
		if l.Name == "" || (!l.Focus && (g.dist > 60 || t.Position.Len() > 60)) || (e == g.ship && g.dist > 300) {
			return
		}
		c := vp.MulVec4(t.Position.Vec4(1))
		if c.W <= 0 {
			return
		}
		sx, sy := (c.X/c.W*0.5+0.5)*ctx.Width, (c.Y/c.W*0.5+0.5)*ctx.Height
		if sx < 0 || sx > ctx.Width || sy < 0 || sy > ctx.Height {
			return
		}
		gr.DrawText(g.font, l.Name, sx+8, sy-6, gfx.RGBA(220, 225, 235, 180))
	})
	focusName := "?"
	if l, ok := ecs.Get[look](w, g.focus[g.focused]); ok {
		focusName = l.Name
	}
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Ship", ui.Rect{X: 12, Y: 12, W: 340, H: 330}, func() {
			u.Label(fmt.Sprintf("focus: %s (Tab cycles)", focusName))
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
			u.Slider("Thrust", &g.thrust, 0, 0.05)
			u.Slider("Time warp", &g.warp, 1, 200)
			u.Label("W/S prograde, A/D sideways, Q/E out of plane. Drag orbits, scroll zooms. Fictional system, G = 1.")
		})
	})
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	focus := flag.String("focus", "Ship", "body to start focused on")
	dist := flag.Float64("dist", 14, "starting camera distance")
	warp := flag.Float64("warp", 1, "starting time warp")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip space", Width: 1024, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, startFocus: *focus, startDist: float32(*dist), startWarp: float32(*warp)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "space:", err)
		os.Exit(1)
	}
}
