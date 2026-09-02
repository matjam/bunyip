---
title: Orbits and space
group: Simulation
order: 2
summary: celestial mechanics for any star system: Kepler orbits, N-body gravity, ships under thrust
---

The [orbit](../pkg/orbit.html) package gives a space game real
celestial mechanics without tying it to our solar system. Bodies are
masses at positions in double precision; the gravitational constant and
the units are yours to choose. Use metres, kilograms and seconds with
the real `G` and the constants in [orbit/sol](../pkg/orbit/sol.html)
for our neighbourhood, or set `G` to 1 and invent a system in game
units, as the space example does.

## Two bodies, exactly

`Elements` are the classical orbital elements (semi-major axis,
eccentricity, inclination, node, argument of periapsis, true anomaly).
`Elements.State` turns them into a position and velocity for a primary
of gravitational parameter mu (G times its mass), and `ElementsOf`
goes back. `Propagate` advances a state by any time span exactly, for
circles, ellipses, parabolas and hyperbolas alike, so a moon's position
a year from now is one call, not a million steps.

```go
el := orbit.Elements{SemiMajorAxis: 20, Eccentricity: 0.3}
s := el.State(mu)
later := orbit.Propagate(s, mu, el.Period(mu)/2) // at apoapsis
```

`Period`, `Periapsis`, `Apoapsis`, `CircularVelocity`,
`EscapeVelocity`, `VisViva`, `Hohmann` and `SphereOfInfluence` give
the numbers a player wants to see and the burns a planner needs.

## Many bodies, numerically

`Simulation` integrates bodies under their mutual gravity with a
symplectic leapfrog, which keeps the energy of a system steady over
long runs. `RK4` steps a single particle through an acceleration field,
the right tool for a ship whose engine is part of the force.

## In the ECS

Give entities an `orbit.Body` (position, velocity, mass) and the
`orbit.System`:

- entities with a `Kepler` component follow an exact orbit around their
  `Primary`, and chains work: moons around planets around a star;
- entities marked `Ship` are integrated numerically under every massive
  body's gravity plus their `Thrust`;
- everything with a `gfx.Transform` gets a scaled position written into
  it each update.

```go
ecs.SetResource(w, orbit.Settings{G: 1, TimeScale: 10, Scale: 1})
star := w.SpawnWith(orbit.Body{Mass: 5000}, gfx.Transform{})
planet := w.SpawnWith(orbit.Body{Mass: 12}, gfx.Transform{},
	orbit.Kepler{Primary: star, Elements: orbit.Elements{SemiMajorAxis: 55, Eccentricity: 0.1}})
ship := w.SpawnWith(orbit.Ship{}, orbit.Thrust{}, orbit.Body{Pos: ..., Vel: ...}, gfx.Transform{})
w.AddSystem("orbits", orbit.System)
```

`Settings.TimeScale` is the time warp. `Settings.Scale` converts
simulation distance into scene units, and `Settings.Origin` is the
floating origin: set it to the ship's or camera's position each frame
and the scene stays near zero, where float32 rendering is precise, no
matter how far the ship has flown.

`Predict` integrates a copy of a ship ahead and returns its path in
scene units for drawing; the planets move along their orbits as it
looks ahead, so the path is right even around a fast-moving world.
`PredictRelative` draws that path in the frame of a chosen body, so a
ship circling a planet shows a loop around the planet where it is now
rather than a streak across the sky. `Around` reports which body
dominates a ship and its orbital elements relative to it, for a readout;
pass its primary to `PredictRelative`.

Ships are integrated with an adaptive step: never longer than a small
fraction of the local orbital timescale, with the Kepler bodies moved
along their orbits at each step. A ship in a twenty-second orbit stays
on it at a time warp of two hundred.

## The space example

`examples/space` puts all of this together: a star, seven planets with
a dozen moons, three hundred massless asteroids in a belt (they follow
their orbits but pull on nothing, so they cost the ship's integration
nothing), and a comet on a steep, eccentric orbit. Every body's orbit is
drawn as a ring of dots from its elements, the ship's predicted path is
drawn in the frame of whatever it orbits, Tab cycles the camera's focus
(and the floating origin) through the bodies, and the scroll wheel zooms
from the ship's hull to the whole system. With `G = 1` and a star of
mass 175 the inner planet takes two minutes to go round and the outer
one an hour, so the time warp slider is how you watch the system move.

## Choosing units

Stay consistent and the equations do not care. With `G = 1`, a star of
mass 5000 and a planet at distance 55, the planet's period is
2π·√(55³/5000) ≈ 36 time units. Pick masses and distances that make the
periods you want, then set `TimeScale` for how fast it should feel.
