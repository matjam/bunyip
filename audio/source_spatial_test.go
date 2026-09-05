package audio

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestVoiceEffectiveState(t *testing.T) {
	m := NewMixer(48000)
	v := m.PlayStream(&rampStream{count: 1000}, PlayOptions{Bus: m.Music()})
	if v.State() != PlaybackPlaying {
		t.Fatal(v.State())
	}
	for _, pause := range []func(bool){v.SetPaused, m.SetPaused, m.Music().SetPaused} {
		pause(true)
		if v.State() != PlaybackPaused || !v.Playing() {
			t.Fatal("effective pause not reported")
		}
		pause(false)
		if v.State() != PlaybackPlaying {
			t.Fatal(v.State())
		}
	}
	v.SetMute(true)
	if v.State() != PlaybackPlaying {
		t.Fatal("mute paused playback")
	}
	v.SetMute(false)
	m.mix(make([]float32, 16)) // establish an audible gain before the stop ramp
	v.Stop()
	if v.State() != PlaybackStopped || v.Playing() {
		t.Fatal("stopped voice active")
	}
	m.mix(make([]float32, 2)) // only one of the 48 stop-ramp frames
	if v.State() != PlaybackStopped {
		t.Fatal("stop ramp revived state")
	}
	ended := m.PlayStream(&rampStream{}, PlayOptions{})
	m.mix(make([]float32, 8))
	if ended.State() != PlaybackStopped {
		t.Fatal("EOF voice active")
	}
}

func TestPlaybackStateStrings(t *testing.T) {
	for s, want := range map[PlaybackState]string{PlaybackStopped: "stopped", PlaybackPlaying: "playing", PlaybackPaused: "paused", PlaybackState(99): "unknown"} {
		if s.String() != want {
			t.Fatalf("%d: %s", s, s)
		}
	}
}

func TestSourceControlsEnableAndRoundTrip(t *testing.T) {
	v := NewMixer(48000).newVoice(PlayOptions{})
	if err := v.SetDirection(lin.V3(0, 0, -4)); err != nil {
		t.Fatal(err)
	}
	if !v.positional || v.Direction() != lin.V3(0, 0, -1) {
		t.Fatal("direction did not enable and normalize")
	}
	v.positional = false
	c := Cone{InnerAngle: 1, OuterAngle: 2, OuterGain: 0.3}
	if err := v.SetCone(c); err != nil {
		t.Fatal(err)
	}
	if !v.positional || v.Cone() != c {
		t.Fatal("cone not retained")
	}
	v.SetRelativeToListener(true)
	if !v.RelativeToListener() {
		t.Fatal("relative mode not retained")
	}
	a := Attenuation{Model: AttenuationLinear, Rolloff: 2}
	if err := v.SetAttenuation(a); err != nil || v.Attenuation() != a {
		t.Fatal("attenuation not retained")
	}
	if err := v.SetDistanceRange(2, 20); err != nil {
		t.Fatal(err)
	}
	if a, b := v.DistanceRange(); a != 2 || b != 20 {
		t.Fatal(a, b)
	}
}

func TestSourceCone(t *testing.T) {
	c, err := (Cone{InnerAngle: math.Pi / 2, OuterAngle: math.Pi, OuterGain: 0.2}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ angle, want float32 }{{0, 1}, {math.Pi / 4, 1}, {3 * math.Pi / 8, 0.6}, {math.Pi / 2, 0.2}, {math.Pi, 0.2}} {
		toward := lin.V3(float32(math.Sin(float64(tc.angle))), 0, float32(math.Cos(float64(tc.angle))))
		if got := c.gain(lin.V3(0, 0, 1), toward); math.Abs(float64(got-tc.want)) > 1e-6 {
			t.Fatalf("angle %v: %v want %v", tc.angle, got, tc.want)
		}
	}
	zero, _ := (Cone{}).normalized()
	if got := zero.gain(lin.V3(0, 0, 1), lin.V3(0, 0, -1)); got != 1 {
		t.Fatal("default cone directional")
	}
}

func TestListenerRelativeSourceBasis(t *testing.T) {
	m := NewMixer(48000)
	l := Listener{Position: lin.V3(10, 20, 30), Forward: lin.V3(1, 0, 0), Up: lin.V3(0, 1, 0), Velocity: lin.V3(4, 5, 6)}
	m.SetListener(l)
	v := m.newVoice(PlayOptions{RelativeToListener: true, Position: lin.V3(2, 3, -4), Direction: lin.V3(0, 0, -1), Velocity: lin.V3(1, 0, 0)})
	p, vel, d := v.sourceSpace(l)
	if p != lin.V3(14, 23, 32) || vel != lin.V3(4, 5, 7) || d != lin.V3(1, 0, 0) {
		t.Fatalf("local transform %v %v %v", p, vel, d)
	}
	v.velocity = lin.Vec3{}
	p, vel, _ = v.sourceSpace(l)
	if got := l.doppler(p, vel, 1, 343); math.Abs(float64(got-1)) > 1e-6 {
		t.Fatalf("listener-relative stationary source Doppler=%v", got)
	}
}

func TestAttenuationDefaultsAndModels(t *testing.T) {
	l := Listener{Forward: lin.V3(0, 0, -1), Up: lin.V3(0, 1, 0)}
	a, _ := (Attenuation{}).normalized()
	for _, dist := range []float32{0, 0.5, 1, 2, 50, 99, 100, 101} {
		want, _ := l.attenuate(lin.V3(0, 0, -dist), 1, 100)
		if got := a.gain(dist, 1, 100); math.Abs(float64(got-want)) > 1e-7 {
			t.Fatalf("default changed at %v: %v want %v", dist, got, want)
		}
	}
	for _, model := range []AttenuationModel{AttenuationDefault, AttenuationLinear, AttenuationInverse, AttenuationExponential} {
		a, _ := (Attenuation{Model: model, Rolloff: 2}).normalized()
		if a.gain(1, 1, 10) != 1 || a.gain(10, 1, 10) != 0 || a.gain(5, 1, 10) > a.gain(2, 1, 10) {
			t.Fatalf("invalid distance model %v", model)
		}
	}
	if (Attenuation{Model: AttenuationNone}).gain(1000, 1, 10) != 1 {
		t.Fatal("None attenuates")
	}
}

func TestSpatialRejectsInvalidInputs(t *testing.T) {
	v := NewMixer(48000).newVoice(PlayOptions{})
	if v.SetCone(Cone{InnerAngle: 2, OuterAngle: 1}) == nil || v.SetCone(Cone{OuterAngle: float32(math.NaN())}) == nil || v.SetDirection(lin.Vec3{}) == nil || v.SetAttenuation(Attenuation{Rolloff: -1}) == nil || v.SetDistanceRange(2, 1) == nil {
		t.Fatal("invalid spatial value accepted")
	}
	v.SetPitch(float32(math.NaN()))
	if v.pitch != 1 {
		t.Fatal("NaN pitch accepted")
	}
}
