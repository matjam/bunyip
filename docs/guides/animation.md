---
title: Animation
group: Graphics
order: 4
summary: keyframe clips for 2D sprites and 3D transforms, flipbooks, skeletons with crossfades, events, root motion, layers, blend spaces, IK and morph targets
---

The [anim](../pkg/anim.html) package animates entities in the
[ECS](ecs.html). It has three layers, each useful on its own: curves
that interpolate keyframes, clips of tracks that write curves into
components, and a `Player` component plus one system that play clips,
sprite-sheet flipbooks and glTF skeletons every update.

## Curves

A curve is a list of keys, each a time, a value and an optional easing
into that key. Curves exist for numbers, 2D and 3D vectors, rotations
(interpolated along the shortest arc) and colours; `NewCurve` takes any
type and its own interpolation function. `At` and `AtEased` make keys
of any type; `Num` and `NumEased` are their number-typed forms, since a
bare `0` would otherwise be an int.

```go
height := anim.Floats(
	anim.Num(0, 0),
	anim.NumEased(0.5, 120, tween.OutQuad),
	anim.NumEased(1.0, 0, tween.OutBounce),
)
height.Sample(0.25) // 90, on the way up
```

## Tracks and clips

A track applies a curve to one property of one component. Tracks exist
for a 3D `gfx.Transform` (`Position`, `Rotation`, `Scale`) and for a 2D
`gfx.Sprite` (`Position2`, `Size2`, `Rotation2`, `Tint`), and
`Property` animates any field of any component through a getter and a
setter, with no reflection:

```go
anim.Property(anim.Floats(anim.Num(0, 1), anim.Num(0.3, 0)),
	func(l *Light) float32 { return l.Intensity },
	func(l *Light, v float32) { l.Intensity = v })
```

A clip bundles tracks with a loop mode: `Once` stops at the end and
reports it, `Loop` starts over, `PingPong` runs back and forth.

```go
jump := anim.NewClip("jump", anim.Once,
	anim.Position(anim.Vec3s(
		anim.At(0, lin.V3(0, 0.5, 0)),
		anim.AtEased(0.4, lin.V3(0, 3, 0), tween.OutQuad),
		anim.AtEased(0.8, lin.V3(0, 0.5, 0), tween.InQuad),
	)),
	anim.Scale(anim.Vec3s(
		anim.At(0, lin.V3(1.3, 0.7, 1.3)),
		anim.At(0.2, lin.V3(0.8, 1.4, 0.8)),
		anim.At(1, lin.V3(1, 1, 1)),
	)),
)
```

The same clip type animates a sprite: swap `Position` for `Position2`
and the vectors for 2D ones. A clip built once can play on any number of
entities.

## Playing

Give an entity a `Player` component (or let `PlayerOf` add one), play a
clip, and register `anim.System` on the world after the systems that
decide what plays and before drawing:

```go
hero := w.SpawnWith(gfx.Transform{}, anim.Player{})
anim.PlayerOf(w, hero).Play(idle)
w.AddSystem("anim", anim.System)
```

`CrossFade(clip, seconds)` starts a clip while the previous one blends
out, so a jump does not snap out of the idle pose. `Speed` scales
playback, `Stop` freezes it, and `Progress` reports where it is. The
same `Player` works for a 2D entity; only the tracks differ.

## Finishing and sequencing

When a `Once` clip ends the system emits `anim.Finished` with the
entity and the clip. To chain animations, add a system that reads the
event: land, then fade back to idle; explode, then despawn.

```go
w.AddSystem("return", func(w *ecs.World, dt float64) {
	for _, ev := range ecs.Events[anim.Finished](w) {
		anim.PlayerOf(w, ev.Entity).CrossFade(idle, 0.3)
	}
})
```

## Flipbooks

A `Flipbook` component plays sprite-sheet frames into the entity's
`gfx.Sprite` by rewriting its texture window each update. It holds a
sheet, the frame indices, a rate and whether to loop. A finished
non-looping flipbook also emits `Finished`.

```go
w.SpawnWith(gfx.Sprite{Size: lin.V2(64, 64), Color: gfx.White}, spriteTexture{tex},
	anim.Flipbook{Sheet: gfx.NewSheet(tex, 16, 16), Frames: []int{0, 1, 2, 3}, FPS: 8, Loop: true})
```

## Skeletons

A `Skeleton` component wraps a `gfx.AnimPlayer` for a glTF model's
clips; the system advances it, and the entity draws with
`gfx.DrawModelAnimated`. Clip selection stays on the player, so a
character controller system names the clip to play. `Play(name, loop)`
snaps to a clip, `CrossFade(name, loop, seconds)` blends into it from
whatever is playing, and `Finished` reports when a one-shot clip has
ended.

