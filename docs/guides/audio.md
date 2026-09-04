---
title: Audio
group: Audio
order: 1
summary: sounds, music, positional voices, reverb, occlusion, effects and tracker music
---

The [audio](../pkg/audio.html) package mixes in Go on the output device's
own thread. A game gets a `Mixer` from `ctx.Audio` and never touches the
device.

Every method is safe to call from the game loop. A setter copies its
value in under a short lock and the mixer picks it up at the start of the
next block, so the game thread never waits for a block to finish; the
mixer in turn reads streams with no lock held, so a `Stream.Read` that
takes locks of its own, or that calls back into the mixer, cannot stall
the game. The exception is `Voice.Seek`, which moves the playhead the
mixer is reading from and so waits for the block in flight.

## Sounds and voices

`NewSound` converts decoded PCM to the mixer's format; `Decode` reads
WAV, Ogg Vorbis and MP3 from bytes, and `Sine` synthesises a tone for
tests and placeholders. `Play` starts a `Voice` with options: volume,
pan, loop, pitch, a fade-in, a low-pass cutoff, a reverb send, occlusion
and a priority. The voice can be adjusted while it runs: `SetVolume`,
`FadeTo`, `FadeOut`, `SetPitch`, `SetPaused`, `SetLowPass`,
`SetPosition`, `SetOcclusion`, `SetMute`, `SetSolo`.

Gains ramp across each block, so changes never click. `Stop`, `StopAll`
and a voice that loses its slot ramp to silence over about a
millisecond, so cutting a sound off mid-cycle is inaudible; the voice
leaves `Playing` at once and the mixer spends that millisecond fading
what is left. `Voice.Position` and `Seek` read and move the playhead,
`Sound.Duration` is its length, and `Voice.OnDone` runs a callback when
the voice ends, for chaining clips.

```go
pcm, err := audio.Decode(data) // WAV, Ogg Vorbis or MP3 bytes
if err != nil {
	return err
}
hit, err := ctx.Audio.NewSound(pcm)
if err != nil {
	return err
}
v := ctx.Audio.Play(hit, audio.PlayOptions{Volume: 0.8, Pan: -0.3, Pitch: 1.1, FadeIn: 0.05})
v.OnDone(func() { ctx.Audio.Play(g.ricochet, audio.PlayOptions{}) })

// Later, while it plays.
v.SetVolume(0.4)
v.SetLowPass(1200)
v.FadeOut(0.5)
```

`audio.Sine(440, 0.3, ctx.Audio.Rate())` makes a `PCM` without a file.
The examples and tests use it to get something to play.

## Buses

Voices play through a `Bus`. A settings screen binds its sliders to
buses rather than to every voice. `Music`, `Effects` and `Dialogue`
already exist; `NewBus` makes more. A bus has its own volume, pause,
mute, solo and reverb, applied with the same ramps. To choose a bus, set
`PlayOptions.Bus`.

```go
m := ctx.Audio
m.Music().SetVolume(0.4) // the settings screen's music slider
m.Effects().SetVolume(0.9)

steps := m.NewBus("footsteps")
m.Play(g.step, audio.PlayOptions{Bus: steps, Volume: 0.6})
m.Play(g.line, audio.PlayOptions{Bus: m.Dialogue(), Priority: 100})
steps.SetVolume(0.3) // the whole group at once, ramped
```

`Mixer.Bus(name)` finds a bus made earlier, so the settings screen does
not have to be handed one.

## Pausing

To pause every voice at once, call `Mixer.SetPaused`. A pause menu calls
it, and the engine calls it for you when `Config.PauseUnfocused` is set
and the window loses focus. `Bus.SetPaused` pauses one bus and
`Voice.SetPaused` one voice. A pause fades out over the block it lands
in and the resume fades back in, so neither clicks. Each level is kept
separately, so resuming the mixer leaves a paused bus paused.

```go
if ctx.Input.KeyPressed(input.KeyEscape) {
	g.menu = !g.menu
	ctx.Audio.SetPaused(g.menu) // everything holds where it is
}
ctx.Audio.Effects().SetPaused(true) // one bus, so effects stop and music plays on
g.engine.SetPaused(true)            // one voice
```

## Mute and solo

