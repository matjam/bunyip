---
title: Game services
group: Engine
order: 4
summary: assets, saves, translation, random numbers, timers, sequences, tweens, grids and networking
---

The data and simulation helpers work in headless servers as well as in
games. Asset file access and CPU decoding do not open a window; asset
loaders that upload textures, fonts or models need a graphics context
and must run on its rendering goroutine. The entity component system has its own guide,
[Entities and systems](ecs.html).

## Assets

[asset](../pkg/asset.html) resolves names across loose directories,
pack files and any `fs.FS` such as an `embed.FS`, in the order given, so
a developer's copy overrides the packed or embedded one. `Open` takes
paths; `OpenFS` takes `Dir`, `PackFile` and `FSSource` sources. One-call
loaders turn a name into an engine object: `asset.Texture`,
`asset.Atlas` (the JSON and the image it names, from the same
directory), `asset.Font`, `asset.SDFFont`, `asset.Sound`, `asset.Music`,
`asset.Model` (with the model's buffers and images resolved through the
same files), `asset.Tracker`, `asset.Emitter` (a `particle.Emitter`
saved as JSON, with the texture its `TextureName` asks for), and
`asset.Scene` and `asset.Prefab` for
the ECS documents described in the
[ECS guide](ecs.html). A `Loader` decodes on worker
goroutines behind a progress counter, for loading screens, a `Watcher`
reports loose files that changed on disk, and a `Reloader` swaps those
files' textures and shaders in place while the game runs.
`bunyip-pack` builds pack files.

Every one-call loader and `NewLoader` accepts a standard `fs.FS` directly:
`asset.Image(embeddedAssets, "sprites/hero.png")` needs no asset wrapper.
Use `asset.Open` or `OpenFS` when combining sources. Their returned `FS`
supports `fs.ReadFile`, `fs.ReadDir`, `fs.Sub` and `fs.WalkDir`; directories
merge while earlier files hide lower-priority names and their children.
`Open` and `ReadFile` require valid `io/fs` paths and return standard
filesystem errors. Legacy `Read` also cleans dot components and backslashes.
Only missing names fall through to the next source; permission errors
are returned. Watchers and reloaders still use `*asset.FS` to locate loose files.

```go
// Loose files first so a developer's copy wins, then the shipped pack.
fs, err := asset.Open("assets", "game.pak")
if err != nil {
	return err
}
g.fs = fs
if g.hero, err = asset.Texture(ctx.Gfx, fs, "sprites/hero.png", gfx.TextureOptions{}); err != nil {
	return err
}
if g.font, err = asset.Font(ctx.Gfx, fs, "fonts/ui.ttf", 16, gfx.FontOptions{}); err != nil {
	return err
}
```

`OpenFS` takes sources instead of paths, so an `embed.FS` can sit under a
loose directory:

```go
//go:embed assets
var embedded embed.FS

embeddedAssets, err := fs.Sub(embedded, "assets")
if err != nil {
	return err
}
files, err := asset.OpenFS(asset.Dir("assets"), asset.FSSource(embeddedAssets))
```

A `Loader` decodes CPU data in the background while a loading screen draws.
`Handle.Ready` polls, `Get` blocks, and `Progress` drives the bar:

```go
loader := asset.NewLoader(fs, 0) // 0 workers means one per core
defer loader.Close()
level := loader.Load("levels/1.json", parseLevel)
...
done, total := loader.Progress()
g.bar = float32(done) / float32(total)
if level.Ready() {
	g.level, err = level.Get()
}
```

Submission may block when the 256-entry job queue fills. `Close` stops
submission and waits for accepted work and the workers to finish, so the
asset FS can close next. Later submissions return ready handles with
`fs.ErrClosed`. A blocked reader or decoder must return before shutdown
finishes; a decoder must not close its own loader. `Wait` waits for work
without closing the loader; stop submitting before calling it. Upload
decoded GPU resources on the rendering goroutine after a handle is ready.

### Hot reload

A `Reloader` keeps what a game loaded in step with the files it came
from, so a texture repainted in an art tool or a shader recompiled with
`bunyip-shader` appears the moment it is saved. Load through it instead
of the package's loaders and call `Reload` once a frame:

