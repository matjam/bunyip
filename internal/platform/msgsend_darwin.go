package platform

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Message sends for the paths that run every frame.
//
// objc.ID.Send and objc.Send[T] take their arguments as ...any and
// objc.Send[T] also builds a reflection-based function on every call, so
// each send costs a microsecond and 5 to 18 allocations. The event loop
// sends dozens a frame, so it uses two cheaper forms instead:
//
//   - A send whose arguments and result are all integers or pointers goes
//     straight to objc_msgSend through purego.SyscallN, which allocates
//     nothing when its argument slice already lives on the heap (call
//     below).
//   - A send that passes or returns a floating-point value or a struct
//     goes through a function registered once against objc_msgSend, one
//     per signature. purego still reflects to make the call, which costs
//     four allocations, but not the registration.
//
// On amd64 a struct larger than 16 bytes comes back through a hidden
// pointer, which objc_msgSend_stret expects, so msgSendRect is
// registered against it there. arm64 has one objc_msgSend for every
// result, and on both a float, a double or a 16-byte NSPoint comes back
// in registers from the plain objc_msgSend.
var (
	msgSendAddr, poolPushAddr, poolPopAddr uintptr

	// msgSendF32 sends a message returning a float.
	msgSendF32 func(objc.ID, objc.SEL) float32
	// msgSendF64 sends a message returning a double.
	msgSendF64 func(objc.ID, objc.SEL) float64
	// msgSendPoint sends a message returning an NSPoint.
	msgSendPoint func(objc.ID, objc.SEL) nsPoint
	// msgSendPointID sends a message taking an NSPoint and an object and
	// returning an NSPoint: convertPoint:fromView:.
	msgSendPointID func(objc.ID, objc.SEL, nsPoint, objc.ID) nsPoint
	// msgSendIDPoint sends a message taking an NSPoint and returning an
	// object: hitTest:.
	msgSendIDPoint func(objc.ID, objc.SEL, nsPoint) objc.ID
	// msgSendIDF64 sends a message taking a double and returning an
	// object: dateWithTimeIntervalSinceNow:.
	msgSendIDF64 func(objc.ID, objc.SEL, float64) objc.ID
	// msgSendRect sends a message returning an NSRect.
	msgSendRect func(objc.ID, objc.SEL) nsRect
)

func init() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(fmt.Errorf("platform: load libobjc: %w", err))
	}
	sym := func(name string) uintptr {
		p, err := purego.Dlsym(lib, name)
		if err != nil {
			panic(fmt.Errorf("platform: %s: %w", name, err))
		}
		return p
	}
	msgSendAddr = sym("objc_msgSend")
	poolPushAddr = sym("objc_autoreleasePoolPush")
	poolPopAddr = sym("objc_autoreleasePoolPop")
	purego.RegisterFunc(&msgSendF32, msgSendAddr)
	purego.RegisterFunc(&msgSendF64, msgSendAddr)
	purego.RegisterFunc(&msgSendPoint, msgSendAddr)
	purego.RegisterFunc(&msgSendPointID, msgSendAddr)
	purego.RegisterFunc(&msgSendIDPoint, msgSendAddr)
	purego.RegisterFunc(&msgSendIDF64, msgSendAddr)
	rect := msgSendAddr
	if runtime.GOARCH == "amd64" {
		rect = sym("objc_msgSend_stret")
	}
	purego.RegisterFunc(&msgSendRect, rect)
}

// callArgs is the argument slice call hands SyscallN. SyscallN marks its
// variadic arguments as escaping, so a slice built at the call site is a
// heap allocation per send; this one already lives on the heap. callBusy
// guards it: a call that finds it taken, from another goroutine or from
// an Objective-C callback made during a send, builds its own slice.
// SyscallN copies the arguments before it calls into C, so the guard only
// has to cover the copy.
var (
	callArgs [6]uintptr
	callBusy atomic.Bool
)

// call runs the C function fn with its first n of up to six integer or
// pointer arguments and returns its integer result.
func call(fn uintptr, n int, a0, a1, a2, a3, a4, a5 uintptr) uintptr {
	if !callBusy.CompareAndSwap(false, true) {
		args := []uintptr{a0, a1, a2, a3, a4, a5}
		r, _, _ := purego.SyscallN(fn, args[:n]...)
		return r
	}
	callArgs = [6]uintptr{a0, a1, a2, a3, a4, a5}
	r, _, _ := purego.SyscallN(fn, callArgs[:n]...)
	callBusy.Store(false)
	return r
}

