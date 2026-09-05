package bunyip

// GameFuncs adapts callbacks to Game. Every callback is optional; a nil
// callback does nothing. Use it for small games and tools that do not need
// a type with methods. It does not implement Recoverer: recovery requires
// a game that explicitly rebuilds its resources.
type GameFuncs struct {
	InitFunc     func(*Context) error
	UpdateFunc   func(*Context) error
	DrawFunc     func(*Context) error
	ShutdownFunc func(*Context)
}

// Init calls InitFunc, when set.
func (g GameFuncs) Init(ctx *Context) error {
	if g.InitFunc != nil {
		return g.InitFunc(ctx)
	}
	return nil
}

// Update calls UpdateFunc, when set.
func (g GameFuncs) Update(ctx *Context) error {
	if g.UpdateFunc != nil {
		return g.UpdateFunc(ctx)
	}
	return nil
}

// Draw calls DrawFunc, when set.
func (g GameFuncs) Draw(ctx *Context) error {
	if g.DrawFunc != nil {
		return g.DrawFunc(ctx)
	}
	return nil
}

// Shutdown calls ShutdownFunc, when set and setup succeeded.
func (g GameFuncs) Shutdown(ctx *Context) {
	if g.ShutdownFunc != nil {
		g.ShutdownFunc(ctx)
	}
}

// Cleanup registers work to run when this context closes, after the
// game's optional Shutdown and before graphics, audio and the window
// close. Call it on the game goroutine, usually just after acquiring a
// resource. Callbacks run in reverse registration order, including when
// Init or Recover fails and before a device-loss rebuild. GPU resources
// already belong to Graphics and need no cleanup registration.
//
// A nil callback is ignored. A callback may register more cleanup work.
// If a callback panics, remaining callbacks still run and the panic
// continues, as with deferred functions.
func (c *Context) Cleanup(fn func()) {
	if fn != nil {
		c.cleanups = append(c.cleanups, fn)
	}
}

func (c *Context) cleanup() {
	// Continue draining during panic unwinding without intercepting the
	// panic. On the ordinary path the loop uses constant stack space.
	defer func() {
		if len(c.cleanups) > 0 {
			c.cleanup()
		}
	}()
	for len(c.cleanups) > 0 {
		i := len(c.cleanups) - 1
		fn := c.cleanups[i]
		c.cleanups[i] = nil
		c.cleanups = c.cleanups[:i]
		fn()
	}
}
