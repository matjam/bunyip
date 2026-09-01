package audio

// Stream produces stereo float32 frames on demand at the mixer's rate; it
// is how music and synthesised sound play without being decoded up front.
type Stream interface {
	// Read fills out with len(out)/2 frames and reports how many frames it
	// wrote; fewer than asked means the stream has ended.
	Read(out []float32) int
}

// PlayStream starts a stream as a voice. Loop and Pitch have no effect; a
// looping stream loops itself.
func (m *Mixer) PlayStream(s Stream, opts PlayOptions) *Voice {
	v := m.newVoice(opts)
	v.stream = s
	return m.add(v)
}
