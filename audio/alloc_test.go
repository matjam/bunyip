package audio

import (
	"runtime"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// mixMallocs mixes one block and reports how many heap allocations it
// made. The mixer runs on the audio device's thread, where an allocation
// can wait on the garbage collector, so a block must make none.
func mixMallocs(m *Mixer, out []float32) uint64 {
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	m.mix(out)
	runtime.ReadMemStats(&b)
	return b.Mallocs - a.Mallocs
}

// TestBinauralVoiceAllocatesOffTheAudioThread starts positional voices
// with binaural rendering on, turns binaural on over voices already
// playing, and makes a voice positional while binaural is on. The
// head-model state is made by the call on the game's goroutine each time,
// so no block allocates.
func TestBinauralVoiceAllocatesOffTheAudioThread(t *testing.T) {
	m := NewMixer(benchRate)
	snd, err := m.NewSound(Sine(440, 1, benchRate))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, benchBlock*2)
	m.mix(out)

	// Playing before binaural is turned on, then turning it on.
	early := m.Play(snd, PlayOptions{Positional: true, Position: lin.Vec3{X: 2}})
	m.mix(out)
	m.SetSpatial(SpatialSettings{Binaural: true})
	if n := mixMallocs(m, out); n != 0 {
		t.Errorf("first binaural block of a playing voice made %d allocations", n)
	}
	for i := range 5 {
		m.Play(snd, PlayOptions{Positional: true, Position: lin.Vec3{X: float32(i), Z: 1}})
		if n := mixMallocs(m, out); n != 0 {
			t.Errorf("first block of binaural voice %d made %d allocations", i, n)
		}
	}
	plain := m.Play(snd, PlayOptions{})
	m.mix(out)
	plain.SetPosition(lin.Vec3{X: -3})
	if n := mixMallocs(m, out); n != 0 {
		t.Errorf("first block after SetPosition made %d allocations", n)
	}
	for _, set := range []func(v *Voice){
		func(v *Voice) { v.SetRelativeToListener(true) },
		func(v *Voice) { v.SetDirection(lin.Vec3{X: 1}) },
		func(v *Voice) { v.SetCone(Cone{InnerAngle: 1, OuterAngle: 2, OuterGain: 0.5}) },
		func(v *Voice) { v.SetAttenuation(Attenuation{Model: AttenuationLinear}) },
		func(v *Voice) { v.SetDistanceRange(1, 50) },
	} {
		v := m.Play(snd, PlayOptions{})
		m.mix(out)
		set(v)
		if n := mixMallocs(m, out); n != 0 {
			t.Errorf("first block after a setter made the voice positional made %d allocations", n)
		}
	}
	m.mu.Lock()
	missing := early.bin == nil || plain.bin == nil
	m.mu.Unlock()
	if missing {
		t.Fatal("binaural voices have no head-model state")
	}
}

// TestOnDoneAllocatesNothing ends a voice with an OnDone callback in
// many blocks. Handing the callbacks over reuses the same slices, so no
// block allocates once the first has run.
func TestOnDoneAllocatesNothing(t *testing.T) {
	m := NewMixer(benchRate)
	pcm := Sine(440, 0.001, benchRate) // 48 frames, done within one block
	snd, err := m.NewSound(pcm)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, benchBlock*2)
	calls := 0
	done := func() { calls++ }
	for i := range 20 {
		v := m.Play(snd, PlayOptions{})
		v.OnDone(done)
		n := mixMallocs(m, out)
		if i >= 2 && n != 0 {
			t.Errorf("block %d, where a voice with OnDone ended, made %d allocations", i, n)
		}
	}
	if calls != 20 {
		t.Fatalf("OnDone ran %d times, want 20", calls)
	}
}
