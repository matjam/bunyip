---
title: Audio
order: 6
summary: sounds, music, positional voices, effects and tracker music
---

The [audio](../pkg/audio.html) package mixes in Go on the output device's
own thread. A game gets a `Mixer` from `ctx.Audio` and never touches the
device.

## Sounds and voices

`NewSound` converts decoded PCM to the mixer's format; `Decode` reads
WAV, Ogg Vorbis and MP3 from bytes, and `Sine` synthesises a tone for
tests and placeholders. `Play` starts a `Voice` with options: volume,
pan, loop, pitch, a fade-in, a low-pass cutoff, a reverb send and a
priority. The voice can be adjusted while it runs: `SetVolume`, `FadeTo`,
`FadeOut`, `SetPitch`, `SetPaused`, `SetLowPass`, `SetPosition`.

Gains ramp across each block, so changes never click.

## Music

`OpenMusic` streams a WAV, Ogg or MP3 file, decoding a couple of seconds
ahead on its own goroutine; `PlayStream` plays it, and `Close` stops it.
Anything implementing `Stream` (fill a buffer of stereo frames) plays the
same way, which is how procedural music and the tracker player plug in.

## Positional audio

Set `Positional` and a `Position` on a voice, and put the listener where
the camera is each frame with `SetListener` (or `SetListener2D` for a 2D
game). Volume falls with distance between `MinDistance` and
`MaxDistance`, and the voice pans by direction.

## Effects and priorities

`SetReverb` configures one shared reverb; voices feed it through their
`Reverb` send. `SetMaxVoices` caps concurrent voices, and when the cap is
reached a new voice replaces the quietest voice of the lowest priority no
higher than its own, so a footstep never steals from the dialogue.

## Tracker music

[audio/tracker](../pkg/audio/tracker.html) loads and plays MOD, S3M, XM
and IT modules with one engine: envelopes, new-note actions, loops, the
IT filter and ProTracker's quirks. The player is a `Stream`, so
`PlayStream` plays it. `bunyip-play` plays any supported file from the
command line and can dump what the device received for comparison.
