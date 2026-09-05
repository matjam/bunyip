---
title: Orbits and space
group: Simulation
order: 2
summary: celestial mechanics for any star system: Kepler orbits, N-body gravity, ships under thrust
---

The [orbit](../pkg/orbit.html) package computes celestial mechanics for
any star system, not only our own. Bodies are masses at positions in
double precision. You choose the gravitational constant and the units.
For our solar system, use metres, kilograms and seconds with the real
`G` and the constants in [orbit/sol](../pkg/orbit/sol.html). For an
invented system, set `G` to 1 and pick your own game units, as the
space example does.

## Two-body orbits

`Elements` are the classical orbital elements (semi-major axis,
eccentricity, inclination, node, argument of periapsis, true anomaly).
`Elements.State` turns them into a position and velocity for a primary
of gravitational parameter mu (G times its mass), and `ElementsOf`
goes back. `Propagate` advances a state using an iterative two-body
solution for circles, ellipses, parabolas and hyperbolas, so a moon's position
a year from now takes one call rather than a million integration steps.
Angles are radians in an XY reference plane with +Z as the normal.
Use positive mu and a nonzero orbital radius. For an exactly parabolic
orbit (`Eccentricity == 1`), `SemiMajorAxis` stores periapsis distance;
the `Periapsis()` formula itself returns zero for that case. Propagation
uses float64 arithmetic and does not report convergence failure.

```go
el := orbit.Elements{SemiMajorAxis: 20, Eccentricity: 0.3}
s := el.State(mu)
later := orbit.Propagate(s, mu, el.Period(mu)/2) // at apoapsis
```

`Period`, `Periapsis`, `Apoapsis`, `CircularVelocity`,
`EscapeVelocity`, `VisViva`, `Hohmann` and `SphereOfInfluence` compute
the figures a readout displays and the burns a transfer needs.

```go
// Real units: low Earth orbit to geostationary, and the elements of a
// state vector measured relative to the primary.
dv1, dv2, coast := orbit.Hohmann(sol.MuEarth, 6678e3, 42164e3)
fmt.Printf("%.2f km/s, coast %.1f h, %.2f km/s\n", dv1/1000, coast/3600, dv2/1000)

now := orbit.ElementsOf(orbit.State{Pos: rel, Vel: relVel}, sol.MuEarth)
soon := now.AtTime(sol.MuEarth, 600) // where it is ten minutes on
```

## N-body simulation

`Simulation` integrates bodies under their mutual gravity with a
symplectic leapfrog. A suitably small fixed step limits energy error;
energy is not conserved exactly. `Softening` adds its square to squared
separations to regularize close encounters. `Energy()` reports the
unsoftened energy, so it is not the softened system's conserved quantity.
`RK4` steps a single particle through an acceleration field.
Use `RK4` for a ship whose engine contributes to the force on it.

```go
// Two stars of equal mass circling each other, in game units.
sim := orbit.Simulation{G: 1, Bodies: []orbit.Body{
	{Pos: orbit.V3(-5, 0, 0), Vel: orbit.V3(0, -5, 0), Mass: 500},
	{Pos: orbit.V3(5, 0, 0), Vel: orbit.V3(0, 5, 0), Mass: 500},
}}
for range 1000 {
	sim.Step(0.01)
}

// A ship falling through that field with its engine burning.
pos, vel = orbit.RK4(pos, vel, dt, func(p, v orbit.Vec3) orbit.Vec3 {
	return sim.FieldAt(p).Add(thrust)
})
```

## In the ECS

Give entities an `orbit.Body` (position, velocity, mass) and the
`orbit.System`:

- entities with a `Kepler` component follow an exact orbit around their
  `Primary`, and chains are supported, such as moons around planets
  around a star;
- entities marked `Ship` are integrated under massive non-Ship bodies'
  gravity plus constant world-axis `Thrust`; ships do not attract one
  another, regardless of mass;
- a `Body` with neither `Ship` nor `Kepler` stays fixed;
- entities with both `Body` and `gfx.Transform` get a scaled position
  written into the transform each update.

Use either `Ship` or `Kepler` on each moving entity. Keep primary chains
acyclic and no more than sixteen links deep. This ECS system uses
prescribed Kepler paths and independent ship integration; use
`Simulation` when all bodies must exert mutual gravity.