// sendID sends a message with no arguments that returns an object or an
// integer.
func sendID(id objc.ID, sel objc.SEL) objc.ID {
	return objc.ID(call(msgSendAddr, 2, uintptr(id), uintptr(sel), 0, 0, 0, 0))
}

// sendID1 sends a message with one integer or object argument.
func sendID1(id objc.ID, sel objc.SEL, a uintptr) objc.ID {
	return objc.ID(call(msgSendAddr, 3, uintptr(id), uintptr(sel), a, 0, 0, 0))
}

// sendUint sends a message with no arguments that returns an integer.
func sendUint(id objc.ID, sel objc.SEL) uint {
	return uint(call(msgSendAddr, 2, uintptr(id), uintptr(sel), 0, 0, 0, 0))
}

// sendBool sends a message with no arguments that returns a BOOL, which
// fills only the low byte of the result register.
func sendBool(id objc.ID, sel objc.SEL) bool {
	return byte(call(msgSendAddr, 2, uintptr(id), uintptr(sel), 0, 0, 0, 0)) != 0
}

// sendBool1 sends a message with one integer or object argument that
// returns a BOOL.
func sendBool1(id objc.ID, sel objc.SEL, a uintptr) bool {
	return byte(call(msgSendAddr, 3, uintptr(id), uintptr(sel), a, 0, 0, 0)) != 0
}

// poolPush opens an autorelease pool on the calling thread; poolPop with
// its result releases everything autoreleased since. A pool belongs to
// one thread, so poolPush locks the goroutine to its thread until
// poolPop. On the main goroutine, which the package locks already, that
// only counts the nesting.
func poolPush() uintptr {
	runtime.LockOSThread()
	return call(poolPushAddr, 0, 0, 0, 0, 0, 0, 0)
}

// poolPop closes the pool poolPush opened.
func poolPop(pool uintptr) {
	call(poolPopAddr, 1, pool, 0, 0, 0, 0, 0)
	runtime.UnlockOSThread()
}

// Selectors the per-frame paths and the window controls send, resolved
// once rather than at each send.
var (
	selContentView                 = objc.RegisterName("contentView")
	selHitTest                     = objc.RegisterName("hitTest:")
	selConvertPointFromView        = objc.RegisterName("convertPoint:fromView:")
	selConvertPointToView          = objc.RegisterName("convertPoint:toView:")
	selConvertPointToScreen        = objc.RegisterName("convertPointToScreen:")
	selSuperview                   = objc.RegisterName("superview")
	selFirstResponder              = objc.RegisterName("firstResponder")
	selIsKeyWindow                 = objc.RegisterName("isKeyWindow")
	selIsMiniaturized              = objc.RegisterName("isMiniaturized")
	selIsHiddenOrHasHiddenAncestor = objc.RegisterName("isHiddenOrHasHiddenAncestor")
	selIsKindOfClass               = objc.RegisterName("isKindOfClass:")
	selIsFlipped                   = objc.RegisterName("isFlipped")
	selRetain                      = objc.RegisterName("retain")
	selSetAutoresizingMask         = objc.RegisterName("setAutoresizingMask:")
	selAddSubview                  = objc.RegisterName("addSubview:")
	selRemoveFromSuperview         = objc.RegisterName("removeFromSuperview")
	selSetFrame                    = objc.RegisterName("setFrame:")
	selSetContentSize              = objc.RegisterName("setContentSize:")
	selSetHidden                   = objc.RegisterName("setHidden:")
	selOrderFront                  = objc.RegisterName("orderFront:")
	selOrderOut                    = objc.RegisterName("orderOut:")
	selScreens                     = objc.RegisterName("screens")
	selLocalizedName               = objc.RegisterName("localizedName")
	selDeviceDescription           = objc.RegisterName("deviceDescription")
	selObjectForKey                = objc.RegisterName("objectForKey:")
	selUnsignedIntValue            = objc.RegisterName("unsignedIntValue")
	selDateSinceNow                = objc.RegisterName("dateWithTimeIntervalSinceNow:")
)
