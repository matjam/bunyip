package platform

import (
	"fmt"
	"unicode"

	"github.com/ebitengine/purego/objc"
	"structs"
)

// nsRange mirrors NSRange: a location and length in UTF-16 units.
type nsRange struct {
	_                structs.HostLayout
	Location, Length uint64
}

const nsNotFound = uint64(0x7fffffffffffffff) // NSIntegerMax

// textRect is where text is being entered, in points from the top-left of
// the content area.
type textRect struct{ X, Y, W, H float64 }

// textSel holds the selectors only the text-input view uses.
type textSel struct {
	interpretKeyEvents, arrayWithObject, array, string, respondsToSelector,
	makeFirstResponder, convertRectToScreen, convertRectToView objc.SEL
}

// registerViewClass creates the NSView subclass that hosts the Metal layer
// and adopts NSTextInputClient. Key presses reach it through
// interpretKeyEvents:, which runs them through the active input method, so
// dead keys and IMEs for Japanese, Chinese or Korean deliver composition
// (EventCompose) and committed text (EventChar) rather than raw key
// characters.
func (a *App) registerViewClass() (objc.Class, error) {
	c := a.c
	s := &a.tsel
	for name, dst := range map[string]*objc.SEL{
		"interpretKeyEvents:": &s.interpretKeyEvents, "arrayWithObject:": &s.arrayWithObject,
		"array": &s.array, "string": &s.string, "respondsToSelector:": &s.respondsToSelector,
		"makeFirstResponder:": &s.makeFirstResponder, "convertRectToScreen:": &s.convertRectToScreen,
		"convertRect:toView:": &s.convertRectToView,
	} {
		*dst = objc.RegisterName(name)
	}
	nsArray := objc.GetClass("NSArray")
	windowOf := func(self objc.ID) *Window {
		return a.views[self]
	}
	// text returns the Go string of an NSString or NSAttributedString.
	text := func(obj objc.ID) string {
		if obj == 0 {
			return ""
		}
		if objc.Send[bool](obj, s.respondsToSelector, s.string) {
			obj = obj.Send(s.string)
		}
		return objc.Send[string](obj, c.sel.UTF8String)
	}
	cls, err := objc.RegisterClass("BunyipView", c.NSView,
		[]*objc.Protocol{objc.GetProtocol("NSTextInputClient")}, nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("acceptsFirstResponder"), Fn: func(_ objc.ID, _ objc.SEL) bool { return true }},
			{Cmd: objc.RegisterName("keyDown:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) {
				self.Send(s.interpretKeyEvents, objc.ID(nsArray).Send(s.arrayWithObject, ev))
			}},
			{Cmd: objc.RegisterName("doCommandBySelector:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.SEL) {
				// Editing commands (moveLeft:, deleteBackward:, insertNewline:)
				// reach games as key events; swallowing them here stops the
				// beep an unhandled command would make.
			}},
			{Cmd: objc.RegisterName("insertText:replacementRange:"), Fn: func(self objc.ID, _ objc.SEL, str objc.ID, _ nsRange) {
				w := windowOf(self)
				if w == nil {
					return
				}
				if w.marked != "" {
					w.marked = ""
					a.push(Event{Kind: EventCompose, Window: w})
				}
				for _, r := range text(str) {
					if r >= 0xF700 && r <= 0xF8FF { // AppKit's private-use range for function keys
						continue
					}
					if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
						continue
					}
					a.push(Event{Kind: EventChar, Window: w, Rune: r, Mods: a.mods})
				}
			}},
			{Cmd: objc.RegisterName("setMarkedText:selectedRange:replacementRange:"), Fn: func(self objc.ID, _ objc.SEL, str objc.ID, _, _ nsRange) {
				if w := windowOf(self); w != nil {
					w.marked = text(str)
					a.push(Event{Kind: EventCompose, Window: w, Text: w.marked})
				}
			}},
			{Cmd: objc.RegisterName("unmarkText"), Fn: func(self objc.ID, _ objc.SEL) {
				if w := windowOf(self); w != nil && w.marked != "" {
					w.marked = ""
					a.push(Event{Kind: EventCompose, Window: w})
				}
			}},
			{Cmd: objc.RegisterName("hasMarkedText"), Fn: func(self objc.ID, _ objc.SEL) bool {
				w := windowOf(self)
				return w != nil && w.marked != ""
			}},
			{Cmd: objc.RegisterName("markedRange"), Fn: func(self objc.ID, _ objc.SEL) nsRange {
				if w := windowOf(self); w != nil && w.marked != "" {
					return nsRange{Location: 0, Length: uint64(utf16Len(w.marked))}
				}
				return nsRange{Location: nsNotFound}
			}},
			{Cmd: objc.RegisterName("selectedRange"), Fn: func(self objc.ID, _ objc.SEL) nsRange {
				if w := windowOf(self); w != nil {
					return nsRange{Location: uint64(utf16Len(w.marked))}
				}
				return nsRange{Location: nsNotFound}
			}},
			{Cmd: objc.RegisterName("validAttributesForMarkedText"), Fn: func(_ objc.ID, _ objc.SEL) objc.ID {
				return objc.ID(nsArray).Send(s.array)
			}},
			{Cmd: objc.RegisterName("attributedSubstringForProposedRange:actualRange:"), Fn: func(_ objc.ID, _ objc.SEL, _ nsRange, _ uintptr) objc.ID {
				return 0
			}},
			{Cmd: objc.RegisterName("characterIndexForPoint:"), Fn: func(_ objc.ID, _ objc.SEL, _ nsPoint) uint64 {
				return nsNotFound
			}},
			{Cmd: objc.RegisterName("firstRectForCharacterRange:actualRange:"), Fn: func(self objc.ID, _ objc.SEL, _ nsRange, _ uintptr) nsRect {
				w := windowOf(self)
				if w == nil {
					return nsRect{}
				}
				// The game's rectangle counts from the top-left; the view
				// counts from the bottom-left.
				r := w.inputRect
				local := nsRect{Origin: nsPoint{X: r.X, Y: float64(w.height) - r.Y - r.H}, Size: nsSize{Width: r.W, Height: r.H}}
				inWindow := objc.Send[nsRect](self, s.convertRectToView, local, objc.ID(0))
				return objc.Send[nsRect](w.nsWindow, s.convertRectToScreen, inWindow)
			}},
		})
	if err != nil {
		return 0, fmt.Errorf("platform: register view class: %w", err)
	}
	return cls, nil
}

// utf16Len counts the UTF-16 units NSString would use for s.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// SetTextInputRect tells the input method where text is being entered, in
// points from the top-left of the content area, so candidate windows open
// beside the field.
func (w *Window) SetTextInputRect(x, y, width, height float64) {
	w.inputRect = textRect{X: x, Y: y, W: width, H: height}
}
