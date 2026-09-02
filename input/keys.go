// Package input is the state of the keyboard, mouse and gamepads as a
// game reads it, and the vocabulary the platform layers fill it with.
//
// State holds every key, button, stick and the pointer. Levels (KeyDown,
// MouseDown, Gamepad.Down) say what is held now; edges (KeyPressed,
// KeyReleased and their mouse and gamepad twins) say what changed since
// the last update and are cleared by the engine after each one, so a
// press is seen exactly once however many updates or frames it spans.
// During Draw the edges cover the whole drawn frame instead, so an
// immediate-mode interface built in Draw misses nothing. KeyHeld,
// KeysDown, KeyRepeated and MouseDoubleClicked cover charged shots,
// combos, scrolling menus and double clicks; Chars and Composition carry
// typed text with the keyboard layout and input method applied.
//
// Keys are named by physical position (KeyW is the key in W's place on
// a US keyboard whatever it prints), which is what movement bindings
// want; Key.String names them for prompts. Actions maps named actions
// to any keys, buttons and axes, with dead zones, rebinding through
// Listen and JSON bindings for a settings file, so game code asks for
// "jump" and takes a gamepad without a second set of checks.
//
// The Feed* methods and EndUpdate, EndFrame and SetDrawing are the
// engine's plumbing; a game under bunyip.Run never calls them, and a
// test drives a State through them.
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
