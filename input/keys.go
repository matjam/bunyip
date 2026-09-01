// Package input defines the platform-independent vocabulary for keyboards,
// mice and gamepads: key codes by physical position, modifier bits and
// button numbers. Platform layers translate native events into these values.
package input

// Key identifies a physical key by position, independent of the active
// keyboard layout, using the names of the equivalent US layout key.
type Key uint8

const (
	KeyUnknown Key = iota
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	KeySpace
	KeyEnter
	KeyEscape
	KeyTab
	KeyBackspace
	KeyDelete
	KeyInsert
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyMinus
	KeyEqual
	KeyLeftBracket
	KeyRightBracket
	KeyBackslash
	KeySemicolon
	KeyApostrophe
	KeyGrave
	KeyComma
	KeyPeriod
	KeySlash
	KeyCapsLock
	KeyNumLock
	KeyScrollLock
	KeyPrintScreen
	KeyPause
	KeyMenu
	KeyLeftShift
	KeyRightShift
	KeyLeftControl
	KeyRightControl
	KeyLeftAlt
	KeyRightAlt
	KeyLeftSuper
	KeyRightSuper
	KeyFunction
	KeyWorld1 // the key left of Z on ISO layouts
	KeyWorld2
	KeyKeypad0
	KeyKeypad1
	KeyKeypad2
	KeyKeypad3
	KeyKeypad4
	KeyKeypad5
	KeyKeypad6
	KeyKeypad7
	KeyKeypad8
	KeyKeypad9
	KeyKeypadDecimal
	KeyKeypadDivide
	KeyKeypadMultiply
	KeyKeypadSubtract
	KeyKeypadAdd
	KeyKeypadEnter
	KeyKeypadEqual
	KeyCount // number of key codes; not a key
)

// Mods is a set of modifier keys held during an event.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModControl
	ModAlt   // Option on macOS
	ModSuper // Command on macOS, the Windows key elsewhere
	ModCapsLock
	ModNumLock
)

// MouseButton numbers buttons from the left; Left, Right and Middle are the
// conventional three and further buttons follow in device order.
type MouseButton uint8

const (
	MouseLeft MouseButton = iota
	MouseRight
	MouseMiddle
	MouseButton4
	MouseButton5
	MouseButtonCount
)
