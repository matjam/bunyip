package audio

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// tail mixes blocks after the voice has ended and sums what is left: the
// energy a reverb adds once the source is silent.
func tail(m *Mixer, v *Voice, blocks int) float64 {
	block := make([]float32, 4096)
	for v.Playing() {
		m.mix(block)
	}
	var sum float64
	for range blocks {
		m.mix(block)
		l, r := rms(block)
		sum += l + r
	}
	return sum
}

func TestBusReverb(t *testing.T) {
	m := NewMixer(44100)
	click, _ := m.NewSound(Sine(1000, 0.02, 44100))
	cave := m.NewBus("cave")
	cave.SetReverb(ReverbSettings{RoomSize: 0.8, Wet: 1})
	// A voice on another bus with the same send stays dry: the mixer's own
	// reverb is off.
	v := m.Play(click, PlayOptions{Reverb: 1, Bus: m.Effects()})
	if got := tail(m, v, 20); got > 1e-3 {
		t.Fatalf("dry bus rang: %.4f", got)
	}
	// A voice on the bus rings on after it ends.
	v = m.Play(click, PlayOptions{Reverb: 1, Bus: cave})
	if got := tail(m, v, 20); got < 0.01 {
		t.Fatalf("no tail from the bus reverb: %.4f", got)
	}
	cave.SetReverb(ReverbSettings{})
	v = m.Play(click, PlayOptions{Reverb: 1, Bus: cave})
	if got := tail(m, v, 20); got > 0 {
		t.Fatalf("bus reverb still audible after being removed: %.4f", got)
	}
}

func TestReverbZones(t *testing.T) {
	m := NewMixer(44100)
	hall := ReverbSettings{RoomSize: 0.9, Damping: 0.2, Width: 1, Wet: 0.8}
	m.SetReverbZones([]ReverbZone{{Center: lin.V3(100, 0, 0), Radius: 10, Fade: 5, Settings: hall}})
	// Outside every zone, with no shared reverb, there is none.
	m.SetListener(Listener{Position: lin.V3(0, 0, 0)})
	if r := m.Reverb(); r.Wet != 0 {
		t.Fatalf("reverb outside the zone: %+v", r)
	}
	// At the centre the zone is at full strength.
	m.SetListener(Listener{Position: lin.V3(100, 0, 0)})
	if r := m.Reverb(); r != hall {
		t.Fatalf("reverb at the centre %+v, want %+v", r, hall)
	}
	// Half way through the fade, half the level, the same room.
	m.SetListener(Listener{Position: lin.V3(107.5, 0, 0)})
	if r := m.Reverb(); math.Abs(float64(r.Wet-0.4)) > 1e-3 || r.RoomSize != hall.RoomSize {
		t.Fatalf("reverb half way in: %+v", r)
	}
	// Outside the zone a click leaves no tail; inside it rings.
	click, _ := m.NewSound(Sine(1000, 0.02, 44100))
	m.SetListener(Listener{Position: lin.V3(0, 0, 0)})
	if got := tail(m, m.Play(click, PlayOptions{Reverb: 1}), 20); got > 1e-3 {
		t.Fatalf("tail outside the zone: %.4f", got)
	}
	m.SetListener(Listener{Position: lin.V3(100, 0, 0)})
	if got := tail(m, m.Play(click, PlayOptions{Reverb: 1}), 20); got < 0.01 {
		t.Fatalf("no tail inside the zone: %.4f", got)
	}
	// A shared reverb blends toward the zone by the listener's depth.
	m.SetReverb(ReverbSettings{RoomSize: 0.3, Wet: 0.2})
	m.SetListener(Listener{Position: lin.V3(107.5, 0, 0)})
	if r := m.Reverb(); math.Abs(float64(r.Wet-0.5)) > 1e-3 || math.Abs(float64(r.RoomSize-0.6)) > 1e-3 {
		t.Fatalf("blend of base and zone: %+v", r)
	}
	m.SetReverbZones(nil)
	if r := m.Reverb(); r.Wet != 0.2 {
		t.Fatalf("base reverb after clearing zones: %+v", r)
	}
}

