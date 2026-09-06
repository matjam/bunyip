package input

// KeySymbol is a layout-dependent logical key. Namespaces are disjoint:
// text:<UTF-8> is unmodified printable text, key:<name> is a named nonprinting
// key, and dead:<native name> is a dead key. Empty means unknown. Dead-key
// names are backend-specific; these symbols do not describe IME composition.
// Named keys use physical-key names where equivalents exist (for example
// key:Enter), or a native name otherwise. Printable names cannot collide.
type KeySymbol string

// TextSymbol constructs a printable logical key for reverse lookup. Empty
// text produces the unknown symbol. Text may contain more than one rune.
func TextSymbol(text string) KeySymbol {
	if text == "" {
		return ""
	}
	return KeySymbol("text:" + text)
}

// KeyDescription describes one physical key in a keyboard-layout snapshot.
type KeyDescription struct {
	Label  string    // native/layout label, empty if unavailable
	Symbol KeySymbol // unmodified level-zero logical symbol, empty if unknown
}

// KeyboardLayout is a snapshot of the active native layout. Refresh it when
// displaying bindings after a layout change. Queries do not change keyboard
// modifiers, dead-key state or IME composition. Symbols use the native layout's
// unmodified level with locks off: on Windows and common XKB layouts keypad
// digits therefore map to navigation, while macOS keypads remain numeric.
// Keys are indexed by physical Key. This is for binding UI, not per-frame polling.
type KeyboardLayout struct {
	Name string // system-provided layout name or identifier, empty if unavailable
	Keys [KeyCount]KeyDescription
}

// Label returns the layout label, falling back to the stable physical name
// when no native label was supplied. The fallback is not a logical mapping.
func (l KeyboardLayout) Label(key Key) string {
	if key < KeyCount && l.Keys[key].Label != "" {
		return l.Keys[key].Label
	}
	return key.String()
}

// Symbol returns the logical symbol, or empty for an unmapped key.
func (l KeyboardLayout) Symbol(key Key) KeySymbol {
	if key >= KeyCount {
		return ""
	}
	return l.Keys[key].Symbol
}

// KeysFor returns every physical key producing symbol, in Key order. A symbol
// may have several keys (for example a digit and its keypad equivalent).
// Unknown symbols return no keys.
func (l KeyboardLayout) KeysFor(symbol KeySymbol) []Key {
	if symbol == "" {
		return nil
	}
	var keys []Key
	for key := KeyUnknown + 1; key < KeyCount; key++ {
		if l.Keys[key].Symbol == symbol {
			keys = append(keys, key)
		}
	}
	return keys
}