```go
w.SetResource(orbit.Settings{G: 1, TimeScale: 10, Scale: 1})
star := w.SpawnWith(orbit.Body{Mass: 5000}, gfx.Transform{})
planet := w.SpawnWith(orbit.Body{Mass: 12}, gfx.Transform{},
	orbit.Kepler{Primary: star, Elements: orbit.Elements{SemiMajorAxis: 55, Eccentricity: 0.1}})
ship := w.SpawnWith(orbit.Ship{}, orbit.Thrust{}, orbit.Body{Pos: ..., Vel: ...}, gfx.Transform{})
w.AddSystem("orbits", orbit.System)
```

`Settings.TimeScale` is the time warp. `Settings.Scale` converts
simulation distance into scene units. `Settings.Origin` is the floating
origin. Set it to the ship's or camera's position each frame. The scene
then stays near zero however far the ship has flown, which is the range
where float32 rendering is precise. Zero `TimeScale` and `Scale` each
mean 1; zero `G` selects the SI constant. Use a nonnegative scaled
timestep because the ship integrator does not run backwards. Pause by
disabling the orbit system or skipping its update, not by setting
`TimeScale` to zero.

Before saving an orbital world, register each type it uses, including
`Body`, `Kepler`, `Ship`, `Thrust` and `Settings`, and use
`ecs.SaveOptions{SkipUnregistered: true}` to omit
the orbit system's private query cache. Kepler's JSON includes its elapsed
simulated time after time scaling, so a
loaded world continues at the saved phase and remaps its `Primary`
references to the restored entities. Older saves without elapsed time
start at the elements' original epoch.

`Predict` integrates a copy of a ship ahead and returns its path in
scene units for drawing. Its horizon is nonnegative simulation time,
unaffected by `TimeScale`. It returns the requested number of samples
after the starting point, including the end of the horizon. `Settings`
and the ship's `Body` must exist; missing either or nonpositive sample
count returns nil. Kepler sources advance during prediction, fixed
sources stay fixed, other ships exert no gravity, and current thrust
stays constant.
`PredictRelative` returns that path in the frame of a chosen body, so a
ship circling a planet draws a closed loop around the planet's current
position instead of a long arc across the scene. Only Kepler reference
bodies advance in that moving-frame correction. `Around` reports which
body dominates a ship and the ship's orbital elements relative to it,
which is what a readout needs. Pass that primary to `PredictRelative`.

```go
// Draw a lap and a half of the ship's path around its primary.
primary, el, mu, ok := orbit.Around(w, ship)
horizon := 60.0
if ok && el.Eccentricity < 1 {
	horizon = min(1.5*el.Period(mu), 600)
}
for _, p := range orbit.PredictRelative(w, ship, primary, horizon, 90) {
	gr.DrawMesh(dot, mat, lin.Translate(p).Mul(lin.Scale(dotSize)))
}
```

Ships use adaptive RK4 steps, starting with at least `Substeps` per
update (default 8) and reducing the step according to local speed and
acceleration, down to one sixty-fourth of that initial step. Kepler
sources are sampled at each step's midpoint. Large time warps still
need adequate substeps and validation for the intended trajectories.

## The space example

`examples/space` uses all of this. It has a star, seven planets with a
dozen moons, three hundred massless asteroids in a belt (they follow
their orbits but pull on nothing, so they add nothing to the cost of
the ship's integration), and a comet on a steep, eccentric orbit. Every
body's orbit is drawn as a ring of dots from its elements, the ship's
predicted path is drawn in the frame of its primary, Tab cycles the
camera's focus (and the floating origin) through the bodies, and the
scroll wheel zooms from the ship's hull out to the whole system. With
`G = 1` and a star of mass 175 the inner planet takes two minutes to
complete an orbit and the outer one an hour, so raise the time warp
slider to watch the system move.

```go
// From examples/space: the time warp, and the focused body pinned to the
// floating origin so the scene near the camera stays precise.
settings := w.Resource[orbit.Settings]()
settings.TimeScale = float64(warp)
if fb, ok := w.Get[orbit.Body](focus); ok {
	settings.Origin = fb.Pos
}
w.Update(ctx.Delta)
```

## Choosing units

Any consistent set of units works. With `G = 1`, a star of mass 5000
and a planet at distance 55, the planet's period is 2π·√(55³/5000) ≈ 36
time units. Pick masses and distances that give the periods you want,
then set `TimeScale` to control how fast play runs.

```go
const g, starMass = 1.0, 5000.0
year := orbit.Elements{SemiMajorAxis: 55}.Period(g * starMass) // ≈ 36
w.SetResource(orbit.Settings{G: g, TimeScale: year / 10, Scale: 1})
// One lap of the inner planet every ten seconds of play.
```
