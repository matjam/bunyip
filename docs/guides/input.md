---
title: Input
order: 13
summary: keys, mouse, gamepads, action maps and rebinding
---

The [input](../pkg/input.html) package is the state of every key,
button, stick and pointer, read through `ctx.Input`. Nothing is polled
in game code: the engine feeds the platform's events in and clears the
edges after each update, so `KeyPressed` is true for exactly one update
per press however many frames or updates a press spans.

## Keys and the mouse

`KeyDown` is the level, `KeyPressed` and `KeyReleased` the edges, and
`KeyRepeated` the operating system's auto-repeat for menus that scroll
while a key is held. `KeyHeld` is how long a key has been down, for a
charged shot, and `KeysDown` lists every held key, for combos and
rebinding screens. `Chars` is the text typed this update with the
keyboard layout and modifiers applied, which is what a text field wants
rather than keys; `Composition` is the input method's text in progress.

`Mouse` and `MousePos` are the pointer in view units, `MouseDelta` its
movement (raw when the cursor is captured), `Scroll` the wheel in lines,
and the `Mouse*` button methods mirror the key ones. `MouseDoubleClicked`
reports the second of two quick presses close together.

During `Draw` the accessors switch to what happened since the last drawn
frame, so an immediate-mode interface built in `Draw` sees every press
even when the frame ran several updates or none.

## Gamepads

`Gamepad(i)` is the ith controller: `Connected`, its `Name`, `Down`,
`Pressed` and `Released` for the standard buttons, and `Axis` for the
sticks and triggers in -1..1. Sticks report up as positive y, as the
hardware does. `JustConnected` and `JustDisconnected` mark the update a
controller appears or vanishes, for a join prompt or a pause.

## Actions

Game code that reads keys directly cannot be rebound and cannot take a
gamepad without a second copy of every check. `Actions` names what the
player does and binds each name to any number of sources:

```go
acts := input.NewActions()
acts.Bind("jump", input.KeySource(input.KeySpace), input.PadButton(input.ButtonA))
acts.Bind("fire", input.MouseSource(input.MouseLeft), input.PadAxis(input.AxisRightTrigger))
acts.Bind("move_x",
	input.KeySource(input.KeyD), input.KeySource(input.KeyA).Neg(),
	input.PadAxis(input.AxisLeftX), input.PadAxis(input.AxisLeftX).Neg())

if acts.Pressed(ctx.Input, "jump") { ... }
vx := acts.Value(ctx.Input, "move_x") // -1..1 from keys or the stick
```

`Down`, `Pressed` and `Released` work as for keys; `Value` sums the
sources and clamps, with a dead zone on sticks. An axis source reads one
side of a stick, so bind its `Neg` for the other. `Pad` picks which
controller the pad sources read.

A settings screen calls `Listen` each update while waiting: it returns
the first key, button or stick flick, and `Rebind` swaps it in. The
whole map marshals to JSON as `{"jump": ["key:J", "pad:A"]}` for a
settings file, and `ParseSource` reads the same names.

## The loop

Updates run at `Config.FixedStep` however fast frames come, and a frame
that took long runs several. `MaxCatchUp` caps how much lost time is
made up after a stall and `MaxSteps` caps the updates in one frame, so
a long load does not turn into a burst of updates; the rest of the
time is dropped. `Context.Alpha` is how far the next update is, for
interpolating what is drawn.
