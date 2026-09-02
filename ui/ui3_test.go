package ui

import (
	"slices"
	"testing"

	"github.com/matjam/bunyip/input"
)

// focusedIs reports whether the navigation focus is on this frame's
// stop number i.
func focusedIs(c *Context, i int) bool {
	return i >= 0 && i < len(c.focusables) && c.focusables[i].id == c.navFocus
}

// nodeRect finds the first accessibility node with a role and label.
func nodeRect(t *testing.T, nodes []AccessibleNode, role, label string) Rect {
	t.Helper()
	for _, n := range nodes {
		if n.Role == role && n.Label == label {
			return n.Rect
		}
	}
	t.Fatalf("no %s %q in %v", role, label, nodes)
	return Rect{}
}

func TestListNavigation(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	sel := -1
	items := []string{"a", "b", "c", "d", "e"}
	var nodes []AccessibleNode
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.Button("Top")
			c.ListBox("items", 70, items, &sel)
			c.Button("Bottom")
		})
		nodes = c.Accessible()
	}
	run(t, c, in, body)
	// Stops: Top, five rows, Bottom. Tab enters the list as one stop.
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	if !focusedIs(c, 0) {
		t.Fatal("Tab did not focus the first button")
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	if !focusedIs(c, 1) {
		t.Fatal("Tab did not enter the list at its first row")
	}
	for range 4 {
		press(in, input.KeyDown, 0)
		run(t, c, in, body)
	}
	if !focusedIs(c, 5) {
		t.Fatal("Down did not reach the last row")
	}
	list := nodeRect(t, nodes, "listbox", "items")
	if row := c.focusables[5].rect; row.Y+row.H > list.Y+list.H+1 || row.Y < list.Y-1 {
		t.Errorf("the focused row %v was not scrolled into the list %v", row, list)
	}
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	if !focusedIs(c, 5) {
		t.Error("Down left the list at its end")
	}
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if sel != 4 {
		t.Errorf("Enter selected %d, want 4", sel)
	}
	press(in, input.KeyHome, 0)
	run(t, c, in, body)
	if !focusedIs(c, 1) {
		t.Error("Home did not go to the first row")
	}
	if row := c.focusables[1].rect; row.Y < list.Y-1 {
		t.Errorf("the first row %v was not scrolled back into %v", row, list)
	}
	press(in, input.KeyEnd, 0)
	run(t, c, in, body)
	if !focusedIs(c, 5) {
		t.Error("End did not go to the last row")
	}
	press(in, input.KeyPageUp, 0)
	run(t, c, in, body)
	if focusedIs(c, 5) || focusedIs(c, 0) || focusedIs(c, 6) {
		t.Error("PageUp did not move up within the list")
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	if !focusedIs(c, 6) {
		t.Error("Tab did not leave the list for the next button")
	}
	press(in, input.KeyTab, input.ModShift)
	run(t, c, in, body)
	if !focusedIs(c, 4) {
		t.Error("Shift-Tab did not return into the list at the remembered row")
	}
	press(in, input.KeyEscape, 0) // clears the modifier state
	run(t, c, in, body)
}

func TestTabsRadioTableKeys(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	tab, radio, clickedRow := 0, 0, -1
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.Tabs([]string{"A", "B", "C"}, &tab)
			c.RadioGroup(&radio, []string{"x", "y"})
			if row := c.Table([]string{"c1", "c2"}, nil, 3, func(row, col int) { c.Cell("v") }); row >= 0 {
				clickedRow = row
			}
		})
	}
	run(t, c, in, body)
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0)
	run(t, c, in, body)
	if !focusedIs(c, 1) {
		t.Fatal("Right did not move to the second tab")
	}
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if tab != 1 {
		t.Errorf("Enter selected tab %d, want 1", tab)
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	if !focusedIs(c, 3) {
		t.Fatal("Tab did not leave the tabs for the radio group")
	}
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if radio != 1 {
		t.Errorf("Enter chose radio %d, want 1", radio)
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	if !focusedIs(c, 5) {
		t.Fatal("Tab did not reach the table's first row")
	}
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0) // rows only move up and down
	run(t, c, in, body)
	if !focusedIs(c, 6) {
		t.Fatal("Down did not move to the second row")
	}
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if clickedRow != 1 {
		t.Errorf("Enter activated row %d, want 1", clickedRow)
	}
	roles := map[string]int{}
	for _, n := range c.Accessible() {
		roles[n.Role]++
	}
	if roles["row"] != 3 {
		t.Errorf("table rows in the accessibility list: %d", roles["row"])
	}
}

