package console

import (
	"fmt"
	"slices"
	"testing"
)

// The output buffer keeps the newest lines, oldest first, however many
// times it has wrapped, and Clear empties it.
func TestOutputKeepsNewestLinesInOrder(t *testing.T) {
	c := New(Options{Lines: 5})
	for i := range 12 {
		c.Print(fmt.Sprint(i))
	}
	if got, want := c.Lines(), []string{"7", "8", "9", "10", "11"}; !slices.Equal(got, want) {
		t.Fatalf("after 12 lines kept %v, want %v", got, want)
	}
	c.Print("a\nb")
	if got, want := c.Lines(), []string{"9", "10", "11", "a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("after a two-line print kept %v, want %v", got, want)
	}
	c.Clear()
	if got := c.Lines(); len(got) != 0 {
		t.Fatalf("Clear left %v", got)
	}
	c.Print("x")
	c.Print("y")
	if got, want := c.Lines(), []string{"x", "y"}; !slices.Equal(got, want) {
		t.Fatalf("after Clear kept %v, want %v", got, want)
	}
}

// The open console draws the newest lines of a wrapped buffer, newest at
// the bottom, and the lines scrolled back to.
func TestDrawShowsNewestLinesOfAWrappedBuffer(t *testing.T) {
	r := newRig(t, Options{Lines: 50})
	for i := range 173 {
		r.con.Print(fmt.Sprint("line ", i))
	}
	r.con.SetOpen(true)
	r.drawN(t, 30) // let the drop-down finish opening
	if len(r.con.shown) == 0 || r.con.shown[0].text != "line 172" {
		t.Fatalf("drew %v first, want the newest line", r.con.shown)
	}
	for i, l := range r.con.shown {
		if want := fmt.Sprint("line ", 172-i); l.text != want {
			t.Fatalf("line %d drawn is %q, want %q", i, l.text, want)
		}
	}
	r.con.scroll = 10
	r.draw(t)
	if r.con.shown[0].text != "line 162" {
		t.Fatalf("scrolled back ten drew %q first, want line 162", r.con.shown[0].text)
	}
	r.con.scroll = 1000
	r.draw(t)
	if n := len(r.con.shown); n != 1 || r.con.shown[0].text != "line 123" {
		t.Fatalf("scrolled to the top drew %d lines from %q, want only the oldest kept, line 123", n, r.con.shown[0].text)
	}
}