func TestOcclusion(t *testing.T) {
	m := NewMixer(44100)
	high, _ := m.NewSound(Sine(6000, 1, 44100))
	low, _ := m.NewSound(Sine(100, 1, 44100))
	block := make([]float32, 4096)
	level := func(s *Sound, occlusion float32) float64 {
		m.StopAll()
		m.Play(s, PlayOptions{Occlusion: occlusion})
		m.mix(block)
		m.mix(block)
		l, _ := rms(block)
		return l
	}
	clearHigh, blockedHigh := level(high, 0), level(high, 1)
	clearLow, blockedLow := level(low, 0), level(low, 1)
	if blockedLow > clearLow*0.2 {
		t.Fatalf("occlusion 1 not quieter: %.4f vs %.4f", blockedLow, clearLow)
	}
	// Duller: the highs lose far more than the lows do.
	if blockedHigh/clearHigh > 0.1*blockedLow/clearLow {
		t.Fatalf("occlusion 1 not duller: high kept %.4f of %.4f, low kept %.4f of %.4f", blockedHigh, clearHigh, blockedLow, clearLow)
	}
	half := level(low, 0.5)
	if half >= clearLow || half <= blockedLow {
		t.Fatalf("occlusion 0.5 gave %.4f, want between %.4f and %.4f", half, blockedLow, clearLow)
	}
	// Set while playing, and read back.
	m.StopAll()
	v := m.Play(low, PlayOptions{Loop: true})
	m.mix(block)
	m.mix(block)
	before, _ := rms(block)
	v.SetOcclusion(2) // clamped to 1
	if v.Occlusion() != 1 {
		t.Fatalf("Occlusion %v, want 1", v.Occlusion())
	}
	m.mix(block) // ramp block
	m.mix(block)
	if after, _ := rms(block); after > before*0.2 {
		t.Fatalf("SetOcclusion did not attenuate: %.4f then %.4f", before, after)
	}
	v.SetOcclusion(0)
	m.mix(block)
	m.mix(block)
	if after, _ := rms(block); math.Abs(after-before) > 0.01 {
		t.Fatalf("clearing occlusion gave %.4f, want %.4f", after, before)
	}
}

func TestMuteAndSolo(t *testing.T) {
	m := NewMixer(1000)
	a := m.Play(dc(m), PlayOptions{Loop: true, Bus: m.Effects()})
	b := m.Play(dc(m), PlayOptions{Loop: true, Bus: m.Music()})
	out := make([]float32, 20)
	settle := func() float64 {
		m.mix(out) // the ramp block
		m.mix(out)
		l, _ := rms(out)
		return l
	}
	unit := math.Sqrt(0.5)
	if l := settle(); math.Abs(l-1) > 1e-3 { // two unity voices, clamped
		t.Fatalf("both voices: %.3f", l)
	}
	a.SetMute(true)
	if !a.Muted() {
		t.Fatal("Muted did not read back")
	}
	m.mix(out) // the ramp: falling, never jumping
	for i := 2; i < len(out); i += 2 {
		if out[i] > out[i-2] {
			t.Fatalf("mute jumped at frame %d: %v then %v", i/2, out[i-2], out[i])
		}
	}
	if l := settle(); math.Abs(l-unit) > 1e-3 {
		t.Fatalf("one voice muted: %.3f, want %.3f", l, unit)
	}
	if a.Position() == 0 {
		t.Fatal("muted voice stopped advancing")
	}
	a.SetMute(false)
	b.SetSolo(true)
	if !b.Soloed() || a.Soloed() {
		t.Fatal("Soloed did not read back")
	}
	if l := settle(); math.Abs(l-unit) > 1e-3 {
		t.Fatalf("one voice soloed: %.3f, want %.3f", l, unit)
	}
	b.SetSolo(false)
	if l := settle(); math.Abs(l-1) > 1e-3 {
		t.Fatalf("solo cleared: %.3f", l)
	}
	// Buses: muting one, then soloing the other, then a voice on no bus.
	m.Effects().SetMute(true)
	if !m.Effects().Muted() {
		t.Fatal("bus Muted did not read back")
	}
	if l := settle(); math.Abs(l-unit) > 1e-3 {
		t.Fatalf("effects muted: %.3f, want %.3f", l, unit)
	}
	m.Effects().SetMute(false)
	m.Music().SetSolo(true)
	if !m.Music().Soloed() {
		t.Fatal("bus Soloed did not read back")
	}
	free := m.Play(dc(m), PlayOptions{Loop: true})
	if l := settle(); math.Abs(l-unit) > 1e-3 {
		t.Fatalf("music soloed with a free voice: %.3f, want %.3f", l, unit)
	}
	m.Music().SetSolo(false)
	free.Stop()
	if l := settle(); math.Abs(l-1) > 1e-3 {
		t.Fatalf("bus solo cleared: %.3f", l)
	}
}

