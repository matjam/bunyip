package ui

// AccessibleNode describes one widget of the last frame for assistive
// technology: what it is, what it says, where it is and its state. A
// game or platform layer can read the list with Accessible and hand it
// to a screen reader, log it, or drive the interface from it.
type AccessibleNode struct {
	// Role is one of button, checkbox, slider, textfield, textarea, tab,
	// tree, menu, menuitem, radio, spinner, listbox, colorpicker, image,
	// label, window, dropdown, progress, row (of a Table), list and
	// listitem (a ReorderableList and its rows), draggable and droptarget.
	Role    string
	Label   string
	Value   string // the current value where there is one
	Rect    Rect
	State   bool // checked, selected, open, dragged or ready for a drop, as the role implies
	Focused bool // has keyboard focus
}

// note records a widget for the accessibility list, at the rectangle the
// last interact call registered.
func (c *Context) note(role, label, value string, state bool) {
	c.noteAt(role, label, value, state, c.lastRect, c.lastID)
}

// noteAt records a widget at an explicit rectangle and identity.
func (c *Context) noteAt(role, label, value string, state bool, r Rect, id widgetID) {
	c.nodes = append(c.nodes, AccessibleNode{Role: role, Label: label, Value: value, Rect: r, State: state, Focused: c.navFocus == id || c.focus == id})
}

// Accessible returns the widgets of the last finished frame in reading
// order.
func (c *Context) Accessible() []AccessibleNode { return c.lastNodes }
