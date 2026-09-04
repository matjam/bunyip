---
title: Physics
group: Simulation
order: 1
summary: rigid bodies in 2D and 3D: shapes, collisions, queries, joints, ragdolls and character controllers, and the cloth, soft bodies and fluids beside them
---

The [phys](../pkg/phys.html) package simulates rigid bodies on the
[ECS](ecs.html) in two and three dimensions. Both dimensions use the
same components. A transform holds the position and rotation, a body
holds the mass and velocity, and a collider holds the shape. One system
per dimension steps the simulation.

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

`Dynamic2` and `Dynamic3` return bodies with default values. Set
`Restitution` for bounce, `Friction` for grip and `LinearDamping` to
reduce speed over time. `Kinematic2` and `Kinematic3` move by their
velocity and push other bodies without being pushed, which suits
platforms, doors and paddles. A body with zero mass, or an entity with
only a collider, is static.

To apply a force between frames, call `AddForce` or `AddTorque`. To
change the velocity at once, call `AddImpulse`. `GravityScale` scales
how strongly gravity pulls on one body, `LockRotation` stops a body
rotating, and setting `Sleeping` freezes a body until the game clears
it again.

```go
// 2D: a paddle that moves under the game's control and pushes what it meets.
paddle := w.SpawnWith(gfx.At2(700, 500), phys.Kinematic2(),
	phys.Collider2{Shape: phys.Box2{HalfW: 90, HalfH: 10}})
if b, ok := ecs.Get[phys.Body2](w, paddle); ok {
	b.Vel = lin.V2(200, 0)
}

// 3D: a bouncy crate, shoved once at spawn.
crate := phys.Dynamic3(2)
crate.Restitution, crate.Friction, crate.LinearDamping = 0.4, 0.6, 0.1
e := w.SpawnWith(gfx.At(0, 5, 0), crate,
	phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
if b, ok := ecs.Get[phys.Body3](w, e); ok {
	b.AddImpulse(lin.V3(0, 0, -6))
}
```

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
`Layers`. Two colliders collide only when each one's `Layer` bits
appear in the other's `Mask`. Use this to let a bullet pass through
bodies on its own team.

```go
// 2D: a terrain outline and a triangle that lands on it.
w.SpawnWith(gfx.Transform2{}, phys.Collider2{Shape: phys.Chain2{
	Points: []lin.Vec2{{X: 0, Y: 300}, {X: 200, Y: 260}, {X: 400, Y: 330}}}})
w.SpawnWith(gfx.At2(120, 40), phys.Dynamic2(1), phys.Collider2{
	Shape: phys.Polygon2{Points: []lin.Vec2{{X: 0, Y: -20}, {X: 17, Y: 10}, {X: -17, Y: 10}}}})

// 3D: a static level mesh, and a hammer made of two boxes on one body.
w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.NewMeshShape(vertices, indices)})
w.SpawnWith(gfx.At(0, 4, 0), phys.Dynamic3(5), phys.Collider3{
	Shape: phys.Compound3{Parts: []phys.Part3{
		{Shape: phys.Box3{Half: lin.V3(0.06, 0.5, 0.06)}},
		{Shape: phys.Box3{Half: lin.V3(0.3, 0.12, 0.12)}, Offset: lin.V3(0, 0.5, 0)},
	}},
	Layers: phys.Layers{Layer: 2, Mask: 1}})
```

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

`Raycast2` and `Raycast3` find the nearest collider along a ray, which
covers picking, line of sight and ground checks. `RaycastAll2` and
`RaycastAll3` return every hit in order of distance. Pass a
`gfx.ScreenRay` to `Raycast3` to find the body under the pointer.
`OverlapSphere3`, `OverlapBox3` and `OverlapShape3`, and their 2D
counterparts, list every collider inside a volume, which covers
explosion radii and selection boxes. `ShapeCast2` and `ShapeCast3`
sweep a shape along a direction and report the first collider it would
hit and how far along the sweep it got. `Nearest2` and `Nearest3` find
the closest collider to a point within a radius.

`SignedDistance2` and `SignedDistance3` measure a point against one
shape placed in the world, without touching the entity world at all.
They return the distance to the surface, negative inside, and the
outward normal there, which is what code that pushes points out of
solids needs. They understand `Sphere`, `Box3`, `Capsule` and compounds
of those in 3D, and `Circle`, `Box2`, `Polygon2` and `Capsule2` in 2D,
and report false for the rest.

A game that casts the same ray every frame can avoid the result slice by
calling `RaycastAll2Into` or `RaycastAll3Into`, which append to a slice
the caller keeps and hands back truncated with `[:0]`:

```go
g.hits = phys.RaycastAll3Into(g.hits[:0], w, ray, 0)
```

```go
// The body under the pointer.
ray := gr.ScreenRay(mx, my)
if hit, ok := phys.Raycast3(w, phys.Ray3{Origin: ray.Origin, Dir: ray.Dir.Mul(200)}, 0); ok {
	hover = hit.Entity
}

// Everything an explosion caught, pushed away from the blast.
for _, h := range phys.OverlapSphere3(w, blast, 5, 0) {
	if b, ok := ecs.Get[phys.Body3](w, h.Entity); ok {
		b.AddImpulse(h.Point.Sub(blast).Norm().Mul(20))
	}
}

// A 2D ground check: sweep the player's circle a little way down.
_, grounded := phys.ShapeCast2(w, phys.Circle{Radius: 12}, pos, 0, lin.V2(0, 4), 0)
```

## Joints

A joint is a component on its own entity that constrains two bodies. To
constrain one body to a point in the world instead, leave the other
side as `ecs.None`. `DistanceJoint2` and `DistanceJoint3` hold two
bodies a fixed distance apart, or within a range when given `Min` and
`Max`. `RevoluteJoint2` and `HingeJoint3` allow rotation about one
axis. `BallJoint3` allows rotation about any axis through a point.
`SpringJoint2` and `SpringJoint3` pull towards a rest length with
stiffness and damping. `FixedJoint2` and `FixedJoint3` remove all
relative motion. Joints are solved in the same iterations as the
contacts, so a chain of hinges holds together.

```go
// 2D: a crate on a rope from a fixed point, and a wheel sprung to a cart.
w.SpawnWith(phys.DistanceJoint2{A: crate, B: ecs.None,
	AnchorB: lin.V2(400, 100), Max: 150})
w.SpawnWith(phys.SpringJoint2{A: cart, B: wheel,
	RestLength: 40, Stiffness: 30, Damping: 4})
```

A hinge or revolute joint measures its angle from the pose on its first
step, positive by the right-hand rule about the axis. `Angle(w)` reads
that angle. `MinAngle` and `MaxAngle` stop the joint at either end, so
a door opens one way only; both zero means unlimited. `MotorSpeed` and
`MaxMotorTorque` drive the joint towards a speed with bounded torque,
for a wheel or a winch. A heavy load slows the motor and a limit stops
it.

```go
w.SpawnWith(phys.HingeJoint3{A: axle, B: wheel, AxisA: lin.V3(1, 0, 0), AxisB: lin.V3(1, 0, 0),
	MotorSpeed: 10, MaxMotorTorque: 50})
w.SpawnWith(phys.HingeJoint3{A: frame, B: door, AnchorA: hingePos, AnchorB: lin.V3(-0.5, 0, 0),
	AxisA: lin.V3(0, 1, 0), AxisB: lin.V3(0, 1, 0), MinAngle: 0, MaxAngle: 1.6})
```

`BallJoint3` holds two bodies together at a point and allows rotation
in every direction. `AxisB` is the limb's axis in its own frame (local
Y by default) and `AxisA` is the centre of its cone in the parent's
frame (by default, the direction the limb pointed on the first step).
`ConeAngle` limits how far the limb swings from that centre and
`TwistAngle` limits how far it turns about itself. `Angles(w)` reads
both.

## Ragdolls

`NewRagdoll3(w, spec)` spawns a humanoid of eleven capsules (pelvis,
spine, head, upper arms, forearms, thighs, shins) joined by ball joints
at the waist, neck, shoulders and hips and by one-way hinges at the
elbows and knees. The joint limits stop the elbows and knees bending
backwards. A zero
`RagdollSpec` gives a figure 1.8 units tall with a mass of 70; `Height`
scales it, and `Mass`, the bone sizes, `Position` (where its feet
stand) and `Rotation` adjust the rest. By default the parts share a
layer that collides with everything except other ragdoll parts.

The result names every part and joint (`Parts`, `Joints`,
`RagdollPelvis` and the other part constants) so a game can draw them,
and `Bones` records each part's size as built, so the mesh for a limb
can be scaled to fit its collider.
`Pose` places the parts to match an animated character's bones, which
is how a game hands over from an animation to the ragdoll. Give it the
world position and rotation of each part. The position is the part's
centre, which is the bone's origin plus half the bone's `Length` along
its rotated -Y axis. `Despawn` removes the whole figure.

```go
r := phys.NewRagdoll3(w, phys.RagdollSpec{Position: lin.V3(0, 3, 0)})
head := r.Parts[phys.RagdollHead]
```

