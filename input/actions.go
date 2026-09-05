package input

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SourceKind is what a Source reads.
type SourceKind uint8

const (
	SourceKey       SourceKind = iota // a keyboard key
	SourceMouse                       // a mouse button
	SourcePadButton                   // a gamepad button
	SourcePadAxis                     // a gamepad axis, one direction
)

// Source is one physical input an action can be bound to: a key, a
// mouse button, a gamepad button, or a gamepad axis in one direction.
// Scale is what it contributes to the action's value when fully on
// (1 by default, -1 for the negative side of an axis pair).
type Source struct {
	Kind  SourceKind // device input category
	Code  int        // Key, MouseButton, GamepadButton or GamepadAxis
	Scale float32    // signed contribution at full travel; zero means 1
}

// KeySource binds a key.
func KeySource(k Key) Source { return Source{Kind: SourceKey, Code: int(k), Scale: 1} }

// MouseSource binds a mouse button.
func MouseSource(b MouseButton) Source { return Source{Kind: SourceMouse, Code: int(b), Scale: 1} }

// PadButton binds a gamepad button.
func PadButton(b GamepadButton) Source { return Source{Kind: SourcePadButton, Code: int(b), Scale: 1} }

// PadAxis binds the positive side of a gamepad axis; Neg flips it.
func PadAxis(a GamepadAxis) Source { return Source{Kind: SourcePadAxis, Code: int(a), Scale: 1} }

// Neg returns the source contributing the opposite sign: the A key of
// an A and D pair, the left half of a stick's x axis.
func (s Source) Neg() Source { s.Scale = -s.Scale; return s }

var (
	mouseNames = [MouseButtonCount]string{"Left", "Right", "Middle", "Button4", "Button5"}
	padNames   = [GamepadButtonCount]string{"A", "B", "X", "Y", "LeftShoulder", "RightShoulder", "LeftStick", "RightStick", "Menu", "Options", "Home", "DpadUp", "DpadDown", "DpadLeft", "DpadRight"}
	axisNames  = [GamepadAxisCount]string{"LeftX", "LeftY", "RightX", "RightY", "LeftTrigger", "RightTrigger"}
)

// String names the source the way bindings are saved: "key:Space",
// "mouse:Left", "pad:A", "axis:LeftX", "-axis:LeftX" for the negative
// side, with a scale other than 1 or -1 appended as "*0.5".
func (s Source) String() string {
	var name string
	switch s.Kind {
	case SourceKey:
		name = "key:" + Key(s.Code).String()
	case SourceMouse:
		name = "mouse:" + nameOf(mouseNames[:], s.Code)
	case SourcePadButton:
		name = "pad:" + nameOf(padNames[:], s.Code)
	case SourcePadAxis:
		name = "axis:" + nameOf(axisNames[:], s.Code)
	}
	scale := s.Scale
	if scale == 0 {
		scale = 1
	}
	if scale < 0 {
		name = "-" + name
		scale = -scale
	}
	if scale != 1 {
		name = fmt.Sprintf("%s*%g", name, scale)
	}
	return name
}

func nameOf(names []string, code int) string {
	if code >= 0 && code < len(names) {
		return names[code]
	}
	return fmt.Sprint(code)
}

func indexOf(names []string, name string) (int, bool) {
	for i, n := range names {
		if strings.EqualFold(n, name) {
			return i, true
		}
	}
	return 0, false
}

// ParseSource reads a source written by String.
func ParseSource(text string) (Source, error) {
	var s Source
	scale := float32(1)
	if strings.HasPrefix(text, "-") {
		scale = -1
		text = text[1:]
	}
	if i := strings.LastIndex(text, "*"); i >= 0 {
		var f float32
		if _, err := fmt.Sscanf(text[i+1:], "%g", &f); err != nil {
			return s, fmt.Errorf("input: bad scale in %q", text)
		}
		scale *= f
		text = text[:i]
	}
	kind, name, ok := strings.Cut(text, ":")
	if !ok {
		return s, fmt.Errorf("input: source %q wants kind:name", text)
	}
	var idx int
	found := false
	switch kind {
	case "key":
		s.Kind = SourceKey
		for k := range KeyCount {
			if strings.EqualFold(Key(k).String(), name) {
				idx, found = int(k), true
				break
			}
		}
	case "mouse":
		s.Kind = SourceMouse
		idx, found = indexOf(mouseNames[:], name)
	case "pad":
		s.Kind = SourcePadButton
		idx, found = indexOf(padNames[:], name)
	case "axis":
		s.Kind = SourcePadAxis
		idx, found = indexOf(axisNames[:], name)
	default:
		return s, fmt.Errorf("input: unknown source kind %q", kind)
	}
	if !found {
		return s, fmt.Errorf("input: unknown %s %q", kind, name)
	}
	s.Code, s.Scale = idx, scale
	return s, nil
}

