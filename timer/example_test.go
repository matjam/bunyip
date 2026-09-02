package timer_test

import (
	"fmt"

	"github.com/matjam/bunyip/timer"
)

func ExampleScheduler() {
	var s timer.Scheduler
	s.After(1, func() { fmt.Println("door opens") })
	tick := s.Every(0.5, func() { fmt.Println("tick") })
	// Update from the game loop with the frame's delta; here two big
	// steps. Timers fire in time order, and at the same moment in the
	// order they were scheduled.
	s.Update(0.6)
	s.Update(0.6)
	s.Cancel(tick)
	s.Update(5)
	// Output:
	// tick
	// door opens
	// tick
}

func ExampleCountdown() {
	var c timer.Countdown
	c.Start(1)
	for i := 1; ; i++ {
		if c.Update(0.4) {
			fmt.Println("ran out on update", i)
			break
		}
	}
	// Output:
	// ran out on update 3
}
