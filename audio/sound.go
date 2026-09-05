package audio

import (
	"fmt"
	"math"
)

// PCM is decoded audio of any channel count and rate, interleaved.
type PCM struct {
	Samples  []float32 // interleaved channel samples, nominally -1..1
	Channels int       // samples per frame; NewSound accepts 1 or 2
	Rate     int       // frames per second; must be positive
}

// Sound is a PCM clip converted to the mixer's stereo rate.
type Sound struct {
	samples []float32
	rate    int
}

// Frames is the sound's length in frames.
func (s *Sound) Frames() int { return len(s.samples) / 2 }

// Duration is the sound's length in seconds.
func (s *Sound) Duration() float64 { return float64(s.Frames()) / float64(s.rate) }

// NewSound converts decoded PCM to the mixer's format: stereo at the mixer
// rate, resampled linearly when the rates differ.
// It copies p.Samples, which must contain a whole number of mono or
// stereo frames. Unsupported channel counts or invalid PCM return an error.
func (m *Mixer) NewSound(p PCM) (*Sound, error) {
	if p.Channels < 1 || p.Channels > 2 || p.Rate <= 0 || len(p.Samples)%p.Channels != 0 {
		return nil, fmt.Errorf("audio: unsupported PCM: %d channels at %d Hz, %d samples", p.Channels, p.Rate, len(p.Samples))
	}
	frames := len(p.Samples) / p.Channels
	stereo := make([]float32, frames*2)
	for i := range frames {
		if p.Channels == 1 {
			stereo[i*2], stereo[i*2+1] = p.Samples[i], p.Samples[i]
		} else {
			stereo[i*2], stereo[i*2+1] = p.Samples[i*2], p.Samples[i*2+1]
		}
	}
	if p.Rate != m.rate {
		stereo = resample(stereo, p.Rate, m.rate)
	}
	return &Sound{samples: stereo, rate: m.rate}, nil
}

// resample converts interleaved stereo from one rate to another with
// linear interpolation, which is adequate for game sound effects.
func resample(in []float32, from, to int) []float32 {
	frames := len(in) / 2
	outFrames := int(math.Round(float64(frames) * float64(to) / float64(from)))
	out := make([]float32, outFrames*2)
	step := float64(from) / float64(to)
	for i := range outFrames {
		src := float64(i) * step
		j := int(src)
		t := float32(src - float64(j))
		k := min(j+1, frames-1)
		j = min(j, frames-1)
		out[i*2] = in[j*2]*(1-t) + in[k*2]*t
		out[i*2+1] = in[j*2+1]*(1-t) + in[k*2+1]*t
	}
	return out
}

// Sine synthesises a tone, handy for tests and placeholder effects.
func Sine(freq float64, seconds float64, rate int) PCM {
	n := int(seconds * float64(rate))
	s := make([]float32, n)
	for i := range n {
		env := float32(1)
		if fade := rate / 100; i < fade { // 10 ms fades avoid clicks
			env = float32(i) / float32(fade)
		} else if n-i < fade {
			env = float32(n-i) / float32(fade)
		}
		s[i] = env * float32(math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return PCM{Samples: s, Channels: 1, Rate: rate}
}

func sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }
