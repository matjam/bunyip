package tween_test

import (
	"fmt"

	"github.com/matjam/bunyip/tween"
)

func ExampleTween() {
	// Slide a value from 0 to 100 over one second with an ease-out curve.
	slide := tween.New(0, 100, 1, tween.OutQuad)
	for range 4 {
		fmt.Printf("%.0f ", slide.Update(0.25))
	}
	fmt.Println(slide.Done())
	// Output:
	// 44 75 94 100 true
}

func ExampleSequence() {
	// Fade in, hold, fade out: each step starts when the previous ends.
	fade := tween.NewSequence(
		tween.New(0, 1, 0.5, nil),
		tween.New(1, 1, 1, nil),
		tween.New(1, 0, 0.5, nil),
	)
	for !fade.Done() {
		fade.Update(0.5)
	}
	fmt.Println(fade.Update(0))
	// Output:
	// 0
}
