---
title: The debug console
group: Engine
order: 5
summary: the in-game command line, its commands and variables, and the panels that show what the engine is doing
---

The [console](../pkg/console.html) package is a drop-down command line
over the top of the view and a window of panels that show what every
part of the engine is doing: frame timings, GPU resources, the entities
of a world, the physics simulation, the mixer, the input devices and
whatever services the game attaches. It is meant for the developer and
for a player who is helping you find a bug, not for the shipped
interface.

## Turning it on

Set `Config.Console` and draw the console last:

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Console.Open() {
		return nil // the console has the keyboard
	}
	// ... the game's own key handling ...
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	// ... the game's drawing and its own interface ...
	return ctx.Console.Draw(ctx)
}

func main() {
	bunyip.Run(bunyip.Config{Title: "Game", Console: true}, &game{})
}
```

The engine builds the console, attaches what it owns (the graphics
context, the mixer, the input state and the frame timings), tees the log
through it and puts it on `Context.Console`. Drawing is the game's call
because draw order belongs to the game: the console has to be the last
thing drawn so it sits above the game's own interface.

Every console method works on a nil console, so the two calls above can
stay in the game when `Config.Console` is off and the field is nil.

`Config.ConsoleKey` chooses the key that opens the drop-down; zero means
the backquote key. A game that wants a console of another shape (a
different height, its own font or theme, scripts read from a pack file)
calls `console.New` itself and drives it the same way:

```go
g.con = console.New(console.Options{Key: input.KeyF1, Height: 0.5, Read: assets.Read})
```

## Using it

| Key | What it does |
|---|---|
| `` ` `` | Opens and closes the drop-down |
| F4 | Opens and closes the debug panels |
| F3 | The engine's frame-timing overlay, which is not the console |
| Up, Down | The previous commands |
| Tab | Completes a command or variable name |
| PageUp, PageDown | Scrolls the output |
| Enter | Runs the line |
| Escape | Closes the console |

While the drop-down is open it takes the keyboard: characters go to the
command line, and the game is expected to check `Open` and leave the
keys alone for that update. Key bindings made with `bind` fire only
while the console is shut, so they do not go off as you type.

The toggle keys and command line are processed in `Draw`. An `Open`
check in `Update` therefore sees the previous draw's state; the opening
keypress may already have reached gameplay. `Open` describes the
drop-down only, not the F4 panels. Keep gameplay input routing explicit
when the panels or another interface also need controls.

## The built-in commands

`help` on its own lists every command with its one-line description, and
`help <command>` describes one.

| Command | What it does |
|---|---|
| `help [command]` | List the commands, or describe one |
| `echo <text...>` | Print the arguments |
| `clear` | Empty the output |
| `quit` | End the game |
| `screenshot [file]` | Write the next frame to a PNG, `screenshot.png` by default |
| `exec <file>` | Run a file of commands, one per line, skipping blank lines and lines starting with `#` or `//` |
| `bind <key> <command...>` | Run a command when a key is pressed |
| `unbind <key>` | Drop a key's binding |
| `binds` | List the key bindings |
| `fps` | The frame rate and frame time |
| `stats` | The frame timings, the GPU pass times, the profile scopes and the draw counts |
| `log <level>` | Show log records at this level and above: debug, info, warn or error |
| `timescale [x]` | Read or set how fast game time runs |
| `panels [tab]` | Open or close the debug panels, on a named tab |
| `set <name> <value>` | Change a registered variable |
| `get <name>` | Print a registered variable |
| `vars [prefix]` | List the registered variables |

Key names are the ones `input.Key.String` gives, so `bind F5 drop` and
`bind Space "echo jump"` both work; an argument in double quotes keeps
its spaces.

`timescale 0.25` runs game time at a quarter speed without changing the
update rate, so a fast fight can be watched a frame at a time; `Delta`
is scaled and `Time` stays real. It reaches the loop through
`Context.SetTimeScale`, which a game may also call itself.

## Commands and variables of your own

Register a command with a name, a one-line description and a function
taking the arguments after the name:

```go
ctx.Console.Register("give", "give <item> [count]: add an item to the pack",
	func(args []string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("give: needs an item")
		}
		g.inventory.Add(args[0])
		return "gave " + args[0], nil
	})
```

What the function returns is printed; the error is printed in red.
Callbacks run synchronously on the goroutine calling `Run` or `Draw`,
so avoid blocking work there. Registration is synchronized, but the
game values exposed through variables and attachment callbacks are
still the game's responsibility to synchronize. Normally these are
all read and written on the game loop goroutine.

A variable is a pointer the console reads and writes, which is how a
game exposes the numbers it is still tuning without writing a command
for each one:

```go
ctx.Console.Float("player.speed", &g.speed, "how fast the player runs")
ctx.Console.Int("world.seed", &g.seed, "")
ctx.Console.Bool("player.noclip", &g.noclip, "walk through walls")
ctx.Console.String("player.name", &g.name, "")
```

Then `set player.speed 8`, `get player.speed`, and `vars player.` to
list a group. Tab completes variable names after `set` and `get`. A
value of another type implements the two-method `console.Var` interface
and registers with `Var`.

Registering from `Init` is the usual place. Device recovery creates a
fresh console and calls the game's `Recover` method when it implements
`bunyip.Recoverer`; it does not call `Init` again. Put registrations in
shared setup called from both methods. A game without `Recoverer`
returns the device-loss error instead of recovering.

A file of commands loaded with `exec` sets a whole scene up at once:

```
# tuning.cfg
set player.speed 8
set player.noclip true
timescale 0.5
panels Physics
```

## The panels

F4, or the `panels` command, opens a resizable window of tabs. Drag its
title bar to move it and its bottom-right corner to resize it.

**Engine** graphs the last few hundred frames, each frame one column
split into the time spent updating, drawing and presenting, with the
line at 16.7 ms that a frame has to stay under to hold sixty a second.
A mark above each column is the time the GPU spent on that frame, drawn
beside the stack rather than inside it because the GPU works on one
frame while the processor records the next, so the two do not add up.
Under it are the frame timings, how many updates the loop ran this frame
and the most it has ever run in one (the fixed-step catch-up), the GPU
time by pass, the profile scopes `Context.Profile` recorded, the 2D and
3D draw counts with what was culled, and the draw budget from
`Config.DrawBudget`.

### GPU pass times

The engine times each pass on the GPU with timestamp queries: the shadow
atlas, the opaque scene, the screen-space reflections, the blended
scene, the decals, the motion vectors, the temporal resolve, motion
blur, depth of field, the light shafts, bloom, ambient occlusion, the
composite, the anti-aliasing resolve and the 2D stream. A pass that runs for a render
texture as well as for the screen is summed into one figure, and a pass
a frame does not run is left out, so the list says what that frame
actually cost. Work outside the frame, such as a `BakeProbe` call, is
not timed: it records on its own command buffers and blocks until it is
done, so the wall clock already tells you what it cost. The panel lists them under the graph, the F3 overlay
shows the frame's total as `gpu`, and a game reads both from
`ctx.Stats.GPUFrameMS` and `ctx.Stats.GPU` (`gfx.FrameStats.GPUFrameMS`
and `GPU` under it) without the console.

The queries are read back at the frame slot's next begin, once its fence
has been waited on, so timing a frame never stalls one: the figures
describe a frame a frame or two back and stand still while nothing new
lands. A device that cannot time work on its queue, which some MoltenVK
configurations are, reports no passes and a zero total rather than
failing, and the graph leaves the mark off.

**Graphics** edits the live `PostSettings`: exposure, bloom and its
threshold, vignette, saturation, contrast, ambient occlusion and its
radius, the occlusion buffer on its own, and the anti-aliasing pass.
Under those it counts the lights the frame kept and dropped and lists
every GPU resource the graphics context has made and not destroyed:
textures, meshes, models, fonts, render textures and environments, with
their sizes and an estimate of the GPU memory they hold. The same list
is `gfx.Graphics.Resources`, so a game can watch it without the console.