## Continuous collision

A small fast body can pass through a thin wall between two steps. Set
`Body.CCD` on bullets and other fast bodies to prevent that. The body
is then swept against the
static colliders each substep and stopped at the first one it would
have crossed, and its bounding sphere is swept against the other moving
bodies so two fast bodies meet rather than cross. The second test is
coarse, so two long thin bodies can still miss each other.

```go
bullet := phys.Dynamic3(0.02)
bullet.CCD = true
bullet.GravityScale = 0.1
bullet.Vel = lin.V3(0, 0, -300)
w.SpawnWith(gfx.At(0, 1.5, 0), bullet,
	phys.Collider3{Shape: phys.Sphere{Radius: 0.02}})
```

## Sleeping

When `Settings.SleepTime` is set, bodies that stay at rest for that long
go to sleep. A sleeping body is neither integrated nor paired with other
sleeping bodies. A contact or an impulse wakes it. `Body.Asleep` reports
the state and `Wake` ends it early. Sleeping is off by default. A body
counts as at rest while it moves slower than `Settings.SleepThreshold`,
in units and radians per second. A stack of boxes at the default solver
quality jitters slightly and may never settle below the threshold;
raise `Substeps` and `Iterations` for stacks that should sleep, or
raise the threshold a little.

```go
ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0),
	Substeps: 8, Iterations: 16, SleepTime: 0.5})

// How much of the world has settled, for a debug readout.
resting := 0
ecs.Each(w, func(e ecs.Entity, b *phys.Body3) {
	if b.Asleep() {
		resting++
	}
})
if b, ok := ecs.Get[phys.Body3](w, crate); ok {
	b.Wake() // the player kicked it
}
```

## Character controllers

`CharacterController3` and `CharacterController2` move a capsule under
direct control rather than through the solver. `Move` sweeps the
capsule along a velocity, slides it along whatever it meets, steps it
up ledges no taller than `StepHeight`, refuses slopes steeper than
`MaxSlope`, and reports `Grounded` and the `GroundNormal`. A controller
is kinematic, so it pushes nothing and nothing pushes it. To let other
entities detect the character, give it a trigger collider.

```go
hero := w.SpawnWith(gfx.At(1, 3.5, -8))
ctrl := phys.CharacterController3{Radius: 0.35, HalfHeight: 0.45,
	StepHeight: 0.45, MaxSlope: 50}

// Each update: walk, and fall until the sweep finds ground.
vel := lin.V3(2.5*dir, -6, 0)
ctrl.Move(w, hero, vel, float32(ctx.Delta))
if ctrl.Grounded && jump {
	// launch, then integrate the vertical velocity yourself
}
```

## Tuning

Each update is split into substeps. In each substep, velocities
integrate gravity and forces. A sweep over bounding boxes finds
candidate pairs. The shapes generate contact points. A sequential
impulse solver iterates over the contacts and joints, applying normal
impulses with restitution, friction impulses clamped by the normal
impulse, and a small positional correction. Positions then integrate.

`Settings.Substeps` (default 4) trades speed for stability under fast
motion and tall stacks; `Iterations` (default 8) stiffens contacts and
joints. Five hundred boxes step in a few milliseconds at the defaults.
Keep the sizes and masses of interacting bodies within a factor of a
hundred or so of each other, as with every impulse solver.

```go
// 2D at pixel scale, with a stiffer solver than the defaults.
ecs.SetResource(w, phys.Settings2{Gravity: lin.V2(0, 900), Substeps: 6, Iterations: 12})

// Measure the cost of the step.
step := ctx.Profile("physics")
w.Update(ctx.Delta)
step.End()
```

## Soft bodies

Rigid bodies keep their shape. For things that bend, squash and flow,
the [phys/soft](../pkg/phys/soft.html) package simulates particles held
together by constraints: `Cloth` for sheets, `SoftBody3` for a closed
mesh that keeps its volume, and `Fluid2` for liquid in the plane. All
three are components stepped by one system, `soft.System`, on the same
world as the rigid bodies. Register it after `System3`, so soft bodies
see where the rigid ones ended the update.

```go
ecs.SetResource(w, soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
w.AddSystem("physics", phys.System3)
w.AddSystem("soft", soft.System)
```

A zero `Gravity3` takes the gravity from the `phys.Settings3` resource,
and a zero `Gravity2` from `phys.Settings2`, so rigid and soft bodies
fall together without saying it twice. `Ground` turns on a floor plane
at `GroundY` for scenes with no collider under them.

Particles collide with the static and kinematic colliders already in
the world, through the signed-distance queries above: spheres, boxes,
capsules and compounds in 3D, and circles, boxes, polygons and capsules
in 2D. They do not push rigid bodies back, and dynamic bodies are
ignored.

