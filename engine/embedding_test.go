package engine

import (
	"errors"
	"testing"
)

func TestEmbeddingRejectsInvalidParentsBeforeStarting(t *testing.T) {
	for _, p := range []*NativeParent{{}, {Backend: NativeWin32}, {Backend: 99, Handle: 1}} {
		if err := Run(Config{Parent: p, Headless: true}, GameFuncs{}); err == nil {
			t.Fatal("invalid parent accepted")
		}
	}
	for _, backend := range []NativeBackend{NativeWin32, NativeCocoa, NativeX11} {
		if err := Run(Config{Parent: &NativeParent{Backend: backend, Handle: 1}, Headless: true}, GameFuncs{}); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("headless parent: %v", err)
		}
	}
	if err := (&Context{}).SetBounds(0, 0, 20, 20); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

type embeddedBoundsWindow struct {
	window
	bounds [4]int
}

func (w *embeddedBoundsWindow) SetBounds(x, y, width, height int) error {
	w.bounds = [4]int{x, y, width, height}
	return nil
}
func TestEmbeddedBoundsDispatch(t *testing.T) {
	w := &embeddedBoundsWindow{}
	c := &Context{win: w}
	if err := c.SetBounds(7, 9, 100, 60); err != nil || w.bounds != [4]int{7, 9, 100, 60} {
		t.Fatalf("bounds=%v error=%v", w.bounds, err)
	}
	if err := c.SetBounds(1, 2, 0, 5); err == nil || w.bounds != [4]int{7, 9, 100, 60} {
		t.Fatal("invalid bounds reached native window")
	}
}
