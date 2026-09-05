---
title: Space
example: space
summary: a ship under gravity in a fictional star system on exact Kepler orbits, with a floating origin, orbit prediction and a slider panel
---

This example flies a ship through a made-up star system. A star, seven
planets, a dozen moons, three hundred asteroids and a comet each follow
an exact Kepler orbit around their primary. The ship is a free body
under gravity plus its own thrust, and its predicted path is drawn ahead
of it in the frame of whatever it is orbiting. The camera can be locked
to any body, which moves the floating origin so the scene stays precise
at any distance from the star.

Nothing here is our solar system. Masses, distances and the
gravitational constant are game units, with `G = 1`, which is what the
[orbit](../pkg/orbit.html) package is for: the real constants live in
[orbit/sol](../pkg/orbit/sol.html) and are not used here. The other
packages are [ecs](../pkg/ecs.html) for the bodies and the orbit system,
[gfx](../pkg/gfx.html) for the scene, and [ui](../pkg/ui.html) for the
panel. The guides are [Orbits and space](../guides/space.html) and
[The interface](../guides/ui.html).

Run it with:

```bash
go run ./examples/space -seconds 3 -shot out.png
```

`-focus Name` starts focused on a named body, `-dist N` sets the
starting camera distance and `-warp N` the starting time warp. Drag
orbits the camera and the scroll wheel zooms from the hull out to the
whole system. Tab cycles the focus. W and S thrust prograde and
retrograde, A and D sideways, Q and E out of the orbital plane. The two
sliders set thrust and time warp; Escape quits.

## The system's description

`look` is the render component: a name, a radius, a colour, a glow, and
two flags saying whether to draw the orbit and whether Tab can focus it.
`planet` and `moon` are plain data used once, in `Init`, to build the
world; they are not components.

The comment on `starMass` is the design note that matters for playing
it: with `G = 1` and this mass the inner planet takes two minutes to go
round and the outer one an hour, which is why the time warp slider
exists. The elements are the classical six: semi-major axis,
eccentricity, inclination, longitude of the ascending node, argument of
periapsis and true anomaly, with the angles in radians.

```go
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
```

## The game type

Three cached queries: all drawable bodies, all bodies on a Kepler orbit,
and the two entity handles for the star and the ship. `focus` is the
list of bodies Tab cycles through and `focused` is the index into it.
The camera is again three numbers, and `stars` is a set of unit
directions used to place the background starfield.

```go
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
	lastX    float32
	lastY    float32
	dragging bool
	shotDone bool
	stars    []lin.Vec3 // unit directions of a background starfield
}
```

## Init: building the system

`orbit.Settings` is a resource: `G` is the gravitational constant in
game units, `TimeScale` is the time warp, `Scale` converts orbit units
to render units and `Substeps` is how finely the integrator runs per
update.

Each body carries an `orbit.Body` with a mass, a `gfx.Transform` that
the orbit system writes, a `look`, and for everything but the ship an
`orbit.Kepler` naming its primary and its elements. A `Kepler` body is
placed analytically on its ellipse rather than integrated, so it never
drifts however long the program runs and however hard the time warp is
pushed.

The asteroids get `orbit.Body{}` with a zero mass. They follow their
orbits but pull on nothing, so three hundred of them cost the ship's
integration nothing.

```go
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
```

The comet has an eccentricity of 0.93, which gives the long thin ellipse
that makes the orbit rings worth drawing.

## Placing the ship

The ship needs a position and a velocity, not elements, because it is a
free body. `w.Update(0)` runs the orbit system once with no time step,
which places every Kepler body at its starting element, so Vessa's
position and velocity can be read. `orbit.Elements{...}.State(mu)`
converts elements to a state vector for a given standard gravitational
parameter, and adding that relative state to Vessa's gives a ship in a
low orbit around it, inside its moons.

`orbit.Ship{}` and `orbit.Thrust{}` mark the entity as an integrated
body that accepts thrust. The focus list is then built: the ship and the
star first, then every body whose `look.Focus` is set, so Tab visits
them in a sensible order.

```go
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
```

## Update: thrust, warp and the floating origin

`g.ui.WantsMouse()` guards the drag, so dragging a slider does not also
turn the camera. That test is the one piece of plumbing an
immediate-mode interface needs from the game.

The thrust directions are derived from the ship's own velocity:
prograde is the velocity normalised, `side` is prograde crossed with a
fixed axis, and `up` completes the set. The keys sum into an
acceleration which is scaled by the thrust slider and written to the
`orbit.Thrust` component; the integrator applies it.

The last step sets `settings.Origin` to the focused body's position.
Everything is then drawn relative to that point, so the coordinates the
renderer sees stay small however far the system extends. Positions in
`orbit` are `float64`, and the origin is what keeps the `float32` render
side precise.

