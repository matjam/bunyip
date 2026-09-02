package tracker_test

import (
	"os"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/audio/tracker"
)

// Play a module through the engine's mixer. Player is an audio.Stream,
// so it plays like any other music.
func ExamplePlayer() {
	data, err := os.ReadFile("song.xm")
	if err != nil {
		return
	}
	mod, err := tracker.Load(data) // MOD, S3M, XM or IT, told apart by content
	if err != nil {
		return
	}
	mixer := audio.NewMixer(48000)
	player := tracker.NewPlayer(mod, mixer.Rate())
	player.Loop = true
	mixer.PlayStream(player, audio.PlayOptions{Volume: 0.8})
}
