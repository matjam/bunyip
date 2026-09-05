package audio

import "github.com/matjam/bunyip/lin"

// VoiceInfo is one playing voice as a debug view sees it: a snapshot,
// taken under the mixer's lock, of what the voice is doing now.
type VoiceInfo struct {
	// Bus is the name of the bus the voice plays through, empty when it
	// plays straight through the master.
	Bus string
	// Volume, Pan and Pitch are the voice's own settings, before the bus
	// and master gains.
	Volume, Pan, Pitch float32
	// Seconds is how far into the sound or stream the voice has played.
	Seconds float64
	// Stream is set for a voice playing a stream (music, a tracker
	// module) rather than a decoded sound.
	Stream bool
	// Positional voices are heard from Position in the listener's world;
	// the rest are panned.
	Positional bool
	Position   lin.Vec3

	Loop, Paused, Muted, Soloed bool
	Priority                    int
	Reverb, Occlusion           float32
}

// Voices returns a snapshot of the playing voices, in the order the
// mixer holds them, for a debug view or a test. The values are copies:
// changing them changes nothing, and a voice may end before the caller
// reads them.
func (m *Mixer) Voices() []VoiceInfo {
	m.mu.Lock()
	voices := append([]*Voice(nil), m.voices...)
	out := make([]VoiceInfo, 0, len(voices))
	for _, v := range voices {
		info := VoiceInfo{
			Volume: v.vol, Pan: v.pan, Pitch: v.pitch, Stream: v.stream != nil,
			Positional: v.positional, Position: v.position, Loop: v.loop,
			Paused: v.paused, Muted: v.mute, Soloed: v.solo, Priority: v.priority,
			Reverb: v.reverb, Occlusion: v.occlusion,
		}
		if v.bus != nil {
			info.Bus = v.bus.name
		}
		out = append(out, info)
	}
	m.mu.Unlock()
	// Position reads the last finished block's playhead and takes no
	// lock, so it is read after this one is released.
	for i, v := range voices {
		out[i].Seconds = v.Position()
	}
	return out
}

// Buses returns the mixer's buses in the order they were made, starting
// with music, effects and dialogue, for a mixing panel that shows them
// all.
func (m *Mixer) Buses() []*Bus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Bus(nil), m.busList...)
}
