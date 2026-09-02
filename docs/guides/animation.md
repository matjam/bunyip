---
title: Animation
order: 5
summary: keyframe clips for 2D sprites and 3D transforms, flipbooks, skeletons and crossfades
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
`gfx.DrawModelAnimated`. Clip selection stays on the player
(`Play(name, loop)`), so a character controller system just names the
clip it wants.

## Where the values come from

Clips are Go values, so they can be written by hand as above, built
from data (a JSON list of keys is a loop over `At`), or generated: the
animation example builds eight orbit clips from a formula. Curves also
work outside the ECS; `Sample` is a plain function, handy for camera
moves and UI transitions.