```go
func (g *game) Init(ctx *engine.Context) error {
	g.rel = asset.NewReloader(ctx.Gfx, g.fs, 0) // 0 means poll twice a second
	var err error
	if g.hero, err = g.rel.Texture("sprites/hero.png", gfx.TextureOptions{}); err != nil {
		return err
	}
	g.water, err = g.rel.Shader("shaders/water.spv")
	return err
}

func (g *game) Update(ctx *engine.Context) error {
	names, err := g.rel.Reload()
	if err != nil {
		ctx.Log.Warn("reload failed", "err", err) // the old asset stays
	}
	for _, n := range names {
		ctx.Log.Info("reloaded", "asset", n)
	}
	...
}
```

Everything it loads keeps the pointer it handed back, which is what
makes a material reload work without bookkeeping: the texture's image is
swapped in place, so every `gfx.Material`, sprite and shader image slot
that names it draws the new pixels, even when the new image is a
different size. A shader's pipelines are rebuilt behind
`gfx.Shader.Reload`, keeping its images and uniforms, so every draw
through it runs the new program. Both cost no GPU wait inside a frame:
the image and the pipelines the old frames may still be drawing from go
on the retire ring.

`Reloader.Watch` covers anything the package has no loader for, such as
a level or a table of tuning values:

```go
g.rel.Watch("levels/1.json", func(data []byte) error {
	lv, err := parseLevel(data)
	if err != nil {
		return err
	}
	g.level = lv
	return nil
})
```

Only loose files change, so a name that resolves into a pack file or an
`embed.FS` is loaded once and never watched: a shipped game pays for the
polling goroutine and nothing else, and the same code runs in both
builds. A file that fails to decode keeps the asset the game already has
and reports the error, so a half-written save does not take the game
down with it, and the next write is tried again. `Watcher` is the layer
under all this for a game that would rather reload by hand.

Models and environments are not reloaded. Swapping a glTF file's
contents gives back different meshes, a different skeleton and different
animation clips, and every `gfx.AnimPlayer`, mesh pointer and node index
the game holds refers to the old ones. A `gfx.Environment` is a
panorama prefiltered into a cube map, and a `gfx.ReflectionProbe` bakes
and owns one of its own, so replacing the image behind one means
rebuilding every level of that cube. A game that wants either loads it
again and rebinds what pointed at the old one.

### The texture pipeline

`bunyip-tex` compresses a PNG or JPEG into a KTX2 file holding BC blocks
and the whole mip chain, so a texture takes a quarter to an eighth of
the GPU memory it would as RGBA and nothing is compressed or
downsampled while the game runs:

```
bunyip-tex -format bc7 -outdir build/textures art/textures/*.png
bunyip-pack -o game.pak build
```

`asset.Texture` reads a name ending in `.ktx2` through
`gfx.NewCompressedTexture` and anything else through the image decoders,
so a game changes the extension and nothing else, and `Reloader.Texture`
watches either kind. Packs store `.ktx2` files as they are, since block
data does not deflate. The
[2D graphics guide](graphics-2d.html) has the formats and what each is
for, and `gfx/ktx2` is the package under both the tool and the loader
for a game with its own pipeline.

## Saves and settings

[save](../pkg/save.html) writes JSON documents into the platform's data
directory (Application Support, XDG data, AppData) and replaces them
through a synced temporary file and rename, so an interrupted write does
not leave partial JSON at the target. Rename atomicity follows the host
filesystem; the parent directory is not synced, so this is not a power-loss
durability guarantee. `Load` reads a
document over defaults, so new settings fields get sensible values when
a file predates them. Whole ECS worlds are saved through
`ecs.World.Save`, described in the ECS guide.

```go
type settings struct {
	Volume     float32
	Fullscreen bool
}

store, err := save.Open("my-game") // Application Support, AppData or XDG
if err != nil {
	return err
}
s, err := store.Load("settings", settings{Volume: 0.8}) // defaults for a missing file
if err != nil {
	return err
}
s.Fullscreen = true
store.Write("settings", s)
names, _ := store.List() // the save slots, for a load menu
```

