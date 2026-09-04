---
title: Audio
example: audio
summary: positional voices orbiting a listener with Doppler, occlusion and a binaural head model, reverb and low-pass on sliders, fades, pitch, voice stealing, a synthesised music stream and microphone capture
---

This program is a tour of the [audio](../pkg/audio.html) mixer. Three
sine sources orbit a listener in the middle of the window, panning and
fading with distance and shifting in pitch from their own velocity. A
looping pad runs through a low-pass filter and a shared reverb, both on
sliders. Buttons fade the pad, pause the music, play a click at a chosen
pitch, and fire forty voices at once into a deliberately small voice cap
so the mixer has to steal. A checkbox swaps the pan law for the binaural
head model, and `-mic` records from the default microphone and draws a
level meter.

Music is a stream rather than a sound: with `-music` it decodes an Ogg,
MP3 or WAV file from disk as it plays, and without one it plays an
arpeggio this program synthesises a buffer at a time. That is the same
interface a game uses for procedural music or its own decoder.

Sounds are values loaded once and played many times; a `Voice` is one
playing instance and is what the fades, filters and positions act on.
[The audio guide](../guides/audio.html) covers the model, buses and the
tracker player. Positions here are in 2D view units, because the program
calls `SetListener2D` and gives the sources positions on the screen; the
mixer works in 3D and treats Z as zero.

Run it:

```bash
go run ./examples/audio -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`, `-music file.ogg` to
stream a file instead of the synthesised arpeggio, `-zone` to put a
reverb zone over the left half of the window and move the listener with
the mouse, and `-mic` to record from the microphone.

## Package and state

`source` is one orbiting voice with the numbers that place it. The game
also keeps the slider values, because a slider edits a Go value and the
program then pushes it into the mixer, and the capture state, which is
only used when `-mic` is given.

```go
// Command audio is the sound tour: positional voices orbiting the
// listener with Doppler and occlusion on sliders, panning or the
// binaural head model on a checkbox, a shared reverb and a low-pass
// filter on sliders, fades, pitch, voice priorities under a small voice
// cap, a synthesised music stream, -music to stream an Ogg, MP3 or WAV
// file from disk, -zone to put a reverb zone at the listener so the room
// changes as the orbiting sources pass through it, and -mic to record
// from the microphone and draw a level meter.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

type source struct {
	voice *audio.Voice
	color gfx.Color
	speed float32
	dist  float32
	pos   lin.Vec3
}

type game struct {
	seconds   float64
	shot      string
	musicPath string
	zone      bool
	mic       bool

	font    *gfx.Font
	ui      *ui.Context
	tone    *audio.Sound
	click   *audio.Sound
	pad     *audio.Voice
	sources []source
	music   *audio.Music
	stream  *audio.Voice

	capture *audio.Capture
	micBuf  []float32
	micErr  string
	micPeak float32

	reverb    float32
	room      float32
	cutoff    float32
	pitch     float32
	occlusion float32
	doppler   float32
	binaural  bool
	shotDone  bool
}
```

## Init: sounds, voices and the stream

`ctx.Audio` is the mixer. `SetMaxVoices(12)` is small on purpose: the
burst button plays forty voices, and when the mixer is full a new voice
takes the place of the quietest voice of the lowest priority no higher
than its own, or is refused. That is what a game meets when a fight gets
loud.

`audio.Sine(freq, seconds, rate)` synthesises samples, and `m.NewSound`
turns them into a sound that can be played any number of times.
`m.Rate()` is the device's sample rate, which the synthesis has to match.

`m.Play` starts a sound and returns the `*audio.Voice`. `audio.PlayOptions`
carries everything about that one playback: `Loop`, `Volume`, `LowPass`
in hertz, `Reverb` as the send level into the shared reverb, `FadeIn` in
seconds, and `Priority`, which decides what survives when the cap is
reached. A positional voice sets `Positional` with a `MinDistance`, the
radius inside which it is at full volume, and a `MaxDistance`, beyond
which it is silent.

`SetDoppler` scales the pitch shift from velocity and `SetSpeedOfSound`
sets what that shift is measured against. Both are in the same units as
the positions, which here are view units, so the speed of sound is 3000
of them per second to keep the orbit's shift musical.

The music is either an `audio.Music` opened from a file with
`m.OpenMusic`, or the `arpeggio` value at the bottom of this program.
Both are played with `PlayStream`, which pulls samples as the device
needs them instead of holding the whole sound in memory.

