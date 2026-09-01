package audio

// Stream produces stereo float32 frames on demand at the mixer's rate; it
// is how music and synthesised sound play without being decoded up front.
type Stream interface {
	// Read fills out with len(out)/2 frames and reports how many frames it
	// wrote; fewer than asked means the stream has ended.
	Read(out []float32) int
}

// PlayStream starts a stream as a voice. Loop has no effect; a looping
// stream loops itself.
func (m *Mixer) PlayStream(s Stream, opts PlayOptions) *Voice {
	if opts.Volume == 0 {
		opts.Volume = 1
	}
	v := &Voice{stream: s, vol: opts.Volume, pan: opts.Pan}
	m.mu.Lock()
	m.voices = append(m.voices, v)
	m.mu.Unlock()
	return v
}

// mixStream pulls from the voice's stream into scratch and accumulates.
func (v *Voice) mixStream(out []float32, master float32, scratch []float32) bool {
	if v.stop {
		return false
	}
	n := v.stream.Read(scratch[:len(out)])
	pan := max(-1, min(1, v.pan))
	left := v.vol * master * sqrt32((1-pan)/2)
	right := v.vol * master * sqrt32((1+pan)/2)
	for i := range n {
		out[i*2] += scratch[i*2] * left
		out[i*2+1] += scratch[i*2+1] * right
	}
	return n*2 == len(out)
}