`save.OpenAt(dir)` takes any directory, which is what tests use.
`BUNYIP_DATA_DIR` overrides the base data directory and the app name is
appended to it. Save names omit `.json` and must be nonempty leaf names
without slashes or `.`/`..` components.
`Store.Load` copies defaults through JSON even when the file is missing,
so mutable maps and slices are independent of the defaults. Defaults that
cannot be round-tripped through JSON return an error.

## Translation

[locale](../pkg/locale.html) holds a `Table` of messages per language,
loaded from JSON a translator edits, with `{name}` placeholders and
plural forms chosen by each language's rules. `Plural` knows the common
languages, from English's two forms to Arabic's six. A `Bundle` falls
back through languages for keys a translation lacks, `Missing` lists
what a translator still has to do, and `For` gives a `Translator` whose
`T` and `N` the game calls.

Supply fallbacks explicitly; English is not installed automatically.
The lookup order is the requested language, its base language, then the
configured fallbacks. A missing key becomes `[key]`, and a placeholder
without an argument remains as written. Include `other` in each plural
entry. Build tables before concurrent reads and synchronize any later
changes; bundles and tables have no internal locks.

```go
b := locale.NewBundle("en") // English last, for keys another language lacks
for _, lang := range []string{"en", "ru"} {
	data, err := fs.Read("lang/" + lang + ".json")
	if err != nil {
		return err
	}
	if err := b.Load(lang, data); err != nil {
		return err
	}
}

t := b.For("ru")
t.T("menu.play")            // "Играть", or English if Russian lacks the key
t.T("greet", "who", "Ada")  // fills the {who} placeholder
t.N("inv.arrows", 3)        // the plural form Russian uses for 3
b.Missing("ru", "en")       // keys the translator still has to do
```

## Random numbers

[rng](../pkg/rng.html) is a seedable PCG32 generator. The same seed
gives the same sequence on every platform; `Fork` gives a subsystem its
own stream, so adding a call in one place never changes what another
produces; `State` and `Restore` put the generator in a save file.
`Pick`, `Shuffle`, `Roll` and `WeightedIndex` cover the usual game needs.

```go
r := rng.New(g.seed)
loot := r.Fork() // its own stream: rolling here never moves the other one

damage := r.Roll(2, 6) + 1 // 2d6+1
if loot.Chance(0.1) {
	drop := loot.Pick(g.rareItems)
	_ = drop
}
i := rng.WeightedIndex(loot, []float32{5, 3, 1}) // common, uncommon, rare
loot.Shuffle(g.deck)

state, inc := r.State() // into the save file; r.Restore(state, inc) on load
```

## Timers, sequences and tweens

[timer](../pkg/timer.html) schedules callbacks on game time (`After`,
`Every`) and offers a pollable `Countdown`. Because time is the game's
own, a paused game stops its timers by not calling `Update`, and a
replay runs them identically.

Use finite, nonnegative time steps. Timers due together fire in
registration order. Repeating timers catch up every elapsed interval,
so a large update may call the same function several times. Callbacks
may schedule or cancel timers, but must not recursively call `Update`.

```go
func (g *game) Update(ctx *engine.Context) error {
	if !g.paused {
		g.timers.Update(ctx.Delta) // g.timers is a timer.Scheduler
		if g.round.Update(ctx.Delta) {
			g.endRound() // the Countdown reached zero on this update
		}
	}
	return nil
}

g.timers.After(1.5, func() { g.door.Open() })
spawns := g.timers.Every(3, g.spawnWave)
g.timers.Cancel(spawns)
g.round.Start(90) // g.round is a timer.Countdown; Running says time remains
```

To write a cutscene, a turn's animation or a boss pattern as a list of
steps instead of a state machine, use `timer.Sequence`. The steps are
`Do` something, `Wait` a second, wait `Until` a condition holds, `Run` a
function each update until it reports it is done, and `Loop` for
patrols. `Skip` jumps to the end for a player who presses through.

