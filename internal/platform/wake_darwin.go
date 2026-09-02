package platform

import "github.com/ebitengine/purego/objc"

const nsEventTypeAppDefined = 15

var (
	selOtherEvent = objc.RegisterName("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:")
	selPostEvent  = objc.RegisterName("postEvent:atStart:")
)

// Wake makes a blocked Poll return, delivering an EventWake. It is safe
// to call from any goroutine, which is how timers, network replies and
// finished loads prod a turn-based game that is asleep in the OS.
func (a *App) Wake() {
	c := a.c
	pool := objc.ID(c.NSAutoreleasePool).Send(c.sel.new)
	defer pool.Send(c.sel.drain)
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selOtherEvent, uint(nsEventTypeAppDefined), nsPoint{}, uint(0), float64(0),
		int(0), objc.ID(0), int16(0), int(0), int(0))
	if ev != 0 {
		a.nsApp.Send(selPostEvent, ev, true)
	}
}
