package rng_test

import (
	"fmt"

	"github.com/matjam/bunyip/rng"
)

func ExampleNew() {
	// The same seed gives the same sequence on every platform.
	r := rng.New(42)
	fmt.Println(r.Intn(100), r.Intn(100), r.Intn(100))
	fmt.Println(r.Roll(2, 6) >= 2)
	// Output:
	// 44 23 95
	// true
}

func ExampleRand_Pick() {
	r := rng.New(7)
	loot := []string{"sword", "shield", "potion"}
	fmt.Println(r.Pick(loot))
	r.Shuffle(loot)
	fmt.Println(len(loot))
	// Output:
	// potion
	// 3
}

func ExampleRand_Fork() {
	// Forked streams let one system roll dice without disturbing another.
	world := rng.New(1)
	combat := world.Fork()
	a := combat.Intn(1000)
	combat2 := rng.New(1).Fork()
	fmt.Println(a == combat2.Intn(1000))
	// Output:
	// true
}