// MarshalText writes the source as String does.
func (s Source) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads what MarshalText wrote.
func (s *Source) UnmarshalText(b []byte) error {
	p, err := ParseSource(string(b))
	if err != nil {
		return err
	}
	*s = p
	return nil
}

// Actions maps named actions ("jump", "fire", "move_x") to the keys,
// buttons and axes that trigger them, so game code asks for actions and
// a settings screen rebinds them. An action bound to several sources
// fires from any; an axis action sums its sources (the D key and the A
// key with Neg, plus a stick) and clamps to -1..1. Bindings save and
// load as JSON, and Listen captures the next input for rebinding.
// Use Actions on the game loop goroutine; concurrent binding changes
// and queries require external synchronization.
type Actions struct {
	entries []binding      // one per name ever bound, in binding order
	index   map[string]int // name to its position in entries
	// Pad is which gamepad the pad sources read; the first by default.
	Pad int
	// DeadZone is the stick travel ignored around centre; zero means 0.2.
	DeadZone float32
}

// binding is one named action and the sources bound to it. A name keeps
// its place for the life of the map, even while nothing is bound to it,
// so an Action handle taken once stays valid across rebinding.
type binding struct {
	name    string
	sources []Source
}

// Action is a resolved handle to one named action. Looking the name up
// once at startup and keeping the handle spares the string hash on every
// query, which a game makes several times per action per frame. The zero
// Action is bound to nothing and reads as off.
//
// A handle stays valid across Bind, Rebind and Unbind: it names the
// action, not the sources behind it.
type Action struct {
	a  *Actions
	id int32 // one past the entry's index, so the zero Action is unbound
}

// NewActions makes an empty map.
func NewActions() *Actions { return &Actions{index: map[string]int{}} }

// Action resolves a name to a handle, which Value, Down, Pressed and
// Released read without hashing the name again. Resolve the handles a
// game uses once, in Init, and keep them:
//
//	g.jump = actions.Action("jump")
//	if g.jump.Pressed(ctx.Input) { ... }
//
// The name need not be bound yet; binding it later fills the same handle.
func (a *Actions) Action(name string) Action {
	return Action{a: a, id: int32(a.entry(name)) + 1}
}

// Name is the action the handle was resolved from, empty for the zero
// Action.
func (h Action) Name() string {
	if b := h.binding(); b != nil {
		return b.name
	}
	return ""
}

// binding is the handle's entry, or nil when it names nothing.
func (h Action) binding() *binding {
	if h.a == nil || h.id <= 0 || int(h.id) > len(h.a.entries) {
		return nil
	}
	return &h.a.entries[h.id-1]
}

// sources are the handle's bound sources, nil when it names nothing.
func (h Action) sources() []Source {
	if b := h.binding(); b != nil {
		return b.sources
	}
	return nil
}

// entry finds or makes the position of a name, so a handle taken for an
// unbound name still points somewhere Bind can fill in later.
func (a *Actions) entry(name string) int {
	if a.index == nil {
		a.index = map[string]int{}
	}
	if i, ok := a.index[name]; ok {
		return i
	}
	i := len(a.entries)
	a.entries = append(a.entries, binding{name: name})
	a.index[name] = i
	return i
}

// Bind adds sources to an action, keeping any it has.
func (a *Actions) Bind(action string, sources ...Source) {
	e := &a.entries[a.entry(action)]
	e.sources = append(e.sources, sources...)
}

// Rebind replaces an action's sources.
func (a *Actions) Rebind(action string, sources ...Source) {
	e := &a.entries[a.entry(action)]
	e.sources = append(e.sources[:0:0], sources...)
}

