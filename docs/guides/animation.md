---
title: Animation
order: 5
summary: keyframe clips for 2D sprites and 3D transforms, flipbooks, skeletons with crossfades, events, root motion, layers, IK and morph targets
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
entity and the clip. A small system that listens for it is how
animations chain: land, then fade back to idle; explode, then despawn.

```go
w.AddSystem("return", func(w *ecs.World, dt float64) {
	for _, ev := range ecs.Events[anim.Finished](w) {
		anim.PlayerOf(w, ev.Entity).CrossFade(idle, 0.3)
	}
})
```

## Flipbooks

A `Flipbook` component plays sprite-sheet frames into the entity's
`gfx.Sprite` by rewriting its texture window each update: a sheet, the
frame indices, a rate and whether to loop. A finished non-looping
flipbook also emits `Finished`.

```go
w.SpawnWith(gfx.Sprite{Size: lin.V2(64, 64), Color: gfx.White}, spriteTexture{tex},
	anim.Flipbook{Sheet: gfx.NewSheet(tex, 16, 16), Frames: []int{0, 1, 2, 3}, FPS: 8, Loop: true})
```

## Skeletons

A `Skeleton` component wraps a `gfx.AnimPlayer` for a glTF model's
clips; the system advances it, and the entity draws with
`gfx.DrawModelAnimated`. Clip selection stays on the player, so a
character controller system just names the clip it wants:
`Play(name, loop)` snaps to a clip, `CrossFade(name, loop, seconds)`
blends into it from whatever is playing, `Finished` says when a
one-shot clip has ended.

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

A footstep sound, the frame a punch connects, the moment a spell spawns
its effect: `AddEvent(clip, time, name)` marks such a moment in a clip,
and every `Advance` reports the events playback crossed, through
`Events` afterwards or `OnEvent` as they happen. An event fires on
every loop, and on the outgoing clip of a crossfade and on layers too,
so a walk's footsteps keep landing while it fades into a run. Through
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
without moving the entity, so collisions and the camera lose track of
it. With `SetRootMotion("Hips")` the player takes that movement out of
the pose and hands it over: `RootMotion` returns how far the root moved
and how far it turned about +Y during the last `Advance`, in model
space, blended through crossfades and added up across a loop point. The
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
arms while the legs keep walking, a flinch over the spine. A mask says
which nodes the layer owns; `Model.MaskSubtree("Spine1")` takes a node
and everything under it, `Model.MaskNodes` exactly the named nodes, and
`nil` means the whole skeleton.

```go
wave := player.Layer("wave", 1, model.MaskSubtree("RightShoulder"))
wave.Loop = false // hold the last frame when it ends; RemoveLayer takes it off
```

Layers blend in order after the main clip and its crossfade. By default
a layer replaces the pose of its nodes, scaled by `Weight`; with
`Additive` set it adds the clip's difference from the rest pose to
whatever is underneath, which is how a breathing loop or a recoil goes
over any base clip. Change `Weight` over time to fade a layer in and
out; `Play` and `CrossFade` leave layers alone.

## Inverse kinematics and node overrides

After the clips are blended, `PostPose` runs with the pose built and
before the joint matrices are made: the place to plant a foot on uneven
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

Under them are the player's node overrides, usable directly from
`PostPose` or after `Advance`: `NodePosition` and `NodeRotation` read a
node in model space, `NodeLocal` and `SetNodeLocal` read and replace
its transform relative to its parent, `SetNodeRotation` sets just the
rotation, and `RotateNode` turns a node by a model-space rotation about
its own position so its children follow. An override lasts until the
next `Advance` samples the clips again, so set it every frame; while
nothing plays, it stays.

## Morph targets

Blend shapes in a glTF file (a smile, a blink, a bent leaf) load as
morph targets with their default weights, and a clip's `weights`
channel animates them like any other. The player carries the weights in
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
morphing characters would want a GPU path, which the engine does not
have yet. Each instance of a morphing mesh gets its own copy on load,
so two characters with the same face can pull different expressions.

## Where the values come from

Clips are Go values, so they can be written by hand as above, built
from data (a JSON list of keys is a loop over `At`), or generated: the
animation example builds eight orbit clips from a formula. Curves also
work outside the ECS; `Sample` is a plain function, handy for camera
moves and UI transitions.