```go
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
		g.yaw -= (x - g.lastX) * 0.01
		g.pitch = lin.Clamp(g.pitch+(y-g.lastY)*0.01, -1.5, 1.5)
	}
	g.lastX, g.lastY = x, y
	if _, dy := in.Scroll(); dy != 0 {
		g.dist = lin.Clamp(g.dist*float32(math.Pow(0.88, float64(dy))), 1, 3000)
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
```

## The camera

The camera orbits the origin, which is the focused body, with `z` up
because the system's reference plane is x-y, which is the convention the
`orbit` package uses. `Near` is small and `Far` is 8000 so the ship's
hull and the whole system are both in range.

```go
// camera orbits the focused body, which is at the origin, with z up: the
// system's reference plane is x-y.
func (g *game) camera() gfx.Camera {
	cp, sp := float32(math.Cos(float64(g.pitch))), float32(math.Sin(float64(g.pitch)))
	cy, sy := float32(math.Cos(float64(g.yaw))), float32(math.Sin(float64(g.yaw)))
	return gfx.Camera{Position: lin.V3(g.dist*cp*cy, g.dist*cp*sy, g.dist*sp), Up: lin.V3(0, 0, 1), Near: 0.05, Far: 8000}
}
```

## Draw: lighting a system from one star

At the scale of a whole system the star is treated as a directional
light: the direction is from the star towards the origin, and the
strength falls off with the square of the distance, normalised so it is
1 at 110 units and clamped into a usable range. That is why the outer
worlds are dimmer without any per-object work.

The second light is planet shine. The body that looms largest over the
focused point, measured as its radius over its distance, lights the
scene from its side in its own colour, so the night side of the ship is
never pure black. It is set through `Sky.Up` and `Sky.Ground`, with
`Vacuum: 1` saying there is no atmosphere to scatter anything, and the
starfield is drawn by hand instead.

```go
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
```

## The starfield, the bodies and the orbit rings

The starfield is 400 emissive spheres at 7000 units in fixed directions.
Because the whole scene is drawn relative to the focused body, they
never move against the system, which is what a distant background should
do.

Each body is drawn no smaller than a fraction of the camera distance, so
a planet far away is still a visible dot rather than a subpixel
triangle. Nameless bodies, meaning asteroids, get a smaller floor.

An orbit ring is drawn by stepping the true anomaly right around the
ellipse and evaluating `Elements.State(mu)` at each step, then adding
the primary's current position and subtracting the origin. `Lin()`
converts the orbit package's `float64` vector to the render vector.
Moon rings are only drawn when the camera is close enough for them to
be distinguishable, and with fewer points.

```go
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
```

## The predicted path and the labels

`orbit.Around(w, e)` reports which body the ship is currently orbiting,
the elements of that orbit and the standard gravitational parameter. On
a closed orbit the prediction horizon is one and a half periods, capped;
on an escape trajectory it falls back to a fixed 60 seconds.
`orbit.PredictRelative` returns points along the future path in the
primary's frame, which is what makes the path stand still while the
ship and its primary both move.

The labels are projected by hand: `cam.ViewProj(aspect)` gives the
matrix, the position is multiplied through it as a homogeneous point,
anything behind the camera is dropped, and the clip coordinates are
mapped into view units. Labels are hidden when the camera is far away
or the body is minor, so the screen does not fill with names.

```go
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
```

## The panel

The interface is rebuilt every frame inside `u.Begin(input, body)`, and
`u.Panel(title, rect, body)` nests inside it. Both take closures and
there is no exported call to end them. Widgets that hold a value take a
pointer, so `u.Slider("Thrust", &g.thrust, 0, 0.05)` writes straight
into the game's field and the next `Update` reads it.

The readout above the sliders is the orbit as elements rather than as
coordinates: which body the ship is orbiting, the semi-major axis and
eccentricity, and, on a closed orbit, the periapsis, apoapsis and
period. Those are the numbers a player steers by.

```go
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
```

## main

```go
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
```

## What to try

- Add a planet to the `planets` table with a large eccentricity and see
  its ring against the others.
- Run with `-focus Nimbus -dist 60` to watch a moon system from the
  outside, and push the time warp slider up.
- Give the ship a fuel figure in `game`, decrement it in `Update` while
  thrusting, and show it as a label in `Draw`.
- Raise `asteroids` to 5000 and check the frame time; massless Kepler
  bodies are placed analytically, so the cost is in drawing them.
- Draw the predicted path in `Draw` in the star's frame instead of the
  primary's, by passing `g.star` to `PredictRelative`, and see why the
  primary's frame is the readable one.
