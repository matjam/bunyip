package audio

// Stream produces stereo float32 frames on demand at the mixer's rate; it
// is how music and synthesised sound play without being decoded up front.
// Read runs on the mixing thread and must return promptly. It may call
// mixer setters or Play, but must not call Voice.Seek. Do not retain out.
type Stream interface {
	// Read fills out with len(out)/2 frames and reports how many frames it
	// wrote, between zero and len(out)/2; fewer than asked means the stream
	// has ended. For a temporary underrun, fill the remainder with silence
	// and report the full frame count to keep the voice alive.
	Read(out []float32) int
}

// PlayStream starts a stream as a voice. Pitch and Doppler resample its
// frames continuously across blocks. Loop has no effect: a looping stream
// loops itself. The voice buffers up to 513 source frames of lookahead.
// The mixer does not close the stream when the voice ends. The caller
// owns its lifetime and must not share a stateful stream between voices
// unless the stream explicitly supports that use.
func (m *Mixer) PlayStream(s Stream, opts PlayOptions) *Voice {
	v := m.newVoice(opts)
	v.stream = s
	return m.add(v)
}
