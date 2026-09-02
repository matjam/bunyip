package input_test

import (
	"fmt"

	"github.com/matjam/bunyip/input"
)

func ExampleState() {
	// The engine feeds the state from platform events; a test can script it.
	var s input.State
	s.FeedKey(input.KeySpace, true, false, 0)
	s.FeedMouseButton(input.MouseLeft, true, 10, 20)
	fmt.Println(s.KeyPressed(input.KeySpace), s.KeyDown(input.KeySpace), s.MousePressed(input.MouseLeft))

	// After an update the edges clear but held state stays.
	s.EndUpdate()
	fmt.Println(s.KeyPressed(input.KeySpace), s.KeyDown(input.KeySpace))
	// Output:
	// true true true
	// false true
}
