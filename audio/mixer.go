// Package audio mixes sounds in Go and hands float32 stereo frames to the
// platform's output device. A Sound holds decoded samples at the mixer's
// rate; Play starts a Voice that can be adjusted or stopped while it runs.
// Mixing happens on the audio device's thread, so every method is safe to
// call from the game loop.
package audio

import (
	"sync"
)

// Mixer sums playing voices into the output stream.
type Mixer struct {
	mu     sync.Mutex
	rate   int
	voices []*Voice
	master float32
}

// NewMixer makes a mixer for the given output sample rate.
func NewMixer(rate int) *Mixer {
	return &Mixer{rate: rate, master: 1}
}

// Rate is the sample rate sounds are stored and mixed at.
func (m *Mixer) Rate() int { return m.rate }

// SetMasterVolume scales every voice; 1 is unity.
func (m *Mixer) SetMasterVolume(v float32) {
	m.mu.Lock()
	m.master = v
	m.mu.Unlock()
}

// PlayOptions shape a voice at start.
type PlayOptions struct {
	Volume float32 // zero means 1
	Pan    float32 // -1 left .. +1 right
	Loop   bool
}

// Play starts a sound and returns its voice.
func (m *Mixer) Play(s *Sound, opts PlayOptions) *Voice {
	if opts.Volume == 0 {
		opts.Volume = 1
	}
	v := &Voice{snd: s, vol: opts.Volume, pan: opts.Pan, loop: opts.Loop}
	m.mu.Lock()
	m.voices = append(m.voices, v)
	m.mu.Unlock()
	return v
}

// StopAll silences every voice.
func (m *Mixer) StopAll() {
	m.mu.Lock()
	for _, v := range m.voices {
		v.done = true
	}
	m.voices = m.voices[:0]
	m.mu.Unlock()
}

// Playing counts active voices.
func (m *Mixer) Playing() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// Mix writes len(out)/2 stereo frames, called by the output device.
func (m *Mixer) Mix(out []float32) {
	clear(out)
	m.mu.Lock()
	defer m.mu.Unlock()
	live := m.voices[:0]
	for _, v := range m.voices {
		if v.mix(out, m.master) {
			live = append(live, v)
		} else {
			v.done = true
		}
	}
	for i := len(live); i < len(m.voices); i++ {
		m.voices[i] = nil
	}
	m.voices = live
	for i, s := range out {
		out[i] = max(-1, min(1, s))
	}
}

// Voice is one playing sound.
type Voice struct {
	snd  *Sound
	pos  int // frames
	vol  float32
	pan  float32
	loop bool
	done bool
	stop bool
}

// Stop ends the voice.
func (v *Voice) Stop() { v.stop = true }

// SetVolume changes the voice's gain; 1 is unity.
func (v *Voice) SetVolume(vol float32) { v.vol = vol }

// SetPan moves the voice between -1 (left) and +1 (right).
func (v *Voice) SetPan(pan float32) { v.pan = pan }

// Playing reports whether the voice is still audible.
func (v *Voice) Playing() bool { return !v.done }

// mix accumulates the voice into out and reports whether it continues.
func (v *Voice) mix(out []float32, master float32) bool {
	if v.stop || v.snd == nil || len(v.snd.samples) == 0 {
		return false
	}
	// Equal-power pan.
	pan := max(-1, min(1, v.pan))
	left := v.vol * master * sqrt32((1-pan)/2)
	right := v.vol * master * sqrt32((1+pan)/2)
	frames := len(out) / 2
	total := len(v.snd.samples) / 2
	for i := range frames {
		if v.pos >= total {
			if !v.loop {
				return false
			}
			v.pos = 0
		}
		out[i*2] += v.snd.samples[v.pos*2] * left
		out[i*2+1] += v.snd.samples[v.pos*2+1] * right
		v.pos++
	}
	return true
}