**Entities** needs a world attached (see below). It counts the world's
entities, systems and resources, lists the entities with the components
each carries, and filters them by component name. The list stops at five
hundred and says how many matched, so a world of a hundred thousand
entities costs no more than that to show. Selecting one shows
its components; numbers, bools and strings are edited in place and
written back through the world, and everything else is shown as it is. A
field being typed into keeps what you type, and every other field
follows the live value, so a moving body's position keeps up. The panel
also despawns the selected entity, spawns from a prefab library, and
lists the systems with their last timings and a switch to turn one off.

**Physics** counts the bodies (and how many are asleep), the colliders,
the contacts and triggers of the last update, and the joints, for both
dimensions. It edits the `phys.Settings2` or `phys.Settings3` resource
on the world: gravity, substeps, solver iterations and the sleep
settings. It pauses the world, which turns every system in it off, and
steps it one update at a time. A switch outlines every collider over the
scene as debug lines, contact normals included, which is
`phys.DrawColliders3` and `phys.DrawColliders2` and can be called
without the console.

**Audio** lists the buses with a gain slider and mute, solo and pause
switches, the playing voices with what they are playing, their gain,
pitch, bus and position, and where the listener is. The mixer does not
measure its own load, so the panel says so rather than making a number
up.

**Input** shows the keys held, the pointer's position, buttons and
scroll, the modifier keys, and every connected gamepad with its buttons
and axes. An attached action map is listed with each action's sources
and its value this frame.

**Services** lists the console's own variables with their values, and
whatever the game attached: lines of text and network link statistics.

## Attaching a game's own things

The engine attaches what it owns. A game attaches the rest, once, with
one call each:

```go
func (g *game) Init(ctx *bunyip.Context) error {
	ctx.Console.Attach("world", g.world)          // Entities and Physics
	ctx.Console.AttachActions("player", g.actions) // Input
	ctx.Console.AttachInfo("locale", func() string { return g.tr.Lang() })
	ctx.Console.AttachInfo("timers", func() string { return strconv.Itoa(g.timers.Pending()) })
	...
}
```

`Attach` takes a name, so several worlds can be attached and the panels
show a list to pick from. `AttachInfo` adds a line to the Services
panel, redrawn from the function every frame: it is how anything the
console knows nothing about gets shown.

Network links go the same way. The console does not depend on the
network package, so the game reads the statistics it wants shown:

```go
ctx.Console.AttachLinks("server", func() []console.Link {
	var out []console.Link
	for _, a := range g.peer.Peers() {
		s, _ := g.peer.Stats(a)
		out = append(out, console.Link{Peer: a.String(), RTT: s.RTT,
			Loss: s.Loss, Pending: s.Pending, Connected: s.Connected})
	}
	return out
})
```

A prefab library is a resource on the world rather than an attachment,
so the Entities panel finds it on whichever world is showing:

```go
ecs.SetResource(g.world, console.Prefabs{"goblin": goblinPrefab, "chest": chestPrefab})
```

## The log

With `Config.Console` the engine tees the log through the console:
every `slog` record shows in the drop-down as well as wherever it was
going, coloured by level, and the file `Config.LogFile` names still gets
its copy. `log debug` widens what is captured from then on; records
already in the buffer are kept as they are. The console keeps the last
thousand lines by default (`Options.Lines`).

A console a game builds itself installs the tee where it builds its
logger:

```go
con := console.New(console.Options{})
cfg.Log = slog.New(con.Handler(slog.Default().Handler()))
```

`Console.Print` and `Printf` write a line straight into the output, from
any goroutine.

## What it costs

A closed console draws nothing and does no work beyond reading the
toggle keys and its bindings, one frame's timings for the graph, and any
collider drawing that was left on. The engine builds the console
whatever the game does with it, so the log tee is in place from the
first frame.

The examples are the fastest way to see it: `go run ./examples/gallery`
and `go run ./examples/physics-lab` both run with the console on.
