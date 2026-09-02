package network

import "testing"

func TestInterest(t *testing.T) {
	in := Interest[int]{Radius: 10, Margin: 2}
	frame := func(positions map[int]float32) (entered, left []int) {
		in.Begin(0, 0)
		for id, x := range positions {
			in.Visit(id, x, 0)
		}
		return in.End()
	}
	entered, left := frame(map[int]float32{1: 5, 2: 9, 3: 11})
	if len(entered) != 2 || len(left) != 0 || !in.Contains(2) || in.Contains(3) {
		t.Fatalf("first frame: entered %v left %v", entered, left)
	}
	// Past the radius but inside the margin: still in.
	if entered, left = frame(map[int]float32{1: 5, 2: 11, 3: 11}); len(entered) != 0 || len(left) != 0 || !in.Contains(2) {
		t.Fatalf("edge: entered %v left %v", entered, left)
	}
	// Past the margin: out. Unvisited entities leave too.
	if _, left = frame(map[int]float32{2: 13}); len(left) != 2 || in.Len() != 0 {
		t.Fatalf("beyond margin: left %v, %d in", left, in.Len())
	}
	// Back inside the margin but outside the radius: does not re-enter.
	if in.Visit(2, 11, 0) {
		t.Fatal("re-entered inside the margin")
	}
	in.End()
	if in.Contains(2) {
		t.Fatal("re-entered inside the margin")
	}
}