```go
player := model.NewAnimPlayer()
player.Play("idle", true)
hero := w.SpawnWith(gfx.Transform{}, anim.Skeleton{Player: player})
// later, from the controller
player.CrossFade("run", true, 0.2)
```

The player is also usable on its own, without the ECS: call `Advance`
with the frame's seconds and draw.

## Animation events

To mark a moment in a clip, call `AddEvent(clip, time, name)`: a
footstep sound, the frame a punch connects, the moment a spell spawns
its effect. Every `Advance` reports the events playback crossed,
through `Events` afterwards or `OnEvent` as they happen. An event fires
on every loop, on the outgoing clip of a crossfade and on layers, so a
walk's footsteps keep landing while it fades into a run. Through
the `Skeleton` component the same events arrive as `anim.SkeletonEvent`
with the entity, beside `Finished`.

```go
player.AddEvent("walk", 0.3, "step")
player.AddEvent("walk", 0.8, "step")
w.AddSystem("footsteps", func(w *ecs.World, dt float64) {
	for _, ev := range ecs.Events[anim.SkeletonEvent](w) {
		if ev.Event.Name == "step" {
			mixer.Play(footstep, audio.PlayOptions{})
		}
	}
})
```

## Root motion

A walk cycle authored in place slides the character across the floor
without moving the entity, so collisions and the camera do not follow
it. `SetRootMotion("Hips")` takes that movement out of the pose and
reports it instead. `RootMotion` returns how far the root moved and how
far it turned about +Y during the last `Advance`, in model space,
blended through crossfades and added up across a loop point. The
`Skeleton` component applies it to the entity's `gfx.Transform`
automatically; set `KeepRootMotion` to read it yourself, for a body
that physics moves:

```go
delta, yaw := player.RootMotion()
body.Velocity = tr.Rotation.Rotate(delta).Mul(1 / float32(dt))
tr.Rotation = lin.AxisAngle(lin.V3(0, 1, 0), yaw).Mul(tr.Rotation)
```

The root's translation and yaw are held at the rest pose; pitch and
roll, and every other node, animate as before. Vertical movement is
reported too, so a jump's rise comes through the delta.

## Layers and masks

A layer plays a second clip over part of the skeleton: a wave over the
arms while the legs keep walking, a flinch over the spine. A mask lists
the nodes the layer applies to; `Model.MaskSubtree("Spine1")` takes a
node and everything under it,
`Model.MaskNodes` exactly the named nodes, and `nil` means the whole
skeleton.

```go
wave := player.Layer("wave", 1, model.MaskSubtree("RightShoulder"))
wave.Loop = false // hold the last frame when it ends; RemoveLayer takes it off
```

Layers blend in order after the main clip and its crossfade. By default
a layer replaces the pose of its nodes, scaled by `Weight`; with
`Additive` set it adds the clip's difference from the rest pose to
whatever is underneath, which is how a breathing loop or a recoil plays
over any base clip. Change `Weight` over time to fade a layer in and
out; `Play` and `CrossFade` leave layers alone.

## Blend spaces

A blend space places clips along a parameter and mixes the ones around
its current value. Use one where a single clip will not do: the speed
the controller asks for falls between a walk and a run, and a strafe is
a mix of forward and sideways. A blend space is plain data with JSON
tags, so a game can build one in code or load it from a file beside the
model.

A `BlendSpace1D` places clips along one parameter: idle at 0, walk at
1, run at 2. At 1.5 the walk and the run each get half the pose; past
the ends the nearest clip plays alone.

```go
locomotion := &anim.BlendSpace1D{Parameter: "speed", Clips: []anim.BlendPoint1D{
	{Clip: "idle", At: 0}, {Clip: "walk", At: 1}, {Clip: "run", At: 2},
}}
```

A `BlendSpace2D` places clips at points in a plane of two parameters,
for a strafe set read from the velocity: idle at the centre, forward,
back, left and right around it. Weights are gradient bands, so a clip
at the current point plays alone, a point on the line between two clips
blends them linearly, and clips drop out as the point moves past them.

```go
strafe := &anim.BlendSpace2D{X: "vx", Y: "vy", Clips: []anim.BlendPoint2D{
	{Clip: "idle", At: lin.V2(0, 0)}, {Clip: "forward", At: lin.V2(0, 1)}, {Clip: "back", At: lin.V2(0, -1)},
	{Clip: "left", At: lin.V2(-1, 0)}, {Clip: "right", At: lin.V2(1, 0)},
}}
```

