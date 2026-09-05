---
title: Input
group: Engine
order: 2
summary: keys, mouse, gamepads, action maps and rebinding
---

The [input](../pkg/input.html) package holds the state of every key,
button, stick and pointer, read through `ctx.Input`. Game code never
polls the platform. The engine feeds events in and clears the edges
after each update, so `KeyPressed` is true for exactly one update per
press, however many frames the press spans. OS repeats also count as
presses; several events in one update collapse to a single true value.

## Keys and the mouse

`KeyDown` reports whether a key is held; `KeyPressed` and `KeyReleased`
report the changes since the last update; `KeyRepeated` reports the
operating system's auto-repeat, for menus that scroll while a key is
held. `KeyHeld` is how long a key has been down, for a charged shot, and
`KeysDown` lists every held key, for combos and rebinding screens.
`KeysDown` allocates the slice it returns, so it belongs in a settings
screen or a check made on demand; to scan every frame, keep a slice and
pass it to `AppendKeysDown`.
`Chars` is the text typed this update with the keyboard layout and
modifiers applied, which is what a text field reads; `Composition` is
the input method's text in progress.
On Wayland, compositor modifier notifications update `Mods` independently
of key events, so modifier releases and lock changes are visible without
waiting for another key press.

Keys are named by physical position. `KeyW` is the key in W's place on a
US keyboard whatever it prints, which is what movement bindings need.
`Key.String` names a key for a prompt.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeySpace) {
		g.jump()
	}
	if in.KeyDown(input.KeyD) {
		g.x += speed * float32(ctx.Delta)
	}
	if t := in.KeyHeld(input.KeyF); t > 0 {
		g.charge = min(t, 2) // seconds the key has been down
	}
	g.held = in.AppendKeysDown(g.held[:0]) // combos, without allocating
	for _, k := range g.held {
		g.combo = append(g.combo, k.String())
	}
	g.typed = append(g.typed, in.Chars()...)
	return nil
}
```

`Mouse` and `MousePos` give the pointer in view units, `MouseDelta` its
movement (raw motion when the cursor is captured), `Scroll` the wheel in
lines, and the `Mouse*` button methods mirror the key ones.
`MouseDoubleClicked` reports the second of two quick presses close
together. Capture belongs to the window rather than the input state:
`ctx.SetCursorCaptured` hides the pointer and delivers relative motion
only, which is what a first-person camera needs.

```go
in := ctx.Input
p := in.MousePos() // view units, a lin.Vec2
if in.MousePressed(input.MouseLeft) {
	g.selectAt(p.X, p.Y)
}
if in.MouseDoubleClicked(input.MouseLeft) {
	g.openAt(p.X, p.Y)
}
if _, dy := in.Scroll(); dy != 0 { // in lines; a trackpad's smooth scrolling is scaled to lines
	g.zoom *= 1 + dy*0.1
}
if in.KeyPressed(input.KeyC) {
	ctx.SetCursorCaptured(!ctx.CursorCaptured())
}
if ctx.CursorCaptured() {
	dx, dy := in.MouseDelta() // raw motion, no screen edge to stop at
	g.yaw += dx * 0.002
	g.pitch += dy * 0.002
}
```

During `Draw` the "changed" accessors cover the whole drawn frame, so an
immediate-mode interface built in `Draw` sees every press even when the
frame ran several updates or none.
Keyboard, mouse and gamepad button transitions are reported once per
drawn frame. Drawing without an intervening update does not repeat an
edge, and drawing does not consume the edge still pending for Update.

## Gamepads

`Gamepad(i)` is the ith controller: `Connected`, its `Name`, `Down`,
`Pressed` and `Released` for the standard buttons, and `Axis` for the
sticks and triggers in -1 to 1. Sticks report up as positive y, as the
hardware does. `JustConnected` and `JustDisconnected` mark the update a
controller appears or vanishes, for a join prompt or a pause. These two
connection flags and axis transitions are update-only; only gamepad
button transitions have a separate whole-frame view during Draw. There are
`MaxGamepads` of them, and `Gamepad(i)` is never nil, so an unplugged
controller reads as nothing held. The returned pointer is a snapshot;
fetch it again for fresh state. `Axis` zeroes values strictly between
-0.08 and 0.08 without rescaling the remainder. Action maps use their
own dead zone, 0.2 by default.

```go
pad := ctx.Input.Gamepad(0)
if pad.JustConnected() {
	g.log("player one: " + pad.Name)
}
if pad.Pressed(input.ButtonA) {
	g.jump()
}
x, y := pad.Axis(input.AxisLeftX), pad.Axis(input.AxisLeftY)
if x*x+y*y > 0.04 { // a dead zone of 0.2
	g.move(x, y) // y is positive up, as the stick reports it
}
g.trigger = pad.Axis(input.AxisRightTrigger) // 0 to 1
for i := range input.MaxGamepads {
	if ctx.Input.Gamepad(i).JustDisconnected() {
		g.paused = true
	}
}
```

## Actions

`Actions` names what the player does and binds each name to any number
of sources. Game code that reads keys directly cannot be rebound, and it
needs a second copy of every check to support a gamepad.

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

### Action handles

The methods above look the name up on every call. A game asks the same
few actions several times per frame, so resolve each one once with
`Action` and keep the handle:

```go
type game struct {
	acts        *input.Actions
	jump, moveX input.Action
}

func (g *game) Init(ctx *bunyip.Context) error {
	g.acts = input.NewActions()
	g.acts.Bind("jump", input.KeySource(input.KeySpace), input.PadButton(input.ButtonA))
	g.acts.Bind("move_x",
		input.KeySource(input.KeyD), input.KeySource(input.KeyA).Neg(),
		input.PadAxis(input.AxisLeftX), input.PadAxis(input.AxisLeftX).Neg())
	g.jump = g.acts.Action("jump")
	g.moveX = g.acts.Action("move_x")
	return nil
}

func (g *game) Update(ctx *bunyip.Context) error {
	if g.jump.Pressed(ctx.Input) { ... }
	vx := g.moveX.Value(ctx.Input)
	return nil
}
```

`Action` is a small value with the same `Value`, `Down`, `Pressed`,
`Released` and `Bindings` methods, minus the name argument. A handle
names the action rather than the sources behind it, so it stays valid
across `Bind`, `Rebind`, `Unbind` and loading a settings file, and the
name need not be bound when the handle is taken. The zero `Action` is
bound to nothing and reads as off.

A settings screen calls `Listen` each update while it waits for the
player. `Listen` returns the first key, button or stick flick, and
`Rebind` swaps it in, replacing every binding the action had. `Names`
and `Bindings` fill the rest of the screen.

```go
for _, name := range acts.Names() {
	g.row(name, acts.Bindings(name)) // each Source prints as "key:Space"
}
if g.listening != "" { // the player pressed "change" on this action
	if src, ok := acts.Listen(ctx.Input); ok {
		acts.Rebind(g.listening, src)
		g.listening = ""
	}
}
```

The whole map marshals to JSON as `{"jump": ["key:J", "pad:A"]}` for a
settings file, and `ParseSource` reads the same names.

```go
data, err := json.Marshal(acts) // {"jump":["key:Space","pad:A"],...}
if err != nil {
	return err
}
loaded := input.NewActions()
if err := json.Unmarshal(data, loaded); err != nil {
	return err
}
src, err := input.ParseSource("axis:RightTrigger*0.5") // half travel
if err == nil {
	loaded.Bind("fire", src)
}
```
