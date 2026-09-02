package audio_test

import (
	"fmt"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/lin"
)

func ExampleMixer() {
	// The engine creates the mixer and pulls Mix from the output device;
	// here we pull it by hand.
	m := audio.NewMixer(48000)
	beep, _ := m.NewSound(audio.Sine(440, 0.1, 48000))
	v := m.Play(beep, audio.PlayOptions{Volume: 0.5, Pan: -0.5})
	buf := make([]float32, 512*2)
	m.Mix(buf)
	fmt.Println(m.Playing(), v.Playing())
	for v.Playing() {
		m.Mix(buf)
	}
	fmt.Println(m.Playing())
	// Output:
	// 1 true
	// 0
}

func ExampleMixer_SetListener2D() {
	// Positional voices pan and fade by where they are relative to the
	// listener; in a 2D game put the listener at the camera each frame.
	m := audio.NewMixer(48000)
	m.SetListener2D(400, 300)
	tone, _ := m.NewSound(audio.Sine(330, 1, 48000))
	v := m.Play(tone, audio.PlayOptions{Loop: true, Positional: true, Position: lin.V3(600, 300, 0), MaxDistance: 800})
	v.SetPosition(lin.V3(200, 300, 0)) // now to the left
	fmt.Println(v.Playing())
	// Output:
	// true
}

func ExampleMixer_SetReverb() {
	m := audio.NewMixer(48000)
	m.SetReverb(audio.ReverbSettings{RoomSize: 0.7, Wet: 0.3})
	hit, _ := m.NewSound(audio.Sine(200, 0.05, 48000))
	m.Play(hit, audio.PlayOptions{Reverb: 1, LowPass: 2000, Pitch: 0.9})
	fmt.Println(m.Playing())
	// Output:
	// 1
}
