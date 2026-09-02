---
title: Physics
order: 6
summary: rigid bodies in 2D and 3D, shapes, collisions, triggers and raycasts
---

The [phys](../pkg/phys.html) package simulates rigid bodies on the
[ECS](ecs.html) in two and three dimensions with the same ideas: a
transform says where an entity is, a body says how it moves, a collider
says what shape it has, and one system per dimension does the rest.

## Setting up

```go
ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
w.AddSystem("physics", phys.System3)

// A static floor: a collider with no body never moves.
w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(20, 0.5, 20)}})

// A crate: a dynamic body with a box collider.
w.SpawnWith(gfx.At(0, 5, 0), phys.Dynamic3(2), phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
```

2D is the same with `Settings2`, `System2`, `gfx.Transform2`, `Body2`
and `Collider2`. Screen coordinates grow downward, so 2D gravity is
usually positive, in pixels per second squared.

## Bodies

`Dynamic2` and `Dynamic3` return bodies with sensible defaults; set
`Restitution` for bounce and `Friction` for grip, and `LinearDamping`
to bleed off speed. `Kinematic2` and `Kinematic3` move by their velocity
and push others without being pushed: platforms, doors, paddles. A body
with mass zero, or an entity with only a collider, is static.

Apply forces between frames with `AddForce` and `AddTorque`, or change
velocity at once with `AddImpulse`. `GravityScale` makes a body float
or sink, `LockRotation` keeps a character upright, and `Sleeping`
freezes a body until you wake it.

## Shapes

2D: `Circle`, `Box2` and convex `Polygon2`. 3D: `Sphere` and `Box3`.
Each collider has an `Offset` from the transform, a `Trigger` flag, and
`Layers`: two colliders meet when each one's `Layer` bits appear in the
other's `Mask`, so bullets can pass through their own team.

## What touched what

The system emits `Collision2` or `Collision3` events for each pair of
colliders in contact during an update, with the point, normal and
depth, and `Trigger2` or `Trigger3` events while a trigger overlaps
something. Read them in a later system:

```go
w.AddSystem("damage", func(w *ecs.World, dt float64) {
	for _, hit := range ecs.Events[phys.Collision3](w) {
		if hit.Depth > 0.1 { /* a hard landing */ }
	}
})
```

`Raycast2` and `Raycast3` find the nearest collider along a ray, for
picking, line of sight and ground checks. Pair `gfx.ScreenRay` with
`Raycast3` to find the body under the pointer.

## How it works, and how to tune it

Each update is split into substeps. In each: velocities integrate
gravity and forces; a sweep over bounding boxes finds candidate pairs;
shapes generate contact points (circles and spheres by distance,
polygons by separating axes with face clipping, boxes by the fifteen
axis test with the incident box's vertices as the manifold); then a
sequential impulse solver iterates over the contacts, applying normal
impulses with restitution, friction impulses clamped by the normal
impulse, and a small positional correction; finally positions
integrate.

`Settings.Substeps` (default 4) trades speed for stability under fast
motion and tall stacks; `Iterations` (default 8) stiffens contacts.
Five hundred boxes step in a few milliseconds at the defaults. Keep
shapes of comparable sizes and masses within a factor of a hundred or
so, as with every impulse solver.

## Beyond rigid bodies

For spaceflight, planets and moons, use the [orbit](space.html)
package: it works at astronomical scale in double precision and writes
into the same transforms.
