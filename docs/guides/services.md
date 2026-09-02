---
title: Game services
order: 12
summary: entities, assets, saves, random numbers, timers, sequences, tweens, translation, grids and networking
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

[asset](../pkg/asset.html) resolves names across loose directories,
pack files and any `fs.FS` such as an `embed.FS`, in the order given, so
a developer's copy overrides the packed or embedded one. `Open` takes
paths; `OpenFS` takes `Dir`, `PackFile` and `FSSource` sources. One-call
loaders turn a name into an engine object: `asset.Texture`,
`asset.Font`, `asset.SDFFont`, `asset.Sound`, `asset.Music`,
`asset.Model` (with the model's buffers and images resolved through the
same files) and `asset.Tracker`. A `Loader` decodes on worker goroutines
behind a progress counter, and a `Watcher` reports loose files that
changed on disk for hot reload. `bunyip-pack` builds pack files.

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

`timer.Sequence` writes a cutscene, a turn's animation or a boss pattern
as a list of steps instead of a state machine: `Do` something, `Wait` a
second, wait `Until` a condition holds, `Run` a function each update
until it says it is done, and `Loop` for patrols. `Skip` jumps to the
end for a player who presses through.

## Text in the player's language: locale

[locale](../pkg/locale.html) holds a `Table` of messages per language,
loaded from JSON a translator edits, with `{name}` placeholders and
plural forms chosen by each language's rules (`Plural` knows the common
ones, from English's two forms to Arabic's six). A `Bundle` falls back
through languages for keys a translation lacks, `Missing` lists a
translator's to-do, and `For` gives a `Translator` whose `T` and `N`
the game calls.

## Maps: grid

[grid](../pkg/grid.html) is for tile games: a generic `Grid`, `AStar`
with four- or eight-way movement and per-step costs, `Dijkstra` maps
with `Downhill` for chasing and fleeing, Bresenham `Line`, shadowcasting
`FOV` and `FloodFill`.

## Networking: network

[network](../pkg/network.html) moves typed messages between game
instances: ordered over TCP for turn-based play, lobbies and chat, and
over UDP for real-time state. A `Registry` names the message types
both ends agree on; events arrive through `Poll` once per frame, and
`SetOnActivity` can wake a sleeping turn-based game. `ListenTLS` and
`DialTLS` encrypt TCP; `SelfSignedConfig` makes a certificate for a
LAN game with a client config pinned to it, and `Fingerprint` with
`PinnedConfig` let a player on another machine pin it too.

A UDP `Peer` has two ways to send: `Send` fires a packet that may be
lost, for state that is replaced every frame, and `SendReliable`
resends until acknowledged and delivers in order, for anything that
must arrive. Every packet acknowledges what came the other way, so the
two share a link; `Stats` reports its round trip, loss and pending
count. Peers say hello and keep alive, so a `Peer` raises `Connected`
and `Disconnected` for UDP addresses (after `SetTimeout` of silence, a
goodbye, or a restart) and `Peers` lists who is there.

For real-time play the helpers do the usual tricks: an `Interpolator`
draws other players a little behind the newest snapshot so they move
smoothly whatever the packet timing; a `Predictor` applies the local
player's inputs at once and reconciles with the server's state by
replaying the inputs it has not seen; a `History` lets a server rewind
targets to where a shooter saw them; and a `Clock` estimates the
server's time from pings. `EncodeDelta` sends only the fields of a
snapshot struct that changed since a baseline, `SnapshotBuffer` picks
each client's baseline from what it last acknowledged (with
`SnapshotReceiver` on the other end), and `Interest` chooses which
entities are near enough to a viewer to send, with hysteresis at the
edge.