`m.OpenCapture` opens the default input device and is the only part of
this program that can fail for a reason outside it, so the error is kept
and shown rather than returned: a machine with no microphone still runs
the rest. The buffer is a tenth of a second at the capture rate, which
is what the update drains into.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	m := ctx.Audio
	m.SetMaxVoices(12) // small on purpose, so the burst button shows stealing
	if g.tone, err = m.NewSound(audio.Sine(440, 0.3, m.Rate())); err != nil {
		return err
	}
	if g.click, err = m.NewSound(audio.Sine(1200, 0.05, m.Rate())); err != nil {
		return err
	}
	pad, err := m.NewSound(audio.Sine(110, 2, m.Rate()))
	if err != nil {
		return err
	}
	g.reverb, g.room, g.cutoff, g.pitch = 0.3, 0.6, 4000, 1
	m.SetReverb(audio.ReverbSettings{RoomSize: g.room, Wet: g.reverb})
	// Doppler in pixels: a high speed of sound keeps the orbit's shift
	// to a semitone or so at full factor.
	g.doppler = 1
	m.SetDoppler(g.doppler)
	m.SetSpeedOfSound(3000)
	// A looping pad the sliders act on.
	g.pad = m.Play(pad, audio.PlayOptions{Loop: true, Volume: 0.35, LowPass: g.cutoff, Reverb: 1, FadeIn: 1.5, Priority: 10})
	// Three orbiting positional sources at different pitches.
	for i, f := range []float64{330, 495, 660} {
		snd, err := m.NewSound(audio.Sine(f, 1, m.Rate()))
		if err != nil {
			return err
		}
		v := m.Play(snd, audio.PlayOptions{Loop: true, Volume: 0.3, Positional: true, MinDistance: 60, MaxDistance: 500, Priority: 10})
		g.sources = append(g.sources, source{voice: v, speed: 0.4 + 0.3*float32(i), dist: 120 + 90*float32(i),
			color: gfx.RGB(uint8(255-80*i), uint8(120+60*i), uint8(80*i+60))})
	}
	// Music: a file if given, otherwise a synthesised arpeggio stream.
	if g.musicPath != "" {
		f, err := os.Open(g.musicPath)
		if err != nil {
			return err
		}
		if g.music, err = m.OpenMusic(f, true); err != nil {
			return err
		}
		g.stream = m.PlayStream(g.music, audio.PlayOptions{Volume: 0.5, Priority: 20})
	} else {
		g.stream = m.PlayStream(&arpeggio{rate: m.Rate()}, audio.PlayOptions{Volume: 0.18, Reverb: 0.6, Priority: 20})
	}
	// -mic records from the default input. Nothing is played back; the
	// samples are read every update so the ring does not fill, and the
	// meter reads the level. A run without -mic never opens the device,
	// and a headless run has none to open.
	if g.mic {
		g.capture, err = m.OpenCapture(audio.CaptureOptions{})
		if err != nil {
			g.micErr = err.Error()
		} else {
			g.micBuf = make([]float32, g.capture.Rate()/10)
		}
	}
	return nil
}
```

## Shutdown

The capture and the music file are closed and the font destroyed. Voices
belong to the mixer, which the engine shuts down.

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.capture != nil {
		g.capture.Close()
	}
	if g.music != nil {
		g.music.Close()
	}
	g.font.Destroy()
}
```

## Update: the listener, the sources and the microphone

`SetListener2D` places the listener, which is what panning and distance
are measured from. With `-zone`, `SetReverbZones` replaces the list of
reverb zones: a centre, a radius, a fade width over which the zone's
settings blend in, and the settings themselves. The listener follows the
mouse, so walking into the zone changes the room.

Each source is placed on its circle from `ctx.Time` and its own speed.
`SetVelocity` is what Doppler reads; the value is the orbit's tangent,
the derivative of the position, so a source moving towards the listener
rises in pitch. Giving it a position without a velocity is legal and
means no shift.

