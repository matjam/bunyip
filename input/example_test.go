package input_test

import (
	"fmt"

	"github.com/matjam/bunyip/input"
)

func ExampleState() {
	// The engine fills the state from platform events and hands it to the
	// game as ctx.Input. Levels say what is held now, edges what changed
	// since the last update, so a press is seen exactly once.
	move := func(in *input.State) (dx float32, jump bool) {
		if in.KeyDown(input.KeyD) {
			dx++
		}
		if in.KeyDown(input.KeyA) {
			dx--
		}
		return dx, in.KeyPressed(input.KeySpace)
	}
	var in input.State // ctx.Input under engine.Run
	fmt.Println(move(&in))
	// Output:
	// 0 false
}