To mute a voice or bus, call `Voice.SetMute` or `Bus.SetMute`. A muted
voice or bus keeps playing silently, so unmuting resumes wherever the
sound has reached; to stop the sound advancing, use `SetPaused` instead.
To solo, call `Voice.SetSolo` or `Bus.SetSolo`. While any voice is
soloed, only soloed voices are heard. While any bus is soloed, only
soloed buses are heard, and a voice on no bus is silent. Clearing the
last solo brings everything back.

```go
music := ctx.Audio.Music()
music.SetMute(!music.Muted()) // the mute button; the music keeps playing silently

// A mixing screen plays one bus at a time; g.audition is "" for none.
for _, b := range []*audio.Bus{ctx.Audio.Music(), ctx.Audio.Effects(), ctx.Audio.Dialogue()} {
	b.SetSolo(b.Name() == g.audition)
}
```

## Music

`OpenMusic` streams a WAV, Ogg or MP3 file, decoding a couple of seconds
ahead on its own goroutine; `PlayStream` plays it, and `Close` stops it.
`Music.Duration` and `Music.Seek` work for all three formats.
Anything implementing `Stream` (fill a buffer of stereo frames) plays the
same way. Procedural music and the tracker player use that.

```go
f, err := os.Open("music/theme.ogg")
if err != nil {
	return err
}
if g.music, err = ctx.Audio.OpenMusic(f, true); err != nil { // true loops
	return err
}
g.theme = ctx.Audio.PlayStream(g.music, audio.PlayOptions{Bus: ctx.Audio.Music(), Volume: 0.5})
...
g.music.Seek(30)  // jump half a minute in
g.music.Close()   // in Shutdown; the voice playing it ends
```

`asset.Music(ctx.Audio, fs, "music/theme.ogg", true)` does the same
through the asset sources, so a packed or embedded track opens the same
way as a loose one.

## Positional audio

Set `Positional` and a `Position` on a voice, and put the listener where
the camera is each frame with `SetListener` (or `SetListener2D` for a 2D
game). Volume falls with distance between `MinDistance` and
`MaxDistance`, and the voice pans by direction.

```go
// 2D: the listener goes where the camera is looking.
ctx.Audio.SetListener2D(g.camX, g.camY)

// 3D: position and orientation.
ctx.Audio.SetListener(audio.Listener{Position: g.eye, Forward: g.dir, Up: lin.V3(0, 1, 0)})

torch := ctx.Audio.Play(g.fire, audio.PlayOptions{
	Loop: true, Positional: true,
	Position: lin.V3(4, 1, -8), MinDistance: 2, MaxDistance: 40,
})
torch.SetPosition(lin.V3(6, 1, -8)) // when the source moves
```

### Binaural rendering

By default a positional voice is panned between the two channels by a
constant-power law. `SetSpatial(audio.SpatialSettings{Binaural: true})`
renders it through a head model instead, which is worth having when the
player wears headphones. Each voice then gets:

- an interaural time difference, from Woodworth's formula for the path
  around a sphere, so the far ear hears it up to about 0.66 ms later;
- an interaural level difference, the far ear down by up to 6 dB;
- a head shadow, a one-pole low-pass on each ear whose cutoff falls from
  22 kHz to 1.5 kHz as the source moves to the far side;
- an elevation cue, a shelf at 4 kHz that lifts the high band for a
  source above the listener and drops it for one below.

This is a parametric head model, not a measured head-related transfer
function: it has no ear shape, so it does not tell front from back, and
it is the same head for every player. `HeadRadius` sets that head's size
in metres; the default of 0.0875 is an average adult, and larger heads
give a wider time difference. Everything is interpolated across each
block, so moving sources and a moving listener glide. The zero
`SpatialSettings` restore panning, and voices that are not positional
are unaffected either way.

```go
// A settings screen's headphones switch.
ctx.Audio.SetSpatial(audio.SpatialSettings{Binaural: g.headphones})
```

The cost is a delay line and three one-pole filters per positional
voice, and the signal collapses to mono before the ears, so a stereo
clip loses its own width when it is spatialised.

### Doppler