The capture is drained every update. `Read` copies what the device has
recorded into the buffer and returns how much, so looping until it
returns zero keeps the ring from filling; nothing is played back here.
`Level` is the recent peak, held with a decay so the meter is readable.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	// The listener sits at the window centre; sources circle it. With
	// -zone, a large hall covers the left half of the window and the
	// listener is moved with the mouse so it can walk in and out.
	cx, cy := ctx.Width/2, ctx.Height/2
	lx, ly := cx, cy
	if g.zone {
		lx, ly = ctx.Input.Mouse()
		ctx.Audio.SetReverbZones([]audio.ReverbZone{{
			Center: lin.V3(cx/2, cy, 0), Radius: cx / 2, Fade: cx / 8,
			Settings: audio.ReverbSettings{RoomSize: 0.95, Damping: 0.2, Wet: 0.8},
		}})
	}
	ctx.Audio.SetListener2D(lx, ly)
	for i := range g.sources {
		s := &g.sources[i]
		a := float64(float32(ctx.Time) * s.speed)
		s.pos = lin.V3(cx+s.dist*float32(math.Cos(a)), cy+s.dist*float32(math.Sin(a)), 0)
		s.voice.SetPosition(s.pos)
		// The orbit's tangent, in pixels per second, drives Doppler.
		v := s.dist * s.speed
		s.voice.SetVelocity(lin.V3(-v*float32(math.Sin(a)), v*float32(math.Cos(a)), 0))
	}
	if g.capture != nil {
		// Drain what the device recorded since the last update, so the
		// ring never fills, and hold the peak for a readable meter.
		for g.capture.Read(g.micBuf) > 0 {
		}
		g.micPeak = max(g.capture.Level(), g.micPeak-float32(ctx.Delta))
	}
	return nil
}
```

## Draw: the scene

The sources are drawn as squares with translucent rings around them.
Colours are `gfx.Color` in linear space; setting `c.A = 0.25` makes the
ring translucent, and the 2D stream premultiplies it on the way to the
GPU. `ctx.Audio.Listener()` reads the listener back rather than the
program remembering where it put it.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	cx := ctx.Width / 2
	for _, s := range g.sources {
		for r := float32(3); r <= 9; r += 3 { // rings fade with distance
			c := s.color
			c.A = 0.25
			gr.FillRect(s.pos.X-r*3, s.pos.Y-r*3, r*6, r*6, c)
		}
		gr.FillRect(s.pos.X-6, s.pos.Y-6, 12, 12, s.color)
	}
	l := ctx.Audio.Listener().Position
	if g.zone {
		gr.FillRect(0, 0, cx, ctx.Height, gfx.RGBA(80, 90, 160, 40)) // the hall
		r := ctx.Audio.Reverb()
		gr.DrawText(g.font, fmt.Sprintf("hall: wet %.2f room %.2f (move the mouse)", r.Wet, r.RoomSize), 16, ctx.Height-30, gfx.RGB(200, 200, 210))
	}
	gr.FillRect(l.X-8, l.Y-8, 16, 16, gfx.RGB(240, 240, 250))
	gr.DrawText(g.font, "listener", l.X-24, l.Y+12, gfx.RGB(200, 200, 210))
```

## Draw: the panel

The panel is one column of sliders and rows of buttons, rebuilt every
frame. Each slider returns whether it moved, so the mixer call happens
only on the frames where something changed. `u.Row(2, ...)` lays the next
two widgets out side by side.

The calls show what a voice and the mixer each own. `SetLowPass`,
`SetOcclusion` and `FadeTo` are on the voice, so they affect one
playback; `SetReverb`, `SetDoppler` and `SetSpatial` are on the mixer, so
they affect everything. `Pitch` and `Pan` are set when a voice starts, in
the play options. `ctx.Audio.Playing()` is the live voice count, which
the burst button pushes against the cap.

`SetSpatial(audio.SpatialSettings{Binaural: ...})` swaps the pan law for
a head model with an interaural delay and a head shadow, which is worth
turning on for headphones and wrong for speakers.

The meter is drawn on a decibel scale rather than on the raw amplitude,
because a linear meter spends most of its length on sounds nobody can
hear. `Dropped` counts the frames the ring lost, which is what a game
watches to know its drain loop is keeping up.

