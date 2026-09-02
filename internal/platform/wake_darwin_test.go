package platform

import (
	"fmt"
	"time"
)

// runWake checks that Wake from another goroutine returns a blocking
// Poll with an EventWake. It runs from TestMain like the text-input test.
func runWake(app *App, w *Window) error {
	go func() {
		time.Sleep(50 * time.Millisecond)
		app.Wake()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range app.Poll(true) {
			if e.Kind == EventWake {
				return nil
			}
		}
	}
	return fmt.Errorf("Poll(true) never delivered EventWake")
}