To turn the Doppler effect on, call `SetDoppler(1)`. A positional sound
closing on the listener then plays sharp, and one receding plays flat,
by how fast each moves along the line between them. The mixer does not
integrate motion, so the game gives it velocities: `Listener.Velocity`
and `Voice.SetVelocity` (or `PlayOptions.Velocity`), in world units per
second. They are measured against the speed of sound, 343 by default,
which suits metres; a game in pixels sets `SetSpeedOfSound` higher to
keep the effect subtle. The factor scales the shift, so 0.5 halves it and
0 (the default) is off. Streams have no pitch, so Doppler does not apply
to them.

```go
ctx.Audio.SetDoppler(1)
ctx.Audio.SetSpeedOfSound(3000) // pixels, not metres, so the shift stays subtle

l := ctx.Audio.Listener()
l.Position, l.Velocity = g.ship.Pos, g.ship.Vel
ctx.Audio.SetListener(l)
train.SetVelocity(lin.V3(0, 0, -40)) // world units per second
```

### Occlusion

A sound behind a wall is quieter and duller than one in the open.
Occlusion applies that. `PlayOptions.Occlusion` and
`Voice.SetOcclusion` take 0 (clear) to 1 (fully blocked, 20 dB down and
low-passed to 400 Hz), with the amounts in between on a decibel scale.
The mixer has no scene, so the game supplies the amount. Cast a physics
ray from the listener to the source each frame and set the occlusion
from what it hits, or fade it as a door opens.

```go
// g.wallBetween is the game's own ray against the level.
for _, s := range g.sources {
	occ := float32(0)
	if g.wallBetween(g.eye, s.pos) {
		occ = 0.8
	}
	s.voice.SetOcclusion(occ)
}
```

## Reverb

`SetReverb` configures the mixer's shared reverb, a Freeverb-style comb
and all-pass network; voices feed it through their `Reverb` send, and the
tail is mixed on top of the dry output. `ReverbSettings` has a room size,
damping, stereo width and wet level, and its zero value is no reverb.
Every field runs from 0 to 1; a larger room size is clamped, because
above about 1.07 the comb feedback reaches one and the tail grows
without end instead of dying away.

```go
ctx.Audio.SetReverb(audio.ReverbSettings{RoomSize: 0.7, Damping: 0.4, Wet: 0.3})

// The send decides how much of each voice reaches it.
ctx.Audio.Play(g.shot, audio.PlayOptions{Reverb: 1})
ctx.Audio.Play(g.click, audio.PlayOptions{Bus: g.menu}) // no send: stays dry
g.pad.SetReverb(0.5)                                    // change it while it plays
```

### Reverb zones

To give an area its own reverb, so a cave does not sound like the field
outside it, call `SetReverbZones`. It takes a list of `ReverbZone`s,
each a sphere with its own settings, and the mixer checks them whenever
the listener moves. Inside a zone its settings replace the shared
reverb, blended in over `Fade` units from the edge so walking through
the doorway never jumps. A zero `Fade` blends across the whole radius;
set it to a fraction of the radius for a room that sounds the same
everywhere but its threshold. Where zones overlap, the one the listener
is furthest inside wins. `Mixer.Reverb` reports what is in effect, for a
debug overlay.

```go
ctx.Audio.SetReverbZones([]audio.ReverbZone{
	{
		Center: lin.V3(0, 0, -40), Radius: 20, Fade: 5, // the cave
		Settings: audio.ReverbSettings{RoomSize: 0.95, Damping: 0.2, Wet: 0.8},
	},
	{
		Center: lin.V3(30, 0, 0), Radius: 8, // the stairwell
		Settings: audio.ReverbSettings{RoomSize: 0.6, Wet: 0.5},
	},
})
here := ctx.Audio.Reverb() // what the listener is hearing now
```

### Bus reverb

`Bus.SetReverb` gives a bus a reverb of its own, and voices on that bus
send to it instead of the shared one. That keeps the music dry while the
cave's effects ring, or gives dialogue a small room while the world has a
large one.

```go
// The cave rings; the music stays dry because it is on another bus.
ctx.Audio.Effects().SetReverb(audio.ReverbSettings{RoomSize: 0.95, Damping: 0.2, Wet: 0.6})
ctx.Audio.Dialogue().SetReverb(audio.ReverbSettings{RoomSize: 0.3, Wet: 0.2})
ctx.Audio.Play(g.step, audio.PlayOptions{Bus: ctx.Audio.Effects(), Reverb: 1})
```

