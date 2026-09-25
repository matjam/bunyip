package ui

import (
	"fmt"
	"testing"

	"github.com/matjam/bunyip/input"
)

// tableRig is a long table in a short ScrollArea, recording which rows
// had their cells built in the last frame.
type tableRig struct {
	c       *Context
	in      *feeder
	rows    int
	built   map[int]bool
	clicked int
	cell    func(row, col int) // extra cell content; nil for a plain cell
}

func newTableRig(t *testing.T, rows int) *tableRig {
	return &tableRig{c: newContext(t), in: newFeeder(), rows: rows, clicked: -1}
}

func (r *tableRig) body() {
	c := r.c
	r.built = map[int]bool{}
	pitch := c.Theme.RowHeight + c.Theme.Spacing
	c.Panel("", Rect{X: 0, Y: 0, W: 300, H: 240}, func() {
		c.ScrollArea("rows", Rect{X: 4, Y: 4, W: 290, H: 150}, float32(r.rows+1)*pitch+c.Theme.Padding, func() {
			if row := c.Table([]string{"a", "b"}, nil, r.rows, func(row, col int) {
				r.built[row] = true
				if r.cell != nil {
					r.cell(row, col)
					return
				}
				c.Cell(fmt.Sprint(row, ",", col))
			}); row >= 0 {
				r.clicked = row
			}
		})
	})
}

func (r *tableRig) frame(t *testing.T) { t.Helper(); run(t, r.c, r.in, r.body) }

// rowNodes returns the accessibility rows of the last frame by number.
func (r *tableRig) rowNodes() map[string]AccessibleNode {
	out := map[string]AccessibleNode{}
	for _, n := range r.c.Accessible() {
		if n.Role == "row" {
			out[n.Label] = n
		}
	}
	return out
}

// focusedRow is the row number the navigation focus is on, or -1.
func (r *tableRig) focusedRow() int {
	for _, n := range r.c.Accessible() {
		if n.Role == "row" && n.Focused {
			var i int
			fmt.Sscan(n.Label, &i)
			return i
		}
	}
	return -1
}

// A long table builds only the rows in view, while every row keeps its
// place and its accessibility entry.
func TestTableBuildsOnlyVisibleRows(t *testing.T) {
	r := newTableRig(t, 500)
	r.frame(t)
	r.frame(t)
	if n := len(r.built); n == 0 || n > 12 {
		t.Fatalf("%d rows built for a view of about five", n)
	}
	nodes := r.rowNodes()
	if len(nodes) != r.rows {
		t.Fatalf("%d row entries, want %d", len(nodes), r.rows)
	}
	pitch := r.c.Theme.RowHeight + r.c.Theme.Spacing
	for i := 1; i < r.rows; i++ {
		prev, cur := nodes[fmt.Sprint(i-1)], nodes[fmt.Sprint(i)]
		if d := cur.Rect.Y - prev.Rect.Y; d < pitch-0.01 || d > pitch+0.01 {
			t.Fatalf("row %d is %v below row %d, want %v", i, d, i-1, pitch)
		}
	}
}

// The arrows, PageDown, End and Home move through rows that were out of
// view, each is scrolled into view and built, and Enter activates it.
func TestTableKeyboardAcrossTheFold(t *testing.T) {
	r := newTableRig(t, 300)
	r.frame(t)
	press(r.in, input.KeyTab, 0)
	r.frame(t)
	if got := r.focusedRow(); got != 0 {
		t.Fatalf("Tab focused row %d, want 0", got)
	}
	want := 0
	step := func(k input.Key, to int) {
		t.Helper()
		press(r.in, k, 0)
		r.frame(t)
		r.frame(t) // the scroll to the moved focus lands a frame later
		want = to
		if got := r.focusedRow(); got != want {
			t.Fatalf("%v focused row %d, want %d", k, got, want)
		}
		if !r.built[want] {
			t.Fatalf("row %d has the focus but was not scrolled into view and built", want)
		}
	}
	for i := 1; i <= 12; i++ {
		step(input.KeyDown, i)
	}
	step(input.KeyPageDown, 22)
	press(r.in, input.KeyEnter, 0)
	r.frame(t)
	if r.clicked != 22 {
		t.Fatalf("Enter activated row %d, want 22", r.clicked)
	}
	step(input.KeyEnd, 299)
	step(input.KeyUp, 298)
	step(input.KeyHome, 0)
}

