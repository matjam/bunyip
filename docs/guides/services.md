---
title: Game services
order: 12
summary: assets, saves, translation, random numbers, timers, sequences, tweens, grids and networking
---

These packages have no GPU or window dependency. Their examples run
under `go test`, and they work in a headless server as well as in a
game. The entity component system has its own guide,
[Entities and systems](ecs.html).

## Assets

[asset](../pkg/asset.html) resolves names across loose directories,
pack files and any `fs.FS` such as an `embed.FS`, in the order given, so
a developer's copy overrides the packed or embedded one. `Open` takes
paths; `OpenFS` takes `Dir`, `PackFile` and `FSSource` sources. One-call
loaders turn a name into an engine object: `asset.Texture`,
`asset.Font`, `asset.SDFFont`, `asset.Sound`, `asset.Music`,
`asset.Model` (with the model's buffers and images resolved through the
same files) and `asset.Tracker`. A `Loader` decodes on worker
goroutines behind a progress counter, for loading screens, and a
`Watcher` reports loose files that changed on disk, for hot reload.
`bunyip-pack` builds pack files.

## Saves and settings

[save](../pkg/save.html) writes JSON documents into the platform's data
directory (Application Support, XDG data, AppData) and replaces them
atomically, so a crash mid-write never corrupts a save. `Load` reads a
document over defaults, so new settings fields get sensible values when
a file predates them. Whole ECS worlds are saved through
`ecs.World.Save`, described in the ECS guide.

## Translation

[locale](../pkg/locale.html) holds a `Table` of messages per language,
loaded from JSON a translator edits, with `{name}` placeholders and
plural forms chosen by each language's rules. `Plural` knows the common
languages, from English's two forms to Arabic's six. A `Bundle` falls
back through languages for keys a translation lacks, `Missing` lists
what a translator still has to do, and `For` gives a `Translator` whose
`T` and `N` the game calls.

## Random numbers

[rng](../pkg/rng.html) is a seedable PCG32 generator. The same seed
gives the same sequence on every platform; `Fork` gives a subsystem its
own stream, so adding a call in one place never changes what another
produces; `State` and `Restore` put the generator in a save file.
`Pick`, `Shuffle`, `Roll` (dice notation) and `WeightedIndex` cover
the usual game needs.

## Timers, sequences and tweens

[timer](../pkg/timer.html) schedules callbacks on game time (`After`,
`Every`) and offers a pollable `Countdown`. Because time is the game's
own, a paused game stops its timers by not calling `Update`, and a
replay runs them identically.

`timer.Sequence` writes a cutscene, a turn's animation or a boss pattern
as a list of steps instead of a state machine: `Do` something, `Wait` a
second, wait `Until` a condition holds, `Run` a function each update
until it reports it is done, and `Loop` for patrols. `Skip` jumps to the
end for a player who presses through.

[tween](../pkg/tween.html) eases a value from one number to another
with the usual curves, repeats and yo-yos; `Sequence` chains tweens, and
`Of` (with `NewVec2` and `NewVec3`) tweens vectors, colours or any
value that has a blend function.

## Grids

[grid](../pkg/grid.html) is for tile games: a generic `Grid`, `AStar`
with four- or eight-way movement and per-step costs, `Dijkstra` maps
with `Downhill` for chasing and fleeing, Bresenham `Line`, shadowcasting
`FOV` and `FloodFill`.

## Networking

[network](../pkg/network.html) moves typed messages between game
instances: ordered over TCP for turn-based play, lobbies and chat, and
over UDP for real-time state. A `Registry` names the message types both
ends agree on; events arrive through `Poll` once per frame, and
`SetOnActivity` can wake a sleeping turn-based game.

`ListenTLS` and `DialTLS` encrypt TCP. `SelfSignedConfig` makes a
certificate for a LAN game together with a client configuration pinned
to it, and `Fingerprint` with `PinnedConfig` lets a player on another
machine pin the same certificate.

A UDP `Peer` has two ways to send. `Send` fires a packet that may be
lost, for state that is replaced every frame. `SendReliable` resends
until the packet is acknowledged and delivers in order, for anything
that must arrive. Every packet acknowledges what came the other way,
and `Stats` reports a link's round trip, loss and pending count. Peers
say hello and keep alive, so a `Peer` raises `Connected` and
`Disconnected` for UDP addresses (after `SetTimeout` of silence, a
goodbye, or a restart), and `Peers` lists who is there.

The remaining helpers are the standard techniques of real-time
netcode. An `Interpolator` draws other players a little behind the
newest snapshot so they move smoothly whatever the packet timing. A
`Predictor` applies the local player's inputs at once and reconciles
with the server's state by replaying the inputs the server has not yet
seen. A `History` lets a server rewind targets to where a shooter saw
them. A `Clock` estimates the server's time from pings. `EncodeDelta`
sends only the fields of a snapshot struct that changed since a
baseline; `SnapshotBuffer` picks each client's baseline from what it
last acknowledged, with `SnapshotReceiver` on the other end. `Interest`
chooses which entities are near enough to a viewer to be worth sending,
with hysteresis at the edge so nothing flickers in and out.