```go
g.cutscene = timer.NewSequence().
	Do(func() { g.camera.PanTo(g.door) }).
	Until(func() bool { return g.camera.Arrived() }).
	Wait(0.5).
	Do(g.door.Open).
	Run(func(dt float32) bool { return g.hero.WalkTo(g.door, dt) })

// From Update; it reports whether the sequence has finished.
g.cutscene.Update(float32(ctx.Delta))
if ctx.Input.KeyPressed(input.KeySpace) {
	g.cutscene.Skip()
}
```

[tween](../pkg/tween.html) eases a value from one number to another
with the usual curves, repeats and yo-yos; `Sequence` chains tweens, and
`Of` (with `NewVec2` and `NewVec3`) tweens vectors, colours or any
value that has a blend function.

Easing curves such as `OutBack` and `OutElastic` may overshoot the
endpoints; `Progress` is not always between zero and one. Scalar and
generic tweens both support `YoYo`, reversing their endpoints on each
repeat. `OnDone` runs once when the complete sequence of repeats ends
and returns the same tween for chaining, including its generic value type.

```go
g.menuX = tween.New(-300, 40, 0.4, tween.OutQuad) // slide the panel in
g.pulse = tween.New(1, 1.2, 0.3, tween.InOutSine)
g.pulse.Repeat, g.pulse.YoYo = -1, true // forever, back and forth
g.fade = tween.NewOf(gfx.Transparent, gfx.White, 0.5, tween.OutQuad, gfx.Color.Lerp).
	OnDone(func() { g.ready = true })

// From Update.
x := g.menuX.Update(float32(ctx.Delta))
tint := g.fade.Update(float32(ctx.Delta))
if g.menuX.Done() {
	g.ready = true
}
```

## Grids

[grid](../pkg/grid.html) is for tile games: a generic `Grid`, `AStar`
with four- or eight-way movement and per-step costs, `Dijkstra` maps
with `Downhill` for chasing and fleeing, Bresenham `Line`, shadowcasting
`FOV` and `FloodFill`.

```go
walls := grid.New[bool](64, 48)
walls.Set(10, 10, true)
cost := func(from, to grid.Point) float32 {
	if walls.At(to.X, to.Y) {
		return grid.Blocked
	}
	return 1
}

// Every traversable step costs at least 1; true enables eight-way movement.
path := grid.AStarWithMinCost(64, 48, g.player, g.exit, true, cost, 1)

// One Dijkstra map moves the whole crowd: every monster steps downhill.
dist := grid.Dijkstra(64, 48, []grid.Point{g.player}, true, cost)
for i, m := range g.monsters {
	if next, ok := grid.Downhill(dist, m, true); ok {
		g.monsters[i] = next
	}
}

// Cells off the map count as opaque, so sight stops at the edge.
opaque := func(p grid.Point) bool { return !walls.In(p.X, p.Y) || walls.At(p.X, p.Y) }
seen := map[grid.Point]bool{}
grid.FOV(g.player, 9, opaque, func(p grid.Point) { seen[p] = true })
```

The algorithms take a cost or passability function over points, not a
`Grid`, so they work against whatever the game keeps its map in.
Costs may be zero or fractional, but must not be NaN. The callback
defines diagonal costs and whether cutting across a blocked corner is
allowed; the algorithms add neither restriction nor a diagonal multiplier.
`AStar` uses a zero heuristic, so it
finds cheapest paths without needing a lower bound on step costs.
When that bound is known, `AStarWithMinCost` can explore fewer cells:
pass `1` for unit-cost movement, or `0.1` if every traversable step
costs at least `0.1`. The bound applies to diagonal steps too. The
search scales Manhattan distance for four-way movement or Chebyshev
distance for eight-way movement by that bound.

A positive finite bound must never exceed any traversable step's cost;
overstating it can produce a more expensive path. Pass zero when the
minimum is unknown or zero-cost moves exist. Zero, negative and
nonfinite bounds all use the safe zero heuristic.

`Dijkstra` computes costs from its sources to every cell. With directed
costs, reverse the callback arguments to compute costs toward a target.
`Downhill` only chooses a neighboring cell with a lower stored value;
it does not check passability, edge costs or blocked corners. It can
stop on a zero-cost plateau and does not guarantee a cheapest route on
weighted or directed maps. The chasing example above uses symmetric
unit-cost movement; use the cost-aware path search for other rules.