A `BlendTree` composes them: a node is a clip, a 1D or 2D space, or
children placed along a parameter and mixed like a 1D space. A crouch
amount fading a standing locomotion space into a crouched one is a tree
of two children.

```go
tree := &anim.BlendTree{Parameter: "crouch", Children: []anim.BlendChild{
	{At: 0, Tree: anim.BlendTree{Space1D: locomotion}},
	{At: 1, Tree: anim.BlendTree{Space1D: crouched}},
}}
```

A `Blend` plays a space or tree on a `gfx.AnimPlayer`. It holds the
parameters, evaluates the tree every update and keeps the mixed clips in
step: all of them run at one phase of their own length, and the phase
advances at the rate of the blended cycle, so a walk's and a run's feet
land together instead of sliding. Clips in a blend loop. The `Skeleton`
component drives a `Blend` set on it from the ECS, with `SetParameter`
to feed it; on its own, call `Advance` in place of the player's.

```go
hero := w.SpawnWith(gfx.Transform{}, anim.Skeleton{Player: player, Blend: anim.NewBlend(tree)})
// from the controller, each update
skel, _ := ecs.Get[anim.Skeleton](w, hero)
skel.SetParameter("speed", velocity.Len())
skel.SetParameter("crouch", crouchAmount)
```

Below `Blend` is the player's `SetBlend`, which plays a list of clips
with weights and times in place of the main clip; events fire and root
motion accrues for every clip in the blend by its weight, and layers
play over it as over a clip. `Play` and `CrossFade` drop a blend, so a
jump from a locomotion blend snaps into its clip; when a game needs the
blend to fade out it can keep a second player in the blend and mix the
poses itself.

## Inverse kinematics and node overrides

`PostPose` runs after the clips are blended and the pose is built, and
before the joint matrices are made. Use it to plant a foot on uneven
ground, reach a hand to a handle or turn a head. `SolveTwoBoneIK` takes
three node indices (hip, knee, foot), a target and a pole point the
middle joint bends towards; `LookAtNode` turns a node so its forward
axis faces a point, within an angle limit. Both are built on the
solvers `TwoBoneIK` and `LookAt`, plain functions over positions and
directions that work outside the ECS as well.

```go
hip, knee, foot := model.NodeIndex("LeftUpLeg"), model.NodeIndex("LeftLeg"), model.NodeIndex("LeftFoot")
head := model.NodeIndex("Head")
player.PostPose = func(p *gfx.AnimPlayer) {
	anim.SolveTwoBoneIK(p, hip, knee, foot, groundUnderLeftFoot, kneeForward)
	anim.LookAtNode(p, head, lin.V3(0, 0, 1), playerPosition, lin.Radians(70))
}
```

Below those helpers are the player's node overrides, usable from
`PostPose` or after `Advance`: `NodePosition` and `NodeRotation` read a
node in model space, `NodeLocal` and `SetNodeLocal` read and replace
its transform relative to its parent, `SetNodeRotation` sets only the
rotation, and `RotateNode` turns a node by a model-space rotation about
its own position so its children follow. An override lasts until the
next `Advance` samples the clips again, so set it every frame; while
nothing plays, it stays.

## Morph targets

Blend shapes in a glTF file (a smile, a blink, a bent leaf) load as
morph targets with their default weights, and a clip's `weights`
channel animates them like any other. The player holds the weights in
its pose and `DrawModelAnimated` blends them in; `SetMorphWeights` on
the player holds weights that no clip is driving, for an expression
chosen by the game, and `Model.SetMorphWeights` blends a model that is
drawn without a player. `Model.MorphTargets` lists a node's target
names.

```go
face := model.NodeIndex("Face")
smile := slices.Index(model.MorphTargets(face), "smile")
weights := make([]float32, len(model.MorphTargets(face)))
weights[smile] = 0.8
player.SetMorphWeights(face, weights)
```

Blending runs on the CPU: when a node's weights change, the mesh's rest
vertices plus each target with a non-zero weight are summed and the
result uploaded, one pass over the vertices per active target and one
upload per changed mesh per frame. Unchanged weights cost nothing. That
is fine for faces and props with a few thousand vertices; a crowd of
morphing characters needs a GPU path, which the engine does not have
yet. Each instance of a morphing mesh gets its own copy on load, so two
characters with the same face can show different expressions.

## Where the values come from

Clips are Go values, so they can be written by hand as above, built
from data (a JSON list of keys is a loop over `At`), or generated: the
animation example builds eight orbit clips from a formula. Blend spaces
and trees decode straight from JSON. Curves also work outside the ECS;
`Sample` is a plain function, handy for camera moves and UI
transitions.
