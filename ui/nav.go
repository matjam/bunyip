package ui

import "github.com/matjam/bunyip/input"

// navigate moves keyboard and gamepad focus between interactive widgets.
// Tab and Shift-Tab step through widgets, treating a container's items as
// one stop; the d-pad's up and down step through everything in order.
// Within a container the arrows, Home, End, PageUp and PageDown move
// between its items and the d-pad's left and right do the same.
func (c *Context) navigate() {
	c.navTextFocus = 0
	c.navMoved = c.navMovedNext
	c.navMovedNext = false
	if c.in == nil || len(c.focusables) == 0 {
		return
	}
	in := c.in
	keys := c.focus == 0 // no text field has the keyboard
	cmd := in.Mods()&(input.ModControl|input.ModSuper) != 0
	next, prev := false, false // Tab: the next widget or container
	step := 0                  // d-pad: the next widget in order
	within := 0                // arrows: the next item of the container
	home, end, page := false, false, 0
	activate := false
	if in.KeyPressed(input.KeyTab) {
		if in.Mods()&input.ModShift != 0 {
			prev = true
		} else {
			next = true
		}
	}
	idx := -1
	for i, f := range c.focusables {
		if f.id == c.navFocus {
			idx = i
		}
	}
	var cur focusable
	if idx >= 0 {
		cur = c.focusables[idx]
	}
	if pad := in.Gamepad(0); pad.Connected {
		if pad.Pressed(input.ButtonDpadDown) {
			step = 1
		}
		if pad.Pressed(input.ButtonDpadUp) {
			step = -1
		}
		if cur.group != 0 && cur.keys&navLeftRight != 0 {
			if pad.Pressed(input.ButtonDpadLeft) {
				within = -1
			}
			if pad.Pressed(input.ButtonDpadRight) {
				within = 1
			}
		}
		activate = pad.Pressed(input.ButtonA)
	}
	if keys {
		activate = activate || in.KeyPressed(input.KeyEnter) || in.KeyPressed(input.KeySpace)
		if cur.group != 0 && !cmd {
			if cur.keys&navUpDown != 0 {
				if in.KeyPressed(input.KeyUp) {
					within = -1
				}
				if in.KeyPressed(input.KeyDown) {
					within = 1
				}
			}
			if cur.keys&navLeftRight != 0 {
				if in.KeyPressed(input.KeyLeft) {
					within = -1
				}
				if in.KeyPressed(input.KeyRight) {
					within = 1
				}
			}
			home = in.KeyPressed(input.KeyHome)
			end = in.KeyPressed(input.KeyEnd)
			if in.KeyPressed(input.KeyPageUp) {
				page = -1
			}
			if in.KeyPressed(input.KeyPageDown) {
				page = 1
			}
		}
	}
	n := len(c.focusables)
	to := idx
	switch {
	case next:
		to = c.leave(idx, 1)
	case prev:
		to = c.leave(idx, -1)
	case step != 0 && idx < 0:
		to = 0
		if step < 0 {
			to = n - 1
		}
	case step != 0:
		to = (idx + step + n) % n
	case within != 0:
		to = c.withinGroup(idx, idx+within)
	case home:
		to = c.groupEnd(idx, -1)
	case end:
		to = c.groupEnd(idx, 1)
	case page != 0:
		to = c.withinGroup(idx, idx+page*max(cur.page, 1))
	}
	if to >= 0 && to != idx {
		f := c.focusables[to]
		c.navFocus = f.id
		// Release the old editor before any widget processes this frame's
		// input. The destination claims text focus only if it is an editor
		// submitted this frame and modal ownership permits it.
		c.focus = 0
		c.navTextFocus = f.id
		c.navMoved = true
		if f.group != 0 {
			c.groupFocus[f.group] = f.id
		}
	}
	c.activate = activate && c.navFocus != 0
}