// Unbind removes an action's sources. Handles taken for the name stay
// valid and read as off until it is bound again.
func (a *Actions) Unbind(action string) {
	if i, ok := a.index[action]; ok {
		a.entries[i].sources = nil
	}
}

// Bindings returns an action's sources, for showing in a settings
// screen or a prompt ("press [Space]").
func (a *Actions) Bindings(action string) []Source {
	if i, ok := a.index[action]; ok {
		return a.entries[i].sources
	}
	return nil
}

// Bindings returns the handle's sources, as Actions.Bindings does.
func (h Action) Bindings() []Source { return h.sources() }

// Names returns every bound action, sorted.
func (a *Actions) Names() []string {
	names := make([]string, 0, len(a.entries))
	for i := range a.entries {
		if len(a.entries[i].sources) > 0 {
			names = append(names, a.entries[i].name)
		}
	}
	sort.Strings(names)
	return names
}

func (a *Actions) pad(s *State) *Gamepad {
	g := s.Gamepad(a.Pad)
	if g == nil || !g.Connected {
		return nil
	}
	return g
}

func (a *Actions) deadZone() float32 {
	if a.DeadZone <= 0 {
		return 0.2
	}
	return a.DeadZone
}

// value is what one source contributes right now.
func (a *Actions) value(s *State, src Source) float32 {
	scale := src.Scale
	if scale == 0 {
		scale = 1
	}
	switch src.Kind {
	case SourceKey:
		if src.Code >= 0 && src.Code < int(KeyCount) && s.KeyDown(Key(src.Code)) {
			return scale
		}
	case SourceMouse:
		if src.Code >= 0 && src.Code < int(MouseButtonCount) && s.MouseDown(MouseButton(src.Code)) {
			return scale
		}
	case SourcePadButton:
		if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadButtonCount) && g.Down(GamepadButton(src.Code)) {
			return scale
		}
	case SourcePadAxis:
		if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadAxisCount) {
			v := g.Axis(GamepadAxis(src.Code))
			dz := a.deadZone()
			if v > dz && scale > 0 {
				// The positive half; a source with a negative scale reads
				// the other half only, so a Neg pair never cancels.
				return (v - dz) / (1 - dz) * scale
			}
			if v < -dz && scale < 0 {
				// The negative half is its own source: its travel times
				// the source's negative scale.
				return (-v - dz) / (1 - dz) * scale
			}
			if v < -dz && scale > 0 {
				return 0 // this source reads the positive side only
			}
		}
	}
	return 0
}

// Value is the action's value now: 1 while a bound key or button is
// held, a stick's travel for an axis, the sum over sources clamped to
// -1..1. An axis source contributes its positive side; bind its Neg for
// the other, so "move_x" is (D, A.Neg, LeftX, LeftX.Neg).
//
// This looks the name up on every call. A game querying the same actions
// every frame resolves them once with Action and asks the handle.
func (a *Actions) Value(s *State, action string) float32 {
	return a.valueOf(s, a.Bindings(action))
}

// Value is the handle's value now, as Actions.Value describes it.
func (h Action) Value(s *State) float32 {
	if h.a == nil {
		return 0
	}
	return h.a.valueOf(s, h.sources())
}

// valueOf sums a set of sources and clamps the total.
func (a *Actions) valueOf(s *State, sources []Source) float32 {
	var v float32
	for _, src := range sources {
		v += a.value(s, src)
	}
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}

// Down reports whether the action is on: any bound key or button held,
// or a bound axis past half.
func (a *Actions) Down(s *State, action string) bool {
	v := a.Value(s, action)
	return v > 0.5 || v < -0.5
}

// Down reports whether the handle's action is on, as Actions.Down does.
func (h Action) Down(s *State) bool {
	v := h.Value(s)
	return v > 0.5 || v < -0.5
}

// Pressed reports whether any of the action's sources went on since the
// last update: a key or button press, or an axis crossing half.
func (a *Actions) Pressed(s *State, action string) bool {
	return a.pressedOf(s, a.Bindings(action))
}

// Pressed reports whether the handle's action went on since the last
// update, as Actions.Pressed does.
func (h Action) Pressed(s *State) bool {
	if h.a == nil {
		return false
	}
	return h.a.pressedOf(s, h.sources())
}

