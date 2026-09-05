package audio

// PlaybackState is a voice's effective playback state, including pauses
// applied by its bus or mixer. Muting does not pause playback.
type PlaybackState uint8

const (
	PlaybackStopped PlaybackState = iota // ended or explicitly stopped
	PlaybackPlaying                      // advancing, even while muted or inaudible
	PlaybackPaused                       // held by the voice, its bus or the mixer
)

// String returns "stopped", "playing", "paused", or "unknown".
func (s PlaybackState) String() string {
	switch s {
	case PlaybackStopped:
		return "stopped"
	case PlaybackPlaying:
		return "playing"
	case PlaybackPaused:
		return "paused"
	default:
		return "unknown"
	}
}

// State reports the effective state. A pause is reported immediately,
// including the block that fades out; Stop reports stopped during its ramp.
func (v *Voice) State() PlaybackState {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	if v.done || v.stop {
		return PlaybackStopped
	}
	if v.paused || v.m.paused || (v.bus != nil && v.bus.paused) {
		return PlaybackPaused
	}
	return PlaybackPlaying
}