// leave finds the next stop from idx in direction dir that is outside the
// current container, wrapping around, and enters it.
func (c *Context) leave(idx, dir int) int {
	n := len(c.focusables)
	if idx < 0 {
		if dir > 0 {
			return c.enter(0)
		}
		return c.enter(n - 1)
	}
	g := c.focusables[idx].group
	for k := 1; k <= n; k++ {
		i := (idx + dir*k + n) % n
		if g == 0 || c.focusables[i].group != g {
			return c.enter(i)
		}
	}
	return idx
}

// enter lands on stop i, or on the remembered item of the container it
// belongs to (its first item when none is remembered).
func (c *Context) enter(i int) int {
	g := c.focusables[i].group
	if g == 0 {
		return i
	}
	if want, ok := c.groupFocus[g]; ok {
		for j, f := range c.focusables {
			if f.group == g && f.id == want {
				return j
			}
		}
	}
	for j := i; j >= 0 && c.focusables[j].group == g; j-- {
		i = j
	}
	return i
}

// withinGroup clamps a move from idx to the bounds of its container.
func (c *Context) withinGroup(idx, to int) int {
	g := c.focusables[idx].group
	to = max(0, min(to, len(c.focusables)-1))
	dir := 1
	if to < idx {
		dir = -1
	}
	last := idx
	for i := idx; i != to+dir; i += dir {
		if c.focusables[i].group != g {
			break
		}
		last = i
	}
	return last
}

// groupEnd finds the first (dir -1) or last (dir 1) item of idx's container.
func (c *Context) groupEnd(idx, dir int) int {
	g := c.focusables[idx].group
	for i := idx + dir; i >= 0 && i < len(c.focusables) && c.focusables[i].group == g; i += dir {
		idx = i
	}
	return idx
}

// beginGroup starts submitting a container's items: they become one Tab
// stop that keys move within; page is how many rows PageUp and PageDown
// move.
func (c *Context) beginGroup(id widgetID, keys navKeys, page int) (saved navGroup) {
	saved = c.group
	c.group = navGroup{id: id, keys: keys, page: page}
	return saved
}

// endGroup restores the container in force before beginGroup.
func (c *Context) endGroup(saved navGroup) { c.group = saved }

// keyNav reports whether the keyboard is free for navigation: no text
// field has it.
func (c *Context) keyNav() bool { return c.in != nil && c.focus == 0 }

// stepKeys reports a step the focused widget id should take: -1 or 1
// from the left and right arrows, the minus and plus keys, or the d-pad's
// left and right, else 0.
func (c *Context) stepKeys(id widgetID) int {
	if c.navFocus != id || c.in == nil {
		return 0
	}
	d := 0
	in := c.in
	if c.keyNav() {
		if in.KeyPressed(input.KeyLeft) || in.KeyPressed(input.KeyMinus) || in.KeyPressed(input.KeyKeypadSubtract) {
			d = -1
		}
		if in.KeyPressed(input.KeyRight) || in.KeyPressed(input.KeyEqual) || in.KeyPressed(input.KeyKeypadAdd) {
			d = 1
		}
	}
	if pad := in.Gamepad(0); pad.Connected {
		if pad.Pressed(input.ButtonDpadLeft) {
			d = -1
		}
		if pad.Pressed(input.ButtonDpadRight) {
			d = 1
		}
	}
	return d
}

// lastFocusable finds id among the previous frame's stops.
func (c *Context) lastFocusable(id widgetID) (focusable, bool) {
	for _, f := range c.lastFocusables {
		if f.id == id {
			return f, true
		}
	}
	return focusable{}, false
}

// thisFocusable finds id among this frame's stops so far.
func (c *Context) thisFocusable(id widgetID) (focusable, bool) {
	for _, f := range c.focusables {
		if f.id == id {
			return f, true
		}
	}
	return focusable{}, false
}

// scrollDelta is how far a scroll offset must grow to bring r inside
// inner: negative when r is above it, zero when it is in view.
func scrollDelta(r, inner Rect) float32 {
	if r.Y < inner.Y {
		return r.Y - inner.Y
	}
	if r.Y+r.H > inner.Y+inner.H {
		return r.Y + r.H - (inner.Y + inner.H)
	}
	return 0
}
