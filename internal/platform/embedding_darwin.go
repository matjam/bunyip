package platform

import (
	"errors"
	"math"

	"github.com/ebitengine/purego/objc"
)

func (a *App) newEmbedded(cfg Config) (*Window, error) {
	p := cfg.Parent
	if p.Backend != 2 || p.Handle == 0 {
		return nil, ErrUnsupported
	}
	parent := objc.ID(p.Handle)
	if !objc.Send[bool](parent, selIsKindOfClass, a.c.NSView) {
		return nil, errors.New("platform: Cocoa parent must be an NSView")
	}
	win := parent.Send(a.c.sel.window)
	if win == 0 {
		return nil, errors.New("platform: Cocoa parent must be attached to an NSWindow")
	}
	a.hosted = true
	frame := objc.Send[nsRect](parent, a.c.sel.bounds)
	w := &Window{app: a, nsWindow: win, parent: parent.Send(selRetain), visible: true, embeddedFocus: true}
	w.view = objc.ID(a.view).Send(a.c.sel.alloc).Send(a.c.sel.initWithFrame, frame)
	if w.view == 0 {
		w.parent.Send(a.c.sel.release)
		return nil, errors.New("platform: embedded NSView creation failed")
	}
	w.layer = objc.ID(a.c.CAMetalLayer).Send(a.c.sel.layer)
	w.view.Send(a.c.sel.setLayer, w.layer)
	w.view.Send(a.c.sel.setWantsLayer, true)
	w.view.Send(selSetAutoresizingMask, uint64(2|16))
	a.views[w.view] = w
	parent.Send(selAddSubview, w.view)
	w.updateGeometry()
	return w, nil
}

func (w *Window) SetBounds(x, y, width, height int) error {
	if w.parent == 0 {
		return ErrUnsupported
	}
	if width <= 0 || height <= 0 || math.Abs(float64(x)) > math.MaxInt32 || math.Abs(float64(y)) > math.MaxInt32 || width > math.MaxInt32 || height > math.MaxInt32 {
		return errors.New("platform: invalid embedded bounds")
	}
	b := objc.Send[nsRect](w.parent, w.app.c.sel.bounds)
	py := float64(y)
	if !objc.Send[bool](w.parent, selIsFlipped) {
		py = b.Size.Height - float64(y+height)
	}
	w.view.Send(selSetAutoresizingMask, uint64(0))
	w.view.Send(selSetFrame, nsRect{Origin: nsPoint{X: b.Origin.X + float64(x), Y: b.Origin.Y + py}, Size: nsSize{Width: float64(width), Height: float64(height)}})
	w.manualBounds = true
	w.updateGeometry()
	return nil
}

// windowForView walks up from view to the nearest view that belongs to a
// Window.
func (a *App) windowForView(view objc.ID) *Window {
	for view != 0 {
		if w := a.views[view]; w != nil {
			return w
		}
		if !sendBool1(view, selIsKindOfClass, uintptr(a.c.NSView)) {
			break
		}
		view = sendID(view, selSuperview)
	}
	return nil
}

// windowForEvent finds the Window an event belongs to. Keys go to the
// first responder's window. A mouse event goes to the view under the
// pointer, which only needs asking for when views are embedded in a host
// or a press starts a drag: otherwise every content view fills its own
// NSWindow and the event's window is the answer.
func (a *App) windowForEvent(ev objc.ID, kind uint) *Window {
	up := kind == nsEventTypeLeftMouseUp || kind == nsEventTypeRightMouseUp || kind == nsEventTypeOtherMouseUp
	drag := kind == nsEventTypeLeftMouseDragged || kind == nsEventTypeRightMouseDragged || kind == nsEventTypeOtherMouseDragged
	if (up || drag) && a.mouseTarget != nil && !a.mouseTarget.closed {
		w := a.mouseTarget
		if up {
			a.mouseTarget = nil
		}
		return w
	}
	win := sendID(ev, a.c.sel.window)
	if win == 0 {
		return nil
	}
	down := kind == nsEventTypeLeftMouseDown || kind == nsEventTypeRightMouseDown || kind == nsEventTypeOtherMouseDown
	if kind == nsEventTypeKeyDown || kind == nsEventTypeKeyUp || kind == nsEventTypeFlagsChanged {
		if w := a.windowForView(sendID(win, selFirstResponder)); w != nil {
			return w
		}
	} else if a.hosted || down {
		content := sendID(win, selContentView)
		p := msgSendPoint(ev, a.c.sel.locationInWindow)
		if w := a.windowForView(msgSendIDPoint(content, selHitTest, p)); w != nil {
			if down {
				a.mouseTarget = w
				if w.parent != 0 {
					sendID1(win, a.tsel.makeFirstResponder, uintptr(w.view))
				}
			}
			return w
		}
	}
	return a.windows[win]
}

// syncEmbedded reads what a host can change without telling the view:
// its place in the hierarchy, its size and scale, whether it can be seen
// and whether it has the keyboard. It runs every Poll.
func (a *App) syncEmbedded() {
	for _, w := range a.views {
		if w.parent == 0 || w.closed {
			continue
		}
		if sendID(w.view, a.c.sel.window) != w.nsWindow || sendID(w.view, selSuperview) != w.parent {
			w.hostLost = true
			a.push(Event{Kind: EventClose, Window: w})
			continue
		}
		b := msgSendRect(w.view, a.c.sel.bounds)
		scale := msgSendF64(w.nsWindow, a.c.sel.backingScaleFactor)
		if int(b.Size.Width) != w.width || int(b.Size.Height) != w.height || scale != w.scale {
			w.updateGeometry()
		}
		visible := !sendBool(w.view, selIsHiddenOrHasHiddenAncestor) && !sendBool(w.nsWindow, selIsMiniaturized) &&
			uint64(sendUint(w.nsWindow, selOcclusionState))&nsWindowOcclusionStateVisible != 0
		if visible != w.visible {
			w.visible = visible
			a.push(Event{Kind: EventVisible, Window: w, Visible: visible})
		}
		focused := a.windowForView(sendID(w.nsWindow, selFirstResponder)) == w && sendBool(w.nsWindow, selIsKeyWindow)
		if focused != w.embeddedFocus {
			w.embeddedFocus = focused
			a.push(Event{Kind: EventFocus, Window: w, Focused: focused})
		}
	}
}
