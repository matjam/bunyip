package save_test

import (
	"fmt"
	"os"

	"github.com/matjam/bunyip/save"
)

type Settings struct {
	Volume     float32
	Fullscreen bool
}

func Example() {
	// In a game: store, err := save.Open("my-game"), which picks the
	// platform's data directory. A temporary directory keeps this example
	// self-contained.
	dir, _ := os.MkdirTemp("", "save-example")
	defer os.RemoveAll(dir)
	store, err := save.OpenAt(dir)
	if err != nil {
		panic(err)
	}

	// Load settings over defaults: a missing file yields the defaults.
	settings, _ := store.Load("settings", Settings{Volume: 0.8})
	fmt.Println(settings.Volume, settings.Fullscreen)

	settings.Fullscreen = true
	store.Write("settings", settings)
	again, _ := store.Load("settings", Settings{Volume: 0.8})
	fmt.Println(again.Fullscreen)

	names, _ := store.List()
	fmt.Println(names)
	// Output:
	// 0.8 false
	// true
	// [settings]
}