### Cloth

`NewCloth` builds a rectangular sheet of particles: distance
constraints along the edges, diagonals across each cell, and a bending
constraint across each pair of edges in line. Pinned particles hang the
sheet up, and `Wind` pushes each cell by the air blowing through it, so
a sheet edge-on to the wind is barely moved.

```go
pins := []int{0, 1, 2, 3} // the top-left corner, held
flag := w.SpawnWith(soft.NewCloth(soft.ClothSpec{
	Width: 26, Height: 16, Spacing: 0.14, Mass: 0.4,
	Origin: lin.V3(-2, 3.6, 0), Pinned: pins, Wind: lin.V3(0, 0, 5),
}))
```

`Pin`, `Free` and `Move` change what is held while the game runs, which
is how a cape follows a running character. `Positions` and
`Velocities` are the particles themselves.

### A mesh that follows

Cloth and soft bodies are drawn by keeping a `gfx.Mesh` in step with
their particles. `NewMesh` uploads a mesh shaped like the body, and
`UpdateMesh` writes the positions and recomputes the normals each
frame. Particle positions are world space, so the mesh is drawn with an
identity matrix and needs no transform. Give cloth a `DoubleSided`
material, because it is seen from both sides.

```go
c, _ := ecs.Get[soft.Cloth](w, flag)
mesh, err := c.NewMesh(ctx.Gfx) // in Init; Destroy it in Shutdown

// In Draw, after the world has stepped.
c.UpdateMesh(mesh)
ctx.Gfx.DrawMesh(mesh, gfx.Material{BaseColor: gfx.RGB(220, 60, 70), DoubleSided: true}, lin.Identity())
```

### Volumetric soft bodies

`NewSoftBody3` takes a closed triangle mesh, from `gfx.CubeMesh`,
`gfx.SphereMesh`, `gfx.TorusMesh` or a loaded glTF model, welds the
vertices that share a position into particles, and holds them with
constraints along the surface edges, one constraint on the enclosed
volume, and shape matching that pulls the body back toward its original
shape rotated to where it is now. A body resting under gravity keeps
its volume within a few percent.

```go
cv, ci := gfx.CubeMesh()
jelly := w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{
	Vertices: cv, Indices: ci, Scale: 1.4, Position: lin.V3(0, 2.4, 0),
	Mass: 3, Compliance: 0.001, ShapeMatch: 0.04,
}))

b, _ := ecs.Get[soft.SoftBody3](w, jelly)
b.AddImpulse(lin.V3(-9, 16, 0)) // kicked
b.Pressure = 1.3                // inflated
```

### Fluids

`Fluid2` is position-based fluids: a density constraint over a spatial
hash keeps the liquid incompressible, a small push at close range stops
it clumping, and viscosity pulls neighbours toward a shared velocity.
`Bounds` is the tank, and the 2D colliders in the world are obstacles
in it. The game draws the particles itself, from `Positions`, as
sprites or circles.

```go
f := soft.NewFluid2(soft.Fluid2Spec{Bounds: tank, Spacing: 7})
f.Fill(lin.Rect{X: tank.X + 8, Y: tank.Y + 8, W: tank.W/2, H: tank.H - 16})
w.SpawnWith(f)

// In Draw.
for i, p := range f.Positions() {
	shade := lin.Clamp(f.Density(i)/f.RestDensity(), 0, 1)
	ctx.Gfx.Draw(drop, gfx.Sprite{Pos: p, Size: lin.V2(16, 16), Color: water(shade)})
}
```

A fluid keeps its own `Substeps`, one by default, because its density
solve is a whole-step pressure solve: splitting it finer leaves the
same residual in a shorter step, and the velocity read back from that
is noise. The `Substeps` in the world settings belongs to cloth and
soft bodies.

### Tuning the soft solver

Constraint stiffness is compliance, in metres per newton: zero is
rigid, larger is softer, and it does not drift with the substep or
iteration count. `Settings.Substeps` (default 4) and `Iterations`
(default 4) trade time for stiffness; cloth is stiffer for the same
work with more substeps and fewer iterations than the other way around.
A thousand-particle sheet and two thousand fluid particles each step in
a few milliseconds, and a step allocates nothing.

The `examples/softbody` program puts all three together:
a flag on a pole, a jelly cube beside a rigid crate, and a tank of
fluid in the corner of the screen.

## Orbital mechanics

For spaceflight, planets and moons, use the [orbit](space.html)
package. It works at astronomical scale in double precision and writes
into the same transforms.
