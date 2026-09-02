---
title: Input
group: Engine
order: 2
summary: keys, mouse, gamepads, action maps and rebinding
---

The [input](../pkg/input.html) package holds the state of every key,
button, stick and pointer, read through `ctx.Input`. Game code never
polls the platform: the engine feeds events in and clears the edges
after each update, so `KeyPressed` is true for exactly one update per
press, however many frames the press spans.

## Keys and the mouse

`KeyDown` reports whether a key is held; `KeyPressed` and `KeyReleased`
report the changes since the last update; `KeyRepeated` reports the
operating system's auto-repeat, for menus that scroll while a key is
held. `KeyHeld` is how long a key has been down, for a charged shot, and
`KeysDown` lists every held key, for combos and rebinding screens.
`Chars` is the text typed this update with the keyboard layout and
modifiers applied, which is what a text field wants; `Composition` is
the input method's text in progress.

Keys are named by physical position: `KeyW` is the key in W's place on a
US keyboard whatever it prints, which is what movement bindings want.

`Mouse` and `MousePos` give the pointer in view units, `MouseDelta` its
movement (raw motion when the cursor is captured), `Scroll` the wheel in
lines, and the `Mouse*` button methods mirror the key ones.
`MouseDoubleClicked` reports the second of two quick presses close
together.

During `Draw` the "changed" accessors cover the whole drawn frame, so an
immediate-mode interface built in `Draw` sees every press even when the
frame ran several updates or none.

## Gamepads

`Gamepad(i)` is the ith controller: `Connected`, its `Name`, `Down`,
`Pressed` and `Released` for the standard buttons, and `Axis` for the
sticks and triggers in -1 to 1. Sticks report up as positive y, as the
hardware does. `JustConnected` and `JustDisconnected` mark the update a
controller appears or vanishes, for a join prompt or a pause.

## Actions

Game code that reads keys directly cannot be rebound, and it needs a
second copy of every check to support a gamepad. `Actions` names what
the player does and binds each name to any number of sources:

```go
acts := input.NewActions()
acts.Bind("jump", input.KeySource(input.KeySpace), input.PadButton(input.ButtonA))
acts.Bind("fire", input.MouseSource(input.MouseLeft), input.PadAxis(input.AxisRightTrigger))
acts.Bind("move_x",
	input.KeySource(input.KeyD), input.KeySource(input.KeyA).Neg(),
	input.PadAxis(input.AxisLeftX), input.PadAxis(input.AxisLeftX).Neg())

if acts.Pressed(ctx.Input, "jump") { ... }
vx := acts.Value(ctx.Input, "move_x") // -1 to 1 from keys or the stick
```

`Down`, `Pressed` and `Released` work as they do for keys. `Value` sums
the sources and clamps the result, applying a dead zone to sticks. An
axis source reads one side of a stick, so bind its `Neg` for the other
side. `Pad` chooses which controller the pad sources read.

A settings screen calls `Listen` each update while it waits for the
player: it returns the first key, button or stick flick, and `Rebind`
swaps it in. The whole map marshals to JSON as
`{"jump": ["key:J", "pad:A"]}` for a settings file, and `ParseSource`
reads the same names.