func TestDoppler(t *testing.T) {
	const rate = 44100
	m := NewMixer(rate)
	tone, _ := m.NewSound(Sine(440, 1, rate))
	frames := func(vel lin.Vec3, listener lin.Vec3) int {
		m.SetListener(Listener{Position: lin.V3(0, 0, 0), Velocity: listener})
		v := m.Play(tone, PlayOptions{Positional: true, Position: lin.V3(0, 0, -10), Velocity: vel, MaxDistance: 1000})
		return len(mixUntilDone(m, v, 1000)) / 2
	}
	// Off by default: the sound plays at its own length however it moves.
	if n := frames(lin.V3(0, 0, 170), lin.Vec3{}); math.Abs(float64(n-rate)) > 1024 {
		t.Fatalf("Doppler off: %d frames, want %d", n, rate)
	}
	m.SetDoppler(1)
	m.SetSpeedOfSound(340)
	// A source closing at half the speed of sound plays an octave up, so
	// in half the time; one receding at half plays at two thirds.
	if n := frames(lin.V3(0, 0, 170), lin.Vec3{}); math.Abs(float64(n-rate/2)) > 1024 {
		t.Fatalf("closing source: %d frames, want %d", n, rate/2)
	}
	if n := frames(lin.V3(0, 0, -170), lin.Vec3{}); math.Abs(float64(n-rate*3/2)) > 1024 {
		t.Fatalf("receding source: %d frames, want %d", n, rate*3/2)
	}
	// The listener moving toward a still source at half the speed of sound
	// hears it a fifth up: 1.5 times the rate.
	if n := frames(lin.Vec3{}, lin.V3(0, 0, -170)); math.Abs(float64(n-rate*2/3)) > 1024 {
		t.Fatalf("closing listener: %d frames, want %d", n, rate*2/3)
	}
	// Sideways motion changes nothing.
	if n := frames(lin.V3(170, 0, 0), lin.Vec3{}); math.Abs(float64(n-rate)) > 1024 {
		t.Fatalf("sideways source: %d frames, want %d", n, rate)
	}
	// A source outrunning sound is clamped, not silenced or reversed.
	m.SetListener(Listener{})
	v := m.Play(tone, PlayOptions{Positional: true, Position: lin.V3(0, 0, -10), Velocity: lin.V3(0, 0, -5000), MaxDistance: 1000})
	if n := len(mixUntilDone(m, v, 2000)) / 2; n < rate || n > rate*20 {
		t.Fatalf("supersonic source: %d frames", n)
	}
	// SetVelocity changes a playing voice, and the factor scales the shift.
	m.SetDoppler(0.5)
	v = m.Play(tone, PlayOptions{Positional: true, Position: lin.V3(0, 0, -10), MaxDistance: 1000})
	v.SetVelocity(lin.V3(0, 0, 170)) // half factor: as if closing at a quarter, 4/3 the pitch
	if n := len(mixUntilDone(m, v, 1000)) / 2; math.Abs(float64(n-rate*3/4)) > 1024 {
		t.Fatalf("half factor: %d frames, want %d", n, rate*3/4)
	}
}

func TestPauseFades(t *testing.T) {
	m := NewMixer(1000)
	v := m.Play(dc(m), PlayOptions{Loop: true})
	out := make([]float32, 20)
	m.mix(out)
	m.mix(out)
	v.SetPaused(true)
	m.mix(out)
	for i := 2; i < len(out); i += 2 {
		if out[i] >= out[i-2] {
			t.Fatalf("voice pause did not fade at frame %d: %v then %v", i/2, out[i-2], out[i])
		}
	}
	pos := v.Position()
	m.mix(out)
	if l, r := rms(out); l+r != 0 || v.Position() != pos {
		t.Fatalf("held voice produced %.3f/%.3f and moved from %v to %v", l, r, pos, v.Position())
	}
	v.SetPaused(false)
	m.mix(out)
	for i := 2; i < len(out); i += 2 {
		if out[i] <= out[i-2] {
			t.Fatalf("resume did not ramp at frame %d: %v then %v", i/2, out[i-2], out[i])
		}
	}
}
