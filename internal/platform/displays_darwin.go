package platform

import (
	"github.com/ebitengine/purego/objc"
	"image"
)

// Displays enumerates NSScreen and the CoreGraphics modes for its display ID.
// Bounds are AppKit logical points with the main display's top edge as origin.
func (a *App) Displays() ([]Display, error) {
	var copyModes func(uint32, uintptr) uintptr
	var copyCurrent func(uint32) uintptr
	var count func(uintptr) int
	var item func(uintptr, int) uintptr
	var release func(uintptr)
	var width, height func(uintptr) uintptr
	var rate func(uintptr) float64
	for name, target := range map[string]any{"CGDisplayCopyAllDisplayModes": &copyModes, "CGDisplayCopyDisplayMode": &copyCurrent, "CFArrayGetCount": &count, "CFArrayGetValueAtIndex": &item, "CFRelease": &release, "CGDisplayModeGetPixelWidth": &width, "CGDisplayModeGetPixelHeight": &height, "CGDisplayModeGetRefreshRate": &rate} {
		if err := loadCoreGraphics(name, target); err != nil {
			return nil, err
		}
	}
	screens := objc.ID(controls().NSScreen).Send(objc.RegisterName("screens"))
	n := objc.Send[uint](screens, a.c.sel.count)
	if n == 0 {
		return nil, nil
	}
	main := screens.Send(a.c.sel.objectAtIndex, uint(0))
	mainFrame := objc.Send[nsRect](main, a.c.sel.frame)
	top := mainFrame.Origin.Y + mainFrame.Size.Height
	mode := func(p uintptr) VideoMode {
		return VideoMode{Width: int(width(p)), Height: int(height(p)), RefreshHz: rate(p)}
	}
	var out []Display
	for i := uint(0); i < n; i++ {
		s := screens.Send(a.c.sel.objectAtIndex, i)
		frame := objc.Send[nsRect](s, a.c.sel.frame)
		name := s.Send(objc.RegisterName("localizedName"))
		d := Display{Name: objc.Send[string](name, a.c.sel.UTF8String), Scale: objc.Send[float64](s, objc.RegisterName("backingScaleFactor")), BoundsKnown: true}
		x, y := int(frame.Origin.X), int(top-frame.Origin.Y-frame.Size.Height)
		d.Bounds = image.Rect(x, y, x+int(frame.Size.Width), y+int(frame.Size.Height))
		desc := s.Send(objc.RegisterName("deviceDescription"))
		number := desc.Send(objc.RegisterName("objectForKey:"), a.c.nsString("NSScreenNumber"))
		id := objc.Send[uint32](number, objc.RegisterName("unsignedIntValue"))
		if current := copyCurrent(id); current != 0 {
			d.Current = mode(current)
			release(current)
		}
		if list := copyModes(id, 0); list != 0 {
			for j := 0; j < count(list); j++ {
				v := mode(item(list, j))
				found := false
				for _, m := range d.Modes {
					if m == v {
						found = true
						break
					}
				}
				if !found {
					d.Modes = append(d.Modes, v)
				}
			}
			release(list)
		}
		out = append(out, d)
	}
	return out, nil
}