// Scrolling a focused row out of view with the wheel keeps its focus,
// and Enter still activates it.
func TestTableFocusedRowScrolledAway(t *testing.T) {
	r := newTableRig(t, 300)
	r.frame(t)
	press(r.in, input.KeyTab, 0)
	r.frame(t)
	press(r.in, input.KeyDown, 0)
	r.frame(t)
	r.in.Input.FeedMouseMove(100, 60)
	r.frame(t)
	for range 10 {
		r.in.Input.FeedScroll(0, -5)
		r.frame(t)
	}
	if r.built[1] {
		t.Fatal("the scroll did not move row 1 out of view")
	}
	if got := r.focusedRow(); got != 1 {
		t.Fatalf("focus moved to row %d after scrolling", got)
	}
	press(r.in, input.KeyEnter, 0)
	r.frame(t)
	if r.clicked != 1 {
		t.Fatalf("Enter activated row %d, want the focused row 1", r.clicked)
	}
}

// A text field in a row keeps the keyboard while its row is scrolled out
// of view, because the row holding the focus is always built.
func TestTableTextFieldScrolledAway(t *testing.T) {
	r := newTableRig(t, 200)
	values := make([]string, r.rows)
	r.cell = func(row, col int) {
		if col == 1 {
			r.c.TextField("v", &values[row])
			return
		}
		r.c.Cell(fmt.Sprint(row))
	}
	r.frame(t)
	var field Rect
	for _, n := range r.c.Accessible() {
		if n.Role == "textfield" {
			field = n.Rect // the first row's field
			break
		}
	}
	if field.W == 0 {
		t.Fatal("no text field in the first row")
	}
	x, y := field.X+field.W/2, field.Y+field.H/2
	r.in.Input.FeedMouseMove(x, y)
	r.frame(t)
	r.in.FeedMouseButton(input.MouseLeft, true, x, y)
	r.frame(t)
	r.in.FeedMouseButton(input.MouseLeft, false, x, y)
	r.frame(t)
	if !r.c.WantsKeyboard() {
		t.Fatal("clicking the field did not give it the keyboard")
	}
	for range 10 {
		r.in.Input.FeedScroll(0, -5)
		r.frame(t)
	}
	if r.built[1] || !r.built[0] {
		t.Fatalf("after the scroll row 1 built %v and the focused row 0 built %v", r.built[1], r.built[0])
	}
	if !r.c.WantsKeyboard() {
		t.Fatal("scrolling the field's row out of view took the keyboard away")
	}
	r.in.Input.FeedChar('z')
	r.frame(t)
	if values[0] != "z" {
		t.Fatalf("typing after the scroll edited %q, want row 0 to hold \"z\"", values[0])
	}
}

// Rows keep the heights they were built with while they are skipped, so
// a table of uneven rows lays out the same scrolled as unscrolled.
func TestTableSkippedRowsKeepTheirHeights(t *testing.T) {
	r := newTableRig(t, 120)
	r.cell = func(row, col int) {
		if row%7 == 3 && col == 0 {
			r.c.next(60) // a cell twice the height of a row
			return
		}
		r.c.Cell(fmt.Sprint(row))
	}
	// Build every row once through a view tall enough to hold them all.
	full := map[string]AccessibleNode{}
	c := r.c
	pitch := c.Theme.RowHeight + c.Theme.Spacing
	run(t, c, r.in, func() {
		c.Panel("", Rect{X: 0, Y: 0, W: 300, H: 240}, func() {
			c.ScrollArea("rows", Rect{X: 4, Y: 4, W: 290, H: 100000}, float32(r.rows+1)*pitch*3, func() {
				c.Table([]string{"a", "b"}, nil, r.rows, r.cell)
			})
		})
	})
	for _, n := range c.Accessible() {
		if n.Role == "row" {
			full[n.Label] = n
		}
	}
	if len(full) != r.rows {
		t.Fatalf("%d rows in the tall view, want %d", len(full), r.rows)
	}
	r.frame(t)
	r.frame(t)
	if len(r.built) > 12 {
		t.Fatalf("%d rows built in the short view", len(r.built))
	}
	skipped := r.rowNodes()
	for label, n := range full {
		got, want := skipped[label].Rect.Y-skipped["0"].Rect.Y, n.Rect.Y-full["0"].Rect.Y
		if got != want {
			t.Fatalf("row %s sits %v below row 0 when skipped, %v when built", label, got, want)
		}
	}
}