## Voice limits

`SetMaxVoices` caps the number of voices playing at once. When the cap
is reached, a new voice replaces the quietest voice whose priority is no
higher than its own, so a footstep never steals from the dialogue.

```go
ctx.Audio.SetMaxVoices(32)

// Dialogue outranks the world, which outranks incidental noise.
ctx.Audio.Play(g.line, audio.PlayOptions{Bus: ctx.Audio.Dialogue(), Priority: 100})
ctx.Audio.Play(g.explosion, audio.PlayOptions{Priority: 50})
ctx.Audio.Play(g.step, audio.PlayOptions{Priority: 0, Volume: 0.3})
n := ctx.Audio.Playing() // for the debug overlay
```

## Microphone input

`OpenCapture` records from the machine's default input, the one the
desktop is set to. It hands back a `Capture` whose `Read` copies
whatever the device has recorded since the last call and returns at
once, so the game loop calls it every update and never waits for the
device. `Level` is the root mean square of the block that arrived most
recently, which is what a meter draws, and `Close` releases the device.

`CaptureOptions` take a rate and a channel count, defaulting to the
mixer's rate and mono, and `Buffer` sets how many seconds the ring holds
before the oldest samples are dropped; the default is half a second.
`Dropped` counts what was lost that way, so a rising count means the
game is not reading often enough.

```go
if g.mic, err = ctx.Audio.OpenCapture(audio.CaptureOptions{}); err != nil {
	return err // no device, no permission, or a headless run
}
...
// In Update, every frame.
for {
	n := g.mic.Read(g.buf)
	if n == 0 {
		break
	}
	g.pushToVoiceChat(g.buf[:n])
}
g.meter = g.mic.Level()
```

Capture is separate from the mixer: nothing recorded is played back
unless the game plays it, which avoids a feedback loop by default. A
headless run and `Config.NoAudio` have no device at all, so `OpenCapture`
returns `ErrNoDevice` there rather than reaching the hardware behind the
game's back. On macOS the operating system asks the player for
microphone access the first time a game records, and a sandboxed
application needs the audio-input entitlement. `go run ./examples/audio
-mic` records and draws a level meter.

## Tracker music

[audio/tracker](../pkg/audio/tracker.html) loads and plays MOD, S3M, XM
and IT modules with one engine: envelopes, new-note actions, loops, the
IT filter and ProTracker's quirks. The player is a `Stream`, so
`PlayStream` plays it. `bunyip-play` plays any supported file from the
command line and can dump what the device received for comparison.

```go
mod, err := tracker.Load(data) // the format is sniffed from the bytes
if err != nil {
	return err
}
g.player = tracker.NewPlayer(mod, ctx.Audio.Rate())
g.player.Loop = true
g.song = ctx.Audio.PlayStream(g.player, audio.PlayOptions{Bus: ctx.Audio.Music(), Volume: 0.6})
```

`asset.Tracker(fs, "music/level1.xm")` loads a module through the asset
sources. `Module.Title`, `Channels` and `Patterns` are there for a
player screen or for driving visuals from the pattern data.

### Tracker control

The player can be driven while it plays; its methods take the same lock
as `Read`, so the game loop calls them freely. `Position` reports the
song position (an index into the order list) and row, `Length` counts the
positions and `Rows` the rows in the pattern at one, and `Seek(order,
row)` jumps there, cutting whatever was sounding, so a level with several
sections in one module can seek between them. `Mute(channel, true)`
silences a pattern channel while the song plays on, `Solo` plays one
channel alone, and `Channels` says how many there are. To drop the drums
while the player hides, mute their channel and unmute it later; the song
does not break.

```go
p := g.player
order, row := p.Position()
g.hud = fmt.Sprintf("%d/%d row %d of %d", order, p.Length(), row, p.Rows(order))
if g.enteredBossRoom {
	p.Seek(3, 0) // the boss section starts at song position 3
}
p.Mute(g.drums, g.hiding) // the drums drop out while the player hides
for ch := range p.Channels() {
	g.lit[ch] = !p.Muted(ch) // a channel strip for the overlay
}
```