```go
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Audio", ui.Rect{X: 16, Y: 16, W: 300, H: 560}, func() {
			u.Label(fmt.Sprintf("%d voices playing (cap 12)", ctx.Audio.Playing()))
			if u.Slider("Reverb wet", &g.reverb, 0, 1) || u.Slider("Room size", &g.room, 0, 1) {
				ctx.Audio.SetReverb(audio.ReverbSettings{RoomSize: g.room, Wet: g.reverb})
			}
			if u.Slider("Pad low-pass Hz", &g.cutoff, 200, 8000) {
				g.pad.SetLowPass(g.cutoff)
			}
			if u.Slider("Orbit occlusion", &g.occlusion, 0, 1) {
				for _, s := range g.sources {
					s.voice.SetOcclusion(g.occlusion)
				}
			}
			if u.Slider("Doppler factor", &g.doppler, 0, 3) {
				ctx.Audio.SetDoppler(g.doppler)
			}
			if u.Checkbox("Binaural (headphones)", &g.binaural) {
				ctx.Audio.SetSpatial(audio.SpatialSettings{Binaural: g.binaural})
			}
			u.Slider("Click pitch", &g.pitch, 0.5, 2)
			u.Row(2, func() {
				if u.Button("Click") {
					ctx.Audio.Play(g.click, audio.PlayOptions{Volume: 0.6, Pitch: g.pitch, Reverb: g.reverb})
				}
				if u.Button("Burst 40") {
					for i := range 40 {
						// Low-priority voices; the cap makes the mixer steal the quietest.
						ctx.Audio.Play(g.tone, audio.PlayOptions{Volume: 0.2, Pitch: 0.8 + 0.02*float32(i), Pan: float32(i%5-2) / 2})
					}
				}
			})
			u.Row(2, func() {
				if u.Button("Fade pad out") {
					g.pad.FadeTo(0, 1.5)
				}
				if u.Button("Fade pad in") {
					g.pad.FadeTo(0.35, 1.5)
				}
			})
			u.Row(2, func() {
				if u.Button("Pause music") {
					g.stream.SetPaused(true)
				}
				if u.Button("Resume music") {
					g.stream.SetPaused(false)
				}
			})
			if g.music != nil {
				u.Label(fmt.Sprintf("Streaming %s, %.1f s buffered", g.musicPath, g.music.Buffered()))
			} else {
				u.Label("Music is a synthesised Stream; pass -music file.ogg to stream a file.")
			}
			switch {
			case g.capture != nil:
				// A meter on a decibel scale, silence at -60 dB.
				db := float32(0)
				if g.micPeak > 0 {
					db = max(0, (20*float32(math.Log10(float64(g.micPeak)))+60)/60)
				}
				u.Progress(fmt.Sprintf("Mic %.3f at %d Hz, %d dropped", g.capture.Level(), g.capture.Rate(), g.capture.Dropped()), db)
			case g.micErr != "":
				u.Label("Microphone: " + g.micErr)
			}
		})
	})
	return nil
}
```

## A Stream of synthesised music

`arpeggio` implements the mixer's stream interface: `Read` fills a buffer
of interleaved stereo float samples and returns the number of frames
written. It is called from the audio thread, so it must not allocate,
block or touch the graphics device. Everything it needs is state on the
value: the sample rate, the elapsed time and the oscillator's phase.

Keeping the phase rather than recomputing it from the time is what
prevents a click at every buffer boundary.

```go
// arpeggio is a Stream: it synthesises a slow chord arpeggio on demand,
// which is how procedural music or a custom decoder plugs into the mixer.
type arpeggio struct {
	rate  int
	t     float64
	phase float64
}

var notes = []float64{261.63, 329.63, 392.00, 523.25, 392.00, 329.63}

func (a *arpeggio) Read(out []float32) int {
	frames := len(out) / 2
	for i := range frames {
		step := int(a.t*3) % len(notes)
		f := notes[step]
		env := 1 - math.Mod(a.t*3, 1) // decays over each note
		a.phase += 2 * math.Pi * f / float64(a.rate)
		s := float32(math.Sin(a.phase)*0.6+math.Sin(2*a.phase)*0.25) * float32(env)
		out[i*2], out[i*2+1] = s, s
		a.t += 1 / float64(a.rate)
	}
	return frames
}
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	music := flag.String("music", "", "Ogg, MP3 or WAV file to stream")
	zone := flag.Bool("zone", false, "put a reverb zone over the left half and move the listener with the mouse")
	mic := flag.Bool("mic", false, "record from the default microphone and show a level meter")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip audio", Width: 900, Height: 600, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, musicPath: *music, zone: *zone, mic: *mic})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio:", err)
		os.Exit(1)
	}
}
```

## What to try

- Drag Doppler to 3 in the panel and listen to the orbiting sources bend
  as they pass.
- Raise `SetMaxVoices` in `Init` to 64 and press Burst 40: nothing is
  stolen any more.
- Give the burst voices a `Priority` above the pad's in `Draw` and watch
  which sound survives instead.
- Change the notes in `notes` or the harmonics in `arpeggio.Read` and
  hear the stream change without restarting anything.
- Pass `-zone` and move the mouse across the boundary to hear the reverb
  zone blend over the fade width set in `Update`.
- Pass `-mic` with headphones on, and play the captured buffer back
  through a `Stream` in `Update` instead of discarding it.
