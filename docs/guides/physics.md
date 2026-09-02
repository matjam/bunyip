---
title: Physics
group: Simulation
order: 1
summary: rigid bodies in 2D and 3D: shapes, collisions, queries, joints, ragdolls and character controllers
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
`Restitution` for bounce, `Friction` for grip and `LinearDamping` to
bleed off speed. `Kinematic2` and `Kinematic3` move by their velocity
and push other bodies without being pushed: platforms, doors, paddles.
A body with zero mass, or an entity with only a collider, is static.

Apply forces between frames with `AddForce` and `AddTorque`, or change
the velocity at once with `AddImpulse`. `GravityScale` makes a body
float or sink, `LockRotation` keeps a character upright, and setting
`Sleeping` freezes a body until the game clears it again.

## Shapes

2D: `Circle`, `Box2`, convex `Polygon2`, `Capsule2`, and for terrain
`Edge2` and `Chain2` (a polyline of edges, optionally closed). 3D:
`Sphere`, `Box3`, `Capsule`, `ConvexHull` from any point cloud,
`MeshShape` for static triangle geometry such as a level or a
heightfield, and `Compound3` for parts with their own offsets and
rotations. Build a `MeshShape` with `NewMeshShape` so its triangle tree
is built once, and draw the same vertices as a `gfx` mesh.

Every pair of shapes collides. Spheres, capsules, boxes and hulls are
tested through their support functions (GJK for distance, EPA for
penetration depth), and any of them collides with a mesh's triangles.

Each collider has an `Offset` from the transform, a `Trigger` flag and
`Layers`. Two colliders meet only when each one's `Layer` bits appear
in the other's `Mask`, which is how bullets pass through their own
team.

## Collisions and triggers

The system emits a `Collision2` or `Collision3` event for each pair of
colliders in contact during an update, with the contact point, normal,
depth and impulse, and a `Trigger2` or `Trigger3` event while a trigger
overlaps something. Read them in a later system:

```go
w.AddSystem("damage", func(w *ecs.World, dt float64) {
	for _, hit := range ecs.Events[phys.Collision3](w) {
		if hit.Impulse > 50 { /* a hard landing */ }
	}
})
```

## Queries

`Raycast2` and `Raycast3` find the nearest collider along a ray, for
picking, line of sight and ground checks; `RaycastAll2` and
`RaycastAll3` return every hit in order of distance. Pair
`gfx.ScreenRay` with `Raycast3` to find the body under the pointer.
`OverlapSphere3`, `OverlapBox3` and `OverlapShape3`, with their 2D
counterparts, list what a volume touches: an explosion's victims, a
selection box. `ShapeCast2` and `ShapeCast3` sweep a shape along a
direction and report the first thing it would hit and how far along the
sweep it got. `Nearest2` and `Nearest3` find the closest collider to a
point within a radius.

## Joints

A joint is a component on its own entity that ties two bodies together,
or ties one body to a point in the world when the other side is left as
`ecs.None`. `DistanceJoint2` and `DistanceJoint3` make rods, and ropes
when given slack through `Min` and `Max`. `RevoluteJoint2` and
`HingeJoint3` are pins and door hinges. `BallJoint3` is a shoulder or a
hip. `SpringJoint2` and `SpringJoint3` have stiffness and damping.
`FixedJoint2` and `FixedJoint3` weld. Joints are solved in the same
iterations as the contacts, so a chain of hinges hangs and swings
without drifting apart.

A hinge or revolute joint measures its angle from the pose on its first
step, positive by the right-hand rule about the axis; `Angle(w)` reads
it. `MinAngle` and `MaxAngle` stop it at either end, for a door that
opens one way; both zero means unlimited. `MotorSpeed` and
`MaxMotorTorque` turn it into a motor that drives towards a speed with
bounded torque, for a wheel or a winch; a heavy load slows it and a
limit stops it.

