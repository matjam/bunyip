---
title: Audio
order: 11
summary: sounds, music, positional voices, reverb, occlusion, effects and tracker music
---

The [audio](../pkg/audio.html) package mixes in Go on the output device's
own thread. A game gets a `Mixer` from `ctx.Audio` and never touches the
device.

## Sounds and voices

`NewSound` converts decoded PCM to the mixer's format; `Decode` reads
WAV, Ogg Vorbis and MP3 from bytes, and `Sine` synthesises a tone for
tests and placeholders. `Play` starts a `Voice` with options: volume,
pan, loop, pitch, a fade-in, a low-pass cutoff, a reverb send, occlusion
and a priority. The voice can be adjusted while it runs: `SetVolume`,
`FadeTo`, `FadeOut`, `SetPitch`, `SetPaused`, `SetLowPass`,
`SetPosition`, `SetOcclusion`, `SetMute`, `SetSolo`.

Gains ramp across each block, so changes never click. `Voice.Position`
and `Seek` read and move the playhead, `Sound.Duration` is its length,
and `Voice.OnDone` runs a callback when the voice ends, for chaining
clips.

## Buses

Voices play through a `Bus`, and a settings screen binds its sliders to
buses rather than to every voice. `Music`, `Effects` and `Dialogue` come
ready; `NewBus` makes more. A bus has its own volume, pause, mute, solo
and reverb, applied with the same ramps. Set `PlayOptions.Bus` to choose
one.

## Pausing

`Mixer.SetPaused` holds every voice at once; a pause menu calls it, and
the engine calls it for you when `Config.PauseUnfocused` is set and the
window loses focus. `Bus.SetPaused` and `Voice.SetPaused` hold less. A
pause fades out over the block it lands in and the resume fades back in,
so neither clicks. Each level is kept separately: resuming the mixer
leaves a paused bus paused.

## Mute and solo

Mute is not pause: a muted voice or bus keeps playing silently, so
unmuting picks up wherever the sound has got to. `Voice.SetMute` and
`Bus.SetMute` do that. Solo is the mixing desk's audition button: while
any voice is soloed only soloed voices are heard, and while any bus is
soloed only soloed buses (a voice on no bus counts as a bus of its own).
`Voice.SetSolo` and `Bus.SetSolo` set it, and clearing the last solo
brings everything back.

## Music

`OpenMusic` streams a WAV, Ogg or MP3 file, decoding a couple of seconds
ahead on its own goroutine; `PlayStream` plays it, and `Close` stops it.
`Music.Duration` and `Music.Seek` work for all three formats.
Anything implementing `Stream` (fill a buffer of stereo frames) plays the
same way, which is how procedural music and the tracker player plug in.

## Positional audio

Set `Positional` and a `Position` on a voice, and put the listener where
the camera is each frame with `SetListener` (or `SetListener2D` for a 2D
game). Volume falls with distance between `MinDistance` and
`MaxDistance`, and the voice pans by direction.

### Doppler

`SetDoppler(1)` turns the Doppler effect on: a positional sound closing
on the listener plays sharp and one receding plays flat, by how fast each
moves along the line between them. The mixer does not integrate motion,
so the game gives it velocities: `Listener.Velocity` and
`Voice.SetVelocity` (or `PlayOptions.Velocity`), in world units per
second. They are measured against the speed of sound, 343 by default,
which suits metres; a game in pixels sets `SetSpeedOfSound` higher to
keep the effect subtle. The factor scales the shift, so 0.5 halves it and
0 (the default) is off. Streams have no pitch, so Doppler leaves them
alone.

### Occlusion

A sound behind a wall is quieter and duller than one in the open.
`PlayOptions.Occlusion` and `Voice.SetOcclusion` take 0 (clear) to 1
(fully blocked, 20 dB down and low-passed to 400 Hz), with the amounts in
between on a decibel scale. The mixer has no scene, so the game decides:
cast a physics ray from the listener to the source each frame and set the
occlusion from what it hits, or fade it as a door opens.

## Reverb

`SetReverb` configures the mixer's shared reverb, a Freeverb-style comb
and all-pass network; voices feed it through their `Reverb` send, and the
tail is mixed on top of the dry output. `ReverbSettings` has a room size,
damping, stereo width and wet level, and its zero value is no reverb.

### Reverb zones

A cave should not sound like the field outside it. `SetReverbZones`
takes a list of `ReverbZone`s, each a sphere with its own settings, and
the mixer checks them whenever the listener moves: inside a zone its
settings replace the shared reverb, blended in over `Fade` units from the
edge so walking through the doorway never jumps. A zero `Fade` blends
across the whole radius; set it to a fraction of the radius for a room
that sounds the same everywhere but its threshold. Where zones overlap
the one the listener is furthest inside wins. `Mixer.Reverb` reports what
is in effect, for a debug overlay.

### Bus reverb

`Bus.SetReverb` gives a bus a reverb of its own, and voices on that bus
send to it instead of the shared one. That keeps the music dry while the
cave's effects ring, or gives dialogue a small room while the world has a
large one.

## Voice limits

`SetMaxVoices` caps the number of voices playing at once. When the cap
is reached, a new voice replaces the quietest voice whose priority is no
higher than its own, so a footstep never steals from the dialogue.

## Tracker music

[audio/tracker](../pkg/audio/tracker.html) loads and plays MOD, S3M, XM
and IT modules with one engine: envelopes, new-note actions, loops, the
IT filter and ProTracker's quirks. The player is a `Stream`, so
`PlayStream` plays it. `bunyip-play` plays any supported file from the
command line and can dump what the device received for comparison.

### Tracker control

The player can be driven while it plays; its methods take the same lock
as `Read`, so the game loop calls them freely. `Position` reports the
song position (an index into the order list) and row, `Length` counts the
positions and `Rows` the rows in the pattern at one, and `Seek(order,
row)` jumps there, cutting whatever was sounding, so a level with several
sections in one module can seek between them. `Mute(channel, true)`
silences a pattern channel while the song plays on, `Solo` auditions
one, and `Channels` says how many there are. A game that drops the drums
while the player hides mutes their channel and unmutes it later without
a break.
