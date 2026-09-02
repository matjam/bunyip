---
title: Game services
order: 9
summary: entities, assets, saves, random numbers, timers, tweens, grids and networking
---

These packages have no GPU or window dependency, so their examples run
under `go test` and they work in a headless server just as well as in a
game.

## Entities: ecs

[ecs](../pkg/ecs.html) is the entity component system, covered in its
own guide, [Entities and systems](ecs.html). Entities are generational
handles, components are plain structs in dense per-type columns,
queries walk them without lookups, and systems, resources and events
structure the game loop. `SetParent` builds a hierarchy and
`WorldMatrix` composes `gfx.Transform` components down the tree.

## Files: asset

[asset](../pkg/asset.html) resolves names across loose directories and
pack files, directories first so a developer's copy overrides the packed
one. A `Loader` decodes on worker goroutines behind a progress counter,
and a `Watcher` reports loose files that changed on disk for hot reload.
`bunyip-pack` builds pack files.

## Saves and settings: save

[save](../pkg/save.html) writes JSON documents into the platform's data
directory (Application Support, XDG data, AppData) and replaces them
atomically. `Load` reads a document over defaults, so new settings fields
get sensible values when a file predates them.

## Randomness: rng

[rng](../pkg/rng.html) is a seedable PCG32. The same seed gives the same
sequence everywhere; `Fork` gives a subsystem its own stream; `State`
and `Restore` put the generator in a save file. `Pick`, `Shuffle`,
`Roll` and `WeightedIndex` cover the usual game needs.

## Time: timer and tween

[timer](../pkg/timer.html) schedules callbacks on game time (`After`,
`Every`) and offers a pollable `Countdown`. [tween](../pkg/tween.html)
eases a value from one number to another with the usual curves, repeats
and yo-yos, and `Sequence` chains tweens.

## Maps: grid

[grid](../pkg/grid.html) is for tile games: a generic `Grid`, `AStar`
with four- or eight-way movement and per-step costs, `Dijkstra` maps
with `Downhill` for chasing and fleeing, Bresenham `Line`, shadowcasting
`FOV` and `FloodFill`.

## Networking: network

[network](../pkg/network.html) moves typed messages between game
instances: ordered over TCP for turn-based play, lobbies and chat, and
unordered over UDP for real-time state. A `Registry` names the message
types both ends agree on; events arrive through `Poll` once per frame,
and `SetOnActivity` can wake a sleeping turn-based game.