`AStar`, `Dijkstra` and `FOV` borrow pooled scratch buffers. First use,
growth and pool eviction can allocate. To retain scratch across frames,
keep a `Pathfinder` for the map and a `Vision` for the viewer and call
their methods. They avoid steady-state allocations once their scratch
and caller-owned result buffers have sufficient capacity.
`Pathfinder.AStar` and `Pathfinder.AStarWithMinCost` append the path to
a slice the game owns and report whether there was one.
`Pathfinder.DijkstraInto` fills a map the game
already has, and `Vision.FOV` reuses the scratch space a cast needs.

```go
// Made once, with the map, and kept.
g.pf = grid.NewPathfinder(64, 48)
g.dist = grid.New[float32](64, 48)

// Each frame, searching into the game's own buffers.
if path, ok := g.pf.AStarWithMinCost(g.path[:0], g.player, g.exit, true, cost, 1); ok {
	g.path = path
}
g.pf.DijkstraInto(g.dist, []grid.Point{g.player}, true, cost)
g.sight.FOV(g.player, 9, opaque, func(p grid.Point) { g.seen[p] = true })
```

`Resize` points a `Pathfinder` at a map of another size when the level
changes. Both types hold scratch space rather than results, so give each
goroutine its own.

## Networking

[network](../pkg/network.html) moves typed messages between game
instances: ordered over TCP for turn-based play, lobbies and chat, and
over UDP for real-time state. A `Registry` names the message types both
ends agree on; events arrive through `Poll` once per frame, and
`SetOnActivity` can wake a sleeping turn-based game.

Closing a local TCP connection or server cancels publication blocked by
a full event queue. Already queued events remain available to `Poll`,
but pending messages or disconnect notifications may be dropped during
local shutdown. A remote disconnect is queued after its preceding
messages as usual.

`SetOnActivity` may be changed while messages arrive, including from
inside its callback. Server registration updates existing and future
connections. If events are pending, registration invokes the new callback
before returning; later notifications run on network goroutines. This
also wakes for a client's initial `Connected` event. Callbacks run outside
locks and should signal the game loop, for example with `ctx.Wake`.
Keep them short; drain pending events before registering again inside a
callback to avoid recursion. A captured callback may finish after
replacement or removal. Nil disables future captures. UDP uses the same
registration behavior; its notifications can cover multiple events.
TCP event queues hold 1024 events and apply backpressure when full.
TCP and TLS `Send` use a five-second `DefaultSendTimeout`, covering
writer acquisition, a pending TLS handshake, and transport writes.
`SendContext(ctx, msg)` accepts your own cancellation and deadline;
`context.Background()` explicitly permits an indefinite wait. Encoding
runs synchronously, so a custom marshaler that blocks cannot be
interrupted. Sends still block the caller; use a short context budget
or send from your own worker when the game loop must remain responsive.

Cancellation while waiting for another sender leaves the connection
usable. An interrupted TLS handshake or failed write closes it to avoid
continuing after a partial frame or an unusable TLS stream. Send errors
preserve `errors.Is` checks for `context.Canceled` and
`context.DeadlineExceeded`. A completed send means the transport
accepted the message, not that the peer processed it.

`Broadcast` sends sequentially with one shared five-second budget for
all selected peers. `BroadcastContext` uses one caller-supplied budget.
Both return a `map[*network.Conn]error` containing only failed peers,
including peers not reached before cancellation; nil means every
selected peer accepted its message. Connection iteration order is
unspecified. Handle individual errors directly:

```go
for peer, err := range server.Broadcast(Chat{From: "server", Text: "Ready"}) {
	fmt.Printf("peer %d: %v\n", peer.ID, err)
}
```

Complete registry registration before opening connections and leave it
unchanged while network goroutines use it.

Messages are plain structs. Both ends build the same registry in the
same order:

```go
type Chat struct{ From, Text string }
type Move struct{ X, Y int }

reg := network.NewRegistry().Register(Chat{}, Move{})
```

The server listens and drains its events each update:

```go
server, err := network.Listen(":7777", reg)
if err != nil {
	return err
}
server.SetOnActivity(ctx.Wake) // turn-based: wake the loop when a message lands

for _, ev := range server.Poll() {
	switch ev.Kind {
	case network.Connected:
		g.log("joined:", ev.Conn.Addr())
	case network.Disconnected:
		g.drop(ev.Conn)
	case network.Message:
		if m, ok := ev.Msg.(*Move); ok {
			server.Broadcast(m, ev.Conn) // to everyone but the sender
		}
	}
}
```

The client works the same way, with `Send` on the connection it embeds:

```go
client, err := network.Dial("gameserver:7777", reg, 5*time.Second)
if err != nil {
	return err
}
defer client.Close()
client.Send(Chat{From: g.name, Text: "hello"})

for _, ev := range client.Poll() {
	if m, ok := ev.Msg.(*Chat); ok {
		g.lines = append(g.lines, m.From+": "+m.Text)
	}
}
```

`ListenTLS` and `DialTLS` encrypt TCP. `SelfSignedConfig` makes a
certificate for a LAN game together with a client configuration pinned
to it, and `Fingerprint` with `PinnedConfig` lets a player on another
machine pin the same certificate.

```go
serverCfg, clientCfg, err := network.SelfSignedConfig("localhost", "192.168.1.20")
if err != nil {
	return err
}
server, err := network.ListenTLS(":7777", reg, serverCfg)
...
// On the host's own machine clientCfg already trusts it.
client, err := network.DialTLS(addr, reg, clientCfg, 5*time.Second)

// Elsewhere: the host reads out this string and the player pins it.
print := network.Fingerprint(serverCfg)
client, err = network.DialTLS(addr, reg, network.PinnedConfig(print), 5*time.Second)
```

A UDP `Peer` has two ways to send. `Send` sends a packet that may be
lost, for state that is replaced every frame. `SendReliable` resends
until the packet is acknowledged and delivers in order, for anything
that must arrive. Every packet acknowledges what came the other way,
and `Stats` reports a link's round trip, loss and pending count. Peers
exchange hello and keep-alive packets, so a `Peer` raises `Connected` and
`Disconnected` for UDP addresses (after `SetTimeout` of silence, a
goodbye, or a restart), and `Peers` lists who is there.

```go
peer, err := network.ListenUDP(":7778", reg)
if err != nil {
	return err
}
defer peer.Close()
peer.SetTimeout(5 * time.Second)

host, err := network.Resolve("gameserver:7778")
peer.Send(host, Move{X: g.x, Y: g.y})    // every frame; a lost one does not matter
peer.SendReliable(host, Chat{Text: "gg"}) // resent until it arrives, in order

if s, ok := peer.Stats(host); ok {
	g.ping = s.RTT // with s.Loss and s.Pending, for the netgraph
}
```

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
with hysteresis at the edge so nothing flickers in and out. The two
slices `Interest.End` returns belong to the `Interest` and are refilled
by the next `End`, so send from them in the same frame and copy anything
that has to outlive it.

Prediction takes the most setting up. `Step` must be the same function
the server runs, so the replay lands where the server did:

```go
step := func(s playerState, in playerInput) playerState { return s.move(in) }
g.pred = network.NewPredictor(playerState{}, step)

// Each update: apply locally at once and send the input with its sequence.
in := playerInput{Dx: axis, Jump: jump}
seq := g.pred.Apply(in)
peer.Send(host, inputMsg{Seq: seq, In: in})
g.drawAt = g.pred.State() // already moved, no wait for the server

// When the server's state arrives, rewind and replay what it has not seen.
g.pred.Reconcile(m.Ack, m.State)
```

Other players go through an `Interpolator` instead, drawn a little
behind the newest snapshot:

```go
g.remote.Delay = 0.1        // two or three send intervals
g.remote.Add(m.Time, m.Pos) // g.remote is a network.Interpolator[lin.Vec2]

// At subtracts Delay itself; the time is the server's, from a Clock.
pos, ok := g.remote.At(g.clock.ServerTime(ctx.Time), lin.Vec2.Lerp)
```