func TestSliderSpinnerKeys(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	v, n, iv := float32(0.5), 5, 3
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.Slider("v", &v, 0, 1)
			c.Spinner("n", &n, 0, 10, 1)
			c.IntSlider("i", &iv, 0, 9)
		})
	}
	run(t, c, in, body)
	if len(c.focusables) != 3 {
		t.Fatalf("%d stops; the spinner's buttons should not be stops", len(c.focusables))
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0)
	run(t, c, in, body)
	if v < 0.549 || v > 0.551 {
		t.Errorf("Right stepped the slider to %v, want 0.55", v)
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0)
	run(t, c, in, body)
	press(in, input.KeyLeft, 0)
	run(t, c, in, body)
	if n != 6 {
		t.Errorf("spinner = %d, want 6", n)
	}
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyLeft, 0)
	run(t, c, in, body)
	if iv != 2 {
		t.Errorf("int slider = %d, want 2", iv)
	}
	// The d-pad steps too.
	var buttons [input.GamepadButtonCount]bool
	buttons[input.ButtonDpadRight] = true
	in.FeedGamepad(0, true, "pad", buttons, [input.GamepadAxisCount]float32{})
	run(t, c, in, body)
	in.FeedGamepad(0, true, "pad", [input.GamepadButtonCount]bool{}, [input.GamepadAxisCount]float32{})
	run(t, c, in, body)
	if iv != 3 {
		t.Errorf("int slider after d-pad right = %d, want 3", iv)
	}
}

func TestDropdownKeys(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	sel := 0
	changes := 0
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			if c.Dropdown("d", &sel, []string{"a", "b", "c"}) {
				changes++
			}
			c.Button("After")
		})
	}
	run(t, c, in, body)
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if c.open == 0 {
		t.Fatal("Enter did not open the dropdown")
	}
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if sel != 1 || changes != 1 || c.open != 0 {
		t.Errorf("after Down and Enter: sel %d, changes %d, open %v", sel, changes, c.open != 0)
	}
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	press(in, input.KeyEscape, 0)
	run(t, c, in, body)
	if sel != 1 || c.open != 0 {
		t.Errorf("Escape: sel %d, open %v", sel, c.open != 0)
	}
	if !focusedIs(c, 0) {
		t.Error("focus did not return to the dropdown after Escape")
	}
}

func TestTreeKeys(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	var flag bool
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.Tree("node", func() { c.Checkbox("flag", &flag) })
		})
	}
	run(t, c, in, body)
	press(in, input.KeyTab, 0)
	run(t, c, in, body)
	press(in, input.KeyRight, 0)
	run(t, c, in, body)
	run(t, c, in, body)
	if len(c.focusables) != 2 {
		t.Fatal("Right did not open the node")
	}
	press(in, input.KeyDown, 0)
	run(t, c, in, body)
	if !focusedIs(c, 1) {
		t.Error("Down did not move into the open node")
	}
	press(in, input.KeyEnter, 0)
	run(t, c, in, body)
	if !flag {
		t.Error("Enter did not toggle the checkbox inside the node")
	}
	press(in, input.KeyUp, 0)
	run(t, c, in, body)
	press(in, input.KeyLeft, 0)
	run(t, c, in, body)
	run(t, c, in, body)
	if len(c.focusables) != 1 {
		t.Error("Left did not close the node")
	}
}

func TestGamepadNavigation(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	clicks := map[string]int{}
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			for _, l := range []string{"A", "B", "C"} {
				if c.Button(l) {
					clicks[l]++
				}
			}
		})
	}
	pad := func(b input.GamepadButton) {
		var buttons [input.GamepadButtonCount]bool
		buttons[b] = true
		in.FeedGamepad(0, true, "pad", buttons, [input.GamepadAxisCount]float32{})
		run(t, c, in, body)
		in.FeedGamepad(0, true, "pad", [input.GamepadButtonCount]bool{}, [input.GamepadAxisCount]float32{})
		run(t, c, in, body)
	}
	run(t, c, in, body)
	pad(input.ButtonDpadDown)
	pad(input.ButtonDpadDown)
	if !focusedIs(c, 1) {
		t.Fatal("d-pad down did not reach the second button")
	}
	pad(input.ButtonA)
	if clicks["B"] != 1 {
		t.Errorf("A pressed %v", clicks)
	}
	pad(input.ButtonDpadUp)
	if !focusedIs(c, 0) {
		t.Error("d-pad up did not move back")
	}
}

