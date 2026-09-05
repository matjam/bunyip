package platform

import (
	"errors"
	"fmt"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"sync"
)

func (w *Window) Capabilities() WindowCapabilities {
	return WindowCapabilities{Resize: true, Show: true, Hide: true, Focus: true, AlwaysOnTop: true, CursorImage: true, PointerPosition: true}
}
func (w *Window) SetSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("platform: window dimensions must be positive")
	}
	w.nsWindow.Send(objc.RegisterName("setContentSize:"), nsSize{Width: float64(width), Height: float64(height)})
	return nil
}
func (w *Window) Show() error {
	w.nsWindow.Send(objc.RegisterName("orderFront:"), objc.ID(0))
	return nil
}
func (w *Window) Hide() error {
	w.nsWindow.Send(objc.RegisterName("orderOut:"), objc.ID(0))
	return nil
}
func (w *Window) RequestFocus() error {
	w.nsWindow.Send(w.app.c.sel.makeKeyAndOrderFront, objc.ID(0))
	return nil
}
func (w *Window) SetPointerPosition(x, y float64) error {
	var warp func(nsPoint) int32
	if err := loadCoreGraphics("CGWarpMouseCursorPosition", &warp); err != nil {
		return err
	}
	// NSWindow converts its content-base point into global AppKit coordinates;
	// Quartz uses the main screen's top edge as its Y origin.
	_, height := w.Size()
	p := objc.Send[nsPoint](w.nsWindow, objc.RegisterName("convertPointToScreen:"), nsPoint{X: x, Y: float64(height) - y})
	p.Y = w.screenHeight() - p.Y
	if status := warp(p); status != 0 {
		return fmt.Errorf("platform: warp pointer: CoreGraphics status %d", status)
	}
	return nil
}

func loadCoreGraphics(name string, target any) error {
	lib, err := coreGraphicsLibrary()
	if err != nil {
		return fmt.Errorf("%w: CoreGraphics: %v", ErrUnsupported, err)
	}
	sym, err := purego.Dlsym(lib, name)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnsupported, name, err)
	}
	purego.RegisterFunc(target, sym)
	return nil
}

var coreGraphicsLibrary = sync.OnceValues(func() (uintptr, error) {
	return purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
})

func (w *Window) RefreshCursor() {
	if w.cursorImage != 0 {
		w.cursorImage.Send(controls().set)
	} else {
		w.SetCursor(w.shape)
	}
}