```go
w.SpawnWith(phys.HingeJoint3{A: axle, B: wheel, AxisA: lin.V3(1, 0, 0), AxisB: lin.V3(1, 0, 0),
	MotorSpeed: 10, MaxMotorTorque: 50})
w.SpawnWith(phys.HingeJoint3{A: frame, B: door, AnchorA: hingePos, AnchorB: lin.V3(-0.5, 0, 0),
	AxisA: lin.V3(0, 1, 0), AxisB: lin.V3(0, 1, 0), MinAngle: 0, MaxAngle: 1.6})
```

`BallJoint3` pins two bodies at a point and lets them turn in every
direction. `AxisB` is the limb's axis in its own frame (local Y by
default) and `AxisA` the centre of its cone in the parent's frame (by
default, where the limb pointed on the first step). `ConeAngle` limits
how far the limb swings from that centre and `TwistAngle` how far it
turns about itself; `Angles(w)` reads both.

## Ragdolls

`NewRagdoll3(w, spec)` spawns a humanoid of eleven capsules (pelvis,
spine, head, upper arms, forearms, thighs, shins) joined by ball joints
at the waist, neck, shoulders and hips and by one-way hinges at the
elbows and knees, with limits that make it flop rather than fold. A zero
`RagdollSpec` gives a figure 1.8 units tall with a mass of 70; `Height`
scales it, and `Mass`, the bone sizes, `Position` (where its feet
stand) and `Rotation` adjust the rest. By default the parts share a
layer that collides with everything except other ragdoll parts.

The result names every part and joint (`Parts`, `Joints`,
`RagdollPelvis` and the other part constants) so a game can draw them.
`Pose` places the parts from an animated character's bones at the
moment it dies: give it the world position and rotation of each part,
where the position is the part's centre, which is the bone's origin
plus half the bone's `Length` along its rotated -Y axis. `Despawn`
removes the whole figure.

```go
r := phys.NewRagdoll3(w, phys.RagdollSpec{Position: lin.V3(0, 3, 0)})
head := r.Parts[phys.RagdollHead]
```

## Continuous collision

A small fast body can pass through a thin wall between two steps. Set
`Body.CCD` on bullets and the like. The body is then swept against the
static colliders each substep and stopped at the first one it would
have crossed, and its bounding sphere is swept against the other moving
bodies so two fast bodies meet rather than cross. The second test is
coarse, so two long thin bodies can still miss each other.

## Sleeping

When `Settings.SleepTime` is set, bodies that stay at rest for that long
go to sleep: they are neither integrated nor paired with other sleeping
bodies. A touch or an impulse wakes them. `Body.Asleep` reports the
state and `Wake` ends it early. Sleeping is off by default. A stack of
boxes at the default solver quality jitters slightly and may never
settle below the threshold; raise `Substeps` and `Iterations` for
stacks that should sleep.

## Character controllers

`CharacterController3` and `CharacterController2` move a capsule the way
players expect rather than the way physics dictates. `Move` sweeps it
along a velocity, slides it along whatever it meets, steps it up ledges
no taller than `StepHeight`, refuses slopes steeper than `MaxSlope`, and
reports `Grounded` and the `GroundNormal`. A controller is kinematic:
it pushes nothing and nothing pushes it. Give the character a trigger
collider for the things that should notice it.

## Tuning

Each update is split into substeps. In each one, velocities integrate
gravity and forces; a sweep over bounding boxes finds candidate pairs;
the shapes generate contact points; a sequential impulse solver
iterates over the contacts and joints, applying normal impulses with
restitution, friction impulses clamped by the normal impulse, and a
small positional correction; and positions integrate.

`Settings.Substeps` (default 4) trades speed for stability under fast
motion and tall stacks; `Iterations` (default 8) stiffens contacts and
joints. Five hundred boxes step in a few milliseconds at the defaults.
Keep the sizes and masses of interacting bodies within a factor of a
hundred or so of each other, as with every impulse solver.

## Beyond rigid bodies

For spaceflight, planets and moons, use the [orbit](space.html)
package. It works at astronomical scale in double precision and writes
into the same transforms.