func (a *Actions) pressedOf(s *State, sources []Source) bool {
	for _, src := range sources {
		switch src.Kind {
		case SourceKey:
			if src.Code >= 0 && src.Code < int(KeyCount) && s.KeyPressed(Key(src.Code)) {
				return true
			}
		case SourceMouse:
			if src.Code >= 0 && src.Code < int(MouseButtonCount) && s.MousePressed(MouseButton(src.Code)) {
				return true
			}
		case SourcePadButton:
			if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadButtonCount) && g.Pressed(GamepadButton(src.Code)) {
				return true
			}
		case SourcePadAxis:
			if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadAxisCount) {
				code := GamepadAxis(src.Code)
				now, before := g.Axis(code), g.prevAxes[code]
				if src.Scale < 0 {
					now, before = -now, -before
				}
				if now > 0.5 && before <= 0.5 {
					return true
				}
			}
		}
	}
	return false
}

// Released reports whether any of the action's sources went off since
// the last update.
func (a *Actions) Released(s *State, action string) bool {
	return a.releasedOf(s, a.Bindings(action))
}

// Released reports whether the handle's action went off since the last
// update, as Actions.Released does.
func (h Action) Released(s *State) bool {
	if h.a == nil {
		return false
	}
	return h.a.releasedOf(s, h.sources())
}

func (a *Actions) releasedOf(s *State, sources []Source) bool {
	for _, src := range sources {
		switch src.Kind {
		case SourceKey:
			if src.Code >= 0 && src.Code < int(KeyCount) && s.KeyReleased(Key(src.Code)) {
				return true
			}
		case SourceMouse:
			if src.Code >= 0 && src.Code < int(MouseButtonCount) && s.MouseReleased(MouseButton(src.Code)) {
				return true
			}
		case SourcePadButton:
			if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadButtonCount) && g.Released(GamepadButton(src.Code)) {
				return true
			}
		case SourcePadAxis:
			if g := a.pad(s); g != nil && src.Code >= 0 && src.Code < int(GamepadAxisCount) {
				code := GamepadAxis(src.Code)
				now, before := g.Axis(code), g.prevAxes[code]
				if src.Scale < 0 {
					now, before = -now, -before
				}
				if now <= 0.5 && before > 0.5 {
					return true
				}
			}
		}
	}
	return false
}

// Listen returns the first input that went on this update, for a
// rebinding screen: press a key, click a button, push a stick past
// half. Escape is never captured, so a screen can cancel with it.
func (a *Actions) Listen(s *State) (Source, bool) {
	for k := range KeyCount {
		if k != KeyEscape && s.KeyPressed(k) {
			return KeySource(k), true
		}
	}
	for b := range MouseButtonCount {
		if s.MousePressed(b) {
			return MouseSource(b), true
		}
	}
	if g := a.pad(s); g != nil {
		for b := range GamepadButtonCount {
			if g.Pressed(b) {
				return PadButton(b), true
			}
		}
		for ax := range GamepadAxisCount {
			now, before := g.Axis(ax), g.prevAxes[ax]
			if now > 0.5 && before <= 0.5 {
				return PadAxis(ax), true
			}
			if now < -0.5 && before >= -0.5 {
				return PadAxis(ax).Neg(), true
			}
		}
	}
	return Source{}, false
}

// MarshalJSON writes the bindings as {"action": ["key:Space", ...]}.
func (a *Actions) MarshalJSON() ([]byte, error) {
	out := map[string][]string{}
	for i := range a.entries {
		e := &a.entries[i]
		for _, s := range e.sources {
			out[e.name] = append(out[e.name], s.String())
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads what MarshalJSON wrote, replacing the bindings.
// Handles taken with Action keep pointing at their names, so loading a
// player's saved bindings does not invalidate them.
func (a *Actions) UnmarshalJSON(b []byte) error {
	var in map[string][]string
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	parsed := make(map[string][]Source, len(in))
	for name, texts := range in {
		for _, t := range texts {
			s, err := ParseSource(t)
			if err != nil {
				return err
			}
			parsed[name] = append(parsed[name], s)
		}
	}
	for i := range a.entries {
		a.entries[i].sources = nil
	}
	for name, srcs := range parsed {
		a.entries[a.entry(name)].sources = srcs
	}
	return nil
}