func TestDragDrop(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	drops := 0
	var got any
	var nodes []AccessibleNode
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.DragSource("gem", "gem", func() { c.Label("Gem") })
			c.Space(40)
			c.Label("Bin")
			if p, ok := c.DropTarget("bin", func(p any) bool { _, ok := p.(string); return ok }); ok {
				drops++
				got = p
			}
		})
		nodes = c.Accessible()
	}
	run(t, c, in, body)
	run(t, c, in, body) // Accessible reports the frame before
	src := nodeRect(t, nodes, "draggable", "gem")
	dst := nodeRect(t, nodes, "droptarget", "bin")
	sx, sy := src.X+src.W/2, src.Y+src.H/2
	dx, dy := dst.X+dst.W/2, dst.Y+dst.H/2
	if src.H < 10 || dst.Y <= src.Y+src.H {
		t.Fatalf("unexpected layout: source %v target %v", src, dst)
	}
	in.FeedMouseMove(sx, sy)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, sx, sy)
	run(t, c, in, body)
	if _, ok := c.Dragging(); ok {
		t.Error("a press alone started a drag")
	}
	in.FeedMouseMove(dx, dy)
	run(t, c, in, body)
	if p, ok := c.Dragging(); !ok || p != "gem" {
		t.Fatalf("moving while pressed did not start the drag: %v %v", p, ok)
	}
	if !c.WantsMouse() {
		t.Error("WantsMouse false during a drag")
	}
	for _, n := range nodes {
		if n.Role == "droptarget" && !n.State {
			t.Error("the target is not marked ready while an accepted payload hovers")
		}
	}
	in.FeedMouseButton(input.MouseLeft, false, dx, dy)
	run(t, c, in, body)
	if drops != 1 || got != "gem" {
		t.Errorf("drops %d payload %v", drops, got)
	}
	if _, ok := c.Dragging(); ok {
		t.Error("the drag outlived its release")
	}
	// Escape cancels: the release drops nothing.
	in.FeedMouseMove(sx, sy)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, sx, sy)
	run(t, c, in, body)
	in.FeedMouseMove(dx, dy)
	run(t, c, in, body)
	if _, ok := c.Dragging(); !ok {
		t.Fatal("second drag did not start")
	}
	press(in, input.KeyEscape, 0)
	run(t, c, in, body)
	if _, ok := c.Dragging(); ok {
		t.Error("Escape did not cancel the drag")
	}
	in.FeedMouseButton(input.MouseLeft, false, dx, dy)
	run(t, c, in, body)
	if drops != 1 {
		t.Errorf("a cancelled drag dropped: %d", drops)
	}
	// A payload the target refuses is not dropped.
	in.FeedMouseMove(sx, sy)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, sx, sy)
	run(t, c, in, body)
	in.FeedMouseMove(dx, dy)
	body2 := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			c.DragSource("gem", 7, func() { c.Label("Gem") })
			c.Space(40)
			c.Label("Bin")
			if _, ok := c.DropTarget("bin", func(p any) bool { _, ok := p.(string); return ok }); ok {
				drops++
			}
		})
	}
	run(t, c, in, body2)
	in.FeedMouseButton(input.MouseLeft, false, dx, dy)
	run(t, c, in, body2)
	if drops != 1 {
		t.Errorf("a refused payload was dropped: %d", drops)
	}
}

func TestReorderableList(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	items := []string{"a", "b", "c", "d"}
	var nodes []AccessibleNode
	body := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 300, H: 220}, func() {
			if from, to, ok := c.ReorderableList("order", items, 160); ok {
				Move(items, from, to)
			}
		})
		nodes = c.Accessible()
	}
	run(t, c, in, body)
	run(t, c, in, body) // Accessible reports the frame before
	a := nodeRect(t, nodes, "listitem", "a")
	cc := nodeRect(t, nodes, "listitem", "c")
	pitch := c.Theme.RowHeight + c.Theme.Spacing
	ax, ay := a.X+a.W/2, a.Y+a.H/2
	in.FeedMouseMove(ax, ay)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, true, ax, ay)
	run(t, c, in, body)
	// Below the middle of c: the marker sits after c, so a lands at 2.
	ty := cc.Y + cc.H/2 + pitch/3
	in.FeedMouseMove(ax, ty)
	run(t, c, in, body)
	if c.reorder == nil {
		t.Fatal("dragging a row did not start a reorder")
	}
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, false, ax, ty)
	run(t, c, in, body)
	if want := []string{"b", "c", "a", "d"}; !slices.Equal(items, want) {
		t.Fatalf("after dragging a below c: %v, want %v", items, want)
	}
	// The dragged row keeps focus; Ctrl+Up moves it a step and focus
	// follows.
	run(t, c, in, body) // the new order is laid out
	if !focusedIs(c, 2) {
		t.Fatal("focus did not follow the dragged row")
	}
	press(in, input.KeyUp, input.ModControl)
	run(t, c, in, body)
	press(in, input.KeyEscape, 0) // clears the modifier state
	run(t, c, in, body)
	if want := []string{"b", "a", "c", "d"}; !slices.Equal(items, want) {
		t.Errorf("after Ctrl+Up: %v, want %v", items, want)
	}
	if !focusedIs(c, 1) {
		t.Error("focus did not follow the moved row")
	}
	press(in, input.KeyDown, 0) // a plain arrow moves focus, not the row
	run(t, c, in, body)
	if !focusedIs(c, 2) || items[1] != "a" {
		t.Error("Down without a modifier did not just move focus")
	}
	roles := map[string]int{}
	for _, n := range nodes {
		roles[n.Role]++
	}
	if roles["list"] != 1 || roles["listitem"] != 4 {
		t.Errorf("accessibility roles %v", roles)
	}
	// Move itself.
	s := []int{0, 1, 2, 3, 4}
	Move(s, 3, 1)
	if want := []int{0, 3, 1, 2, 4}; !slices.Equal(s, want) {
		t.Errorf("Move(3, 1) = %v, want %v", s, want)
	}
}
