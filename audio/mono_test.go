package audio

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestNewSoundDetectsMono checks that a sound made from one channel, or
// from two channels that are the same bit for bit, is marked mono, so
// the mixer reads and filters one channel instead of two.
func TestNewSoundDetectsMono(t *testing.T) {
	m := NewMixer(benchRate)
	mono := Sine(440, 0.1, benchRate)
	same := make([]float32, len(mono.Samples)*2)
	differ := make([]float32, len(mono.Samples)*2)
	for i, v := range mono.Samples {
		same[i*2], same[i*2+1] = v, v
		differ[i*2], differ[i*2+1] = v, v*0.5
	}
	for _, c := range []struct {
		name string
		pcm  PCM
		want bool
	}{
		{"one channel", mono, true},
		{"one channel resampled", Sine(440, 0.1, 44100), true},
		{"identical channels", PCM{Samples: same, Channels: 2, Rate: benchRate}, true},
		{"different channels", PCM{Samples: differ, Channels: 2, Rate: benchRate}, false},
	} {
		snd, err := m.NewSound(c.pcm)
		if err != nil {
			t.Fatal(err)
		}
		if snd.mono != c.want {
			t.Errorf("%s: mono = %v, want %v", c.name, snd.mono, c.want)
		}
	}
}

// TestMonoVoiceChannelsMatch plays a filtered, occluded mono sound
// centred, where both ears must receive exactly the same samples.
func TestMonoVoiceChannelsMatch(t *testing.T) {
	m := NewMixer(benchRate)
	snd, err := m.NewSound(Sine(300, 0.5, benchRate))
	if err != nil {
		t.Fatal(err)
	}
	m.Play(snd, PlayOptions{LowPass: 900, Occlusion: 0.3, Loop: true})
	out := make([]float32, benchBlock*2)
	var peak float32
	for blk := range 20 {
		m.mix(out)
		for i := 0; i < len(out); i += 2 {
			if out[i] != out[i+1] {
				t.Fatalf("block %d frame %d: left %g, right %g", blk, i/2, out[i], out[i+1])
			}
			peak = max(peak, out[i])
		}
	}
	if peak < 0.1 {
		t.Fatalf("mix is nearly silent (peak %g)", peak)
	}
}

// TestBinauralFilterStateSurvivesModeChange switches a filtered stereo
// voice from the head model back to panning. Its filters ran on the mono
// downmix while binaural, and both channels must continue from that
// state rather than one of them resuming from stale values.
func TestBinauralFilterStateSurvivesModeChange(t *testing.T) {
	m := NewMixer(benchRate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	pcm := Sine(300, 1, benchRate)
	s := make([]float32, len(pcm.Samples)*2)
	for i, v := range pcm.Samples {
		s[i*2], s[i*2+1] = v, v*0.25
	}
	snd, err := m.NewSound(PCM{Samples: s, Channels: 2, Rate: benchRate})
	if err != nil {
		t.Fatal(err)
	}
	v := m.Play(snd, PlayOptions{LowPass: 900, Loop: true, Positional: true, Position: lin.Vec3{X: 2}})
	out := make([]float32, benchBlock*2)
	for range 4 {
		m.mix(out)
	}
	m.mu.Lock()
	lp := *v.lp
	m.mu.Unlock()
	if lp.l0 != lp.r0 || lp.l1 != lp.r1 {
		t.Fatalf("filter channels diverged while binaural: %+v", lp)
	}
}
