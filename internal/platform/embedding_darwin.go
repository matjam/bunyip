package platform

import (
	"errors"
	"github.com/ebitengine/purego/objc"
	"math"
)

func (a *App) newEmbedded(cfg Config) (*Window, error) {
	p := cfg.Parent
	if p.Backend != 2 || p.Handle == 0 {
		return nil, ErrUnsupported
	}
	parent := objc.ID(p.Handle)
	if !objc.Send[bool](parent, objc.RegisterName("isKindOfClass:"), a.c.NSView) {
		return nil, errors.New("platform: Cocoa parent must be an NSView")
	}
	win := parent.Send(a.c.sel.window)
	if win == 0 {
		return nil, errors.New("platform: Cocoa parent must be attached to an NSWindow")
	}
	a.hosted = true
	frame := objc.Send[nsRect](parent, a.c.sel.bounds)
	w := &Window{app: a, nsWindow: win, parent: parent.Send(objc.RegisterName("retain")), visible: true, embeddedFocus: true}
	w.view = objc.ID(a.view).Send(a.c.sel.alloc).Send(a.c.sel.initWithFrame, frame)
	if w.view == 0 {
		w.parent.Send(a.c.sel.release)
		return nil, errors.New("platform: embedded NSView creation failed")
	}
	w.layer = objc.ID(a.c.CAMetalLayer).Send(a.c.sel.layer)
	w.view.Send(a.c.sel.setLayer, w.layer)
	w.view.Send(a.c.sel.setWantsLayer, true)
	w.view.Send(objc.RegisterName("setAutoresizingMask:"), uint64(2|16))
	a.views[w.view] = w
	parent.Send(objc.RegisterName("addSubview:"), w.view)
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
	if !objc.Send[bool](w.parent, objc.RegisterName("isFlipped")) {
		py = b.Size.Height - float64(y+height)
	}
	w.view.Send(objc.RegisterName("setAutoresizingMask:"), uint64(0))
	w.view.Send(objc.RegisterName("setFrame:"), nsRect{Origin: nsPoint{X: b.Origin.X + float64(x), Y: b.Origin.Y + py}, Size: nsSize{Width: float64(width), Height: float64(height)}})
	w.manualBounds = true
	w.updateGeometry()
	return nil
}

func (a *App) windowForView(view objc.ID) *Window {
	for view != 0 {
		if w := a.views[view]; w != nil {
			return w
		}
		if !objc.Send[bool](view, objc.RegisterName("isKindOfClass:"), a.c.NSView) {
			break
		}
		view = view.Send(objc.RegisterName("superview"))
	}
	return nil
}

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
	win := ev.Send(a.c.sel.window)
	if win == 0 {
		return nil
	}
	if kind == nsEventTypeKeyDown || kind == nsEventTypeKeyUp || kind == nsEventTypeFlagsChanged {
		if w := a.windowForView(win.Send(objc.RegisterName("firstResponder"))); w != nil {
			return w
		}
	} else {
		content := win.Send(objc.RegisterName("contentView"))
		p := objc.Send[nsPoint](ev, a.c.sel.locationInWindow)
		if w := a.windowForView(content.Send(objc.RegisterName("hitTest:"), p)); w != nil {
			if kind == nsEventTypeLeftMouseDown || kind == nsEventTypeRightMouseDown || kind == nsEventTypeOtherMouseDown {
				a.mouseTarget = w
				if w.parent != 0 {
					win.Send(a.tsel.makeFirstResponder, w.view)
				}
			}
			return w
		}
	}
	return a.windows[win]
}

func (a *App) syncEmbedded() {
	for _, w := range a.views {
		if w.parent == 0 || w.closed {
			continue
		}
		if w.view.Send(a.c.sel.window) != w.nsWindow || w.view.Send(objc.RegisterName("superview")) != w.parent {
			w.hostLost = true
			a.push(Event{Kind: EventClose, Window: w})
			continue
		}
		b := objc.Send[nsRect](w.view, a.c.sel.bounds)
		scale := objc.Send[float64](w.nsWindow, a.c.sel.backingScaleFactor)
		if int(b.Size.Width) != w.width || int(b.Size.Height) != w.height || scale != w.scale {
			w.updateGeometry()
		}
		visible := !objc.Send[bool](w.view, objc.RegisterName("isHiddenOrHasHiddenAncestor")) && !objc.Send[bool](w.nsWindow, objc.RegisterName("isMiniaturized")) && objc.Send[uint64](w.nsWindow, selOcclusionState)&nsWindowOcclusionStateVisible != 0
		if visible != w.visible {
			w.visible = visible
			a.push(Event{Kind: EventVisible, Window: w, Visible: visible})
		}
		focused := a.windowForView(w.nsWindow.Send(objc.RegisterName("firstResponder"))) == w && objc.Send[bool](w.nsWindow, objc.RegisterName("isKeyWindow"))
		if focused != w.embeddedFocus {
			w.embeddedFocus = focused
			a.push(Event{Kind: EventFocus, Window: w, Focused: focused})
		}
	}
}
