package audio

import (
	"sync"
	"testing"
)

// blockingStream stands in for music, a tracker player or game code: its
// Read takes a lock of its own, which the game goroutine also takes. If
// the mixer held its lock across Read, the two would serialise; the test
// checks they do not deadlock and that no state is shared unguarded.
type blockingStream struct {
	mu   sync.Mutex
	n    int64
	seek float64
}

func (s *blockingStream) Read(out []float32) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range out {
		out[i] = float32(s.n%64)/64 - 0.5
		s.n++
	}
	return len(out) / 2
}

func (s *blockingStream) Seek(seconds float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seek = seconds
	return nil
}

func (s *blockingStream) touch() {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}

// TestConcurrentSetters runs the game loop's setters against a mixing
// thread. Run it with -race: the mixer reads streams with no lock held,
// so every field a setter touches has to be copied out under the lock
// before mixing starts and written back under it afterwards.
func TestConcurrentSetters(t *testing.T) {
	m := NewMixer(48000)
	snd, err := m.NewSound(Sine(440, 1, 48000))
	if err != nil {
		t.Fatal(err)
	}
	bus := m.NewBus("scene")
	bus.SetReverb(ReverbSettings{Wet: 0.3})
	stream := &blockingStream{}
	sv := m.PlayStream(stream, PlayOptions{Bus: bus, Reverb: 0.2})
	voices := make([]*Voice, 0, 8)
	for i := range 8 {
		v := m.Play(snd, PlayOptions{Loop: true, Bus: bus, Occlusion: 0.5, LowPass: 800, Positional: i%2 == 0})
		v.OnDone(func() {})
		voices = append(voices, v)
	}

	const blocks = 300
	out := make([]float32, 512*2)
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range blocks {
			m.mix(out)
		}
		close(stop)
	}()

	setter := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				fn(i)
			}
		}()
	}

	setter(func(i int) {
		v := voices[i%len(voices)]
		v.SetVolume(float32(i%10) / 10)
		v.SetPan(float32(i%3) - 1)
		v.SetPitch(1 + float32(i%4)/8)
		v.SetOcclusion(float32(i%5) / 5)
		v.SetLowPass(float32(200 + i%2000))
		v.SetMute(i%7 == 0)
		v.SetSolo(i%11 == 0)
		v.SetReverb(float32(i%4) / 4)
	})
	setter(func(i int) {
		v := voices[i%len(voices)]
		_ = v.Position()
		_ = v.Playing()
		_ = v.Muted()
		_ = v.Soloed()
		_ = v.Occlusion()
		_ = m.Playing()
		_ = m.Reverb()
		_ = m.Listener()
	})
	setter(func(i int) {
		m.SetMasterVolume(float32(i%8) / 8)
		m.SetPaused(i%13 == 0)
		m.SetDoppler(float32(i % 2))
		m.SetListener(Listener{})
		m.SetReverb(ReverbSettings{Wet: float32(i%3) / 3})
		m.SetReverbZones([]ReverbZone{{Radius: 10, Settings: ReverbSettings{Wet: 0.5}}})
		bus.SetVolume(float32(i%6) / 6)
		bus.SetMute(i%9 == 0)
		bus.SetPaused(i%17 == 0)
		bus.SetReverb(ReverbSettings{Wet: float32(i%5) / 5})
	})
	setter(func(i int) {
		if i%3 == 0 {
			_ = sv.Seek(float64(i % 4))
		}
		stream.touch()
		v := m.Play(snd, PlayOptions{Bus: bus, FadeIn: 0.01})
		v.FadeOut(0.01)
	})

	wg.Wait()
	m.StopAll()
	if m.Playing() != 0 {
		t.Errorf("StopAll left %d voices", m.Playing())
	}
}

// TestOnDoneRunsOnce checks that a voice finishing inside a block reports
// exactly one OnDone, and that it lands after the block, not during it.
func TestOnDoneRunsOnce(t *testing.T) {
	m := NewMixer(1000)
	snd, err := m.NewSound(PCM{Samples: make([]float32, 8), Channels: 1, Rate: 1000})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	v := m.Play(snd, PlayOptions{})
	v.OnDone(func() { calls++ })
	out := make([]float32, 40) // 20 frames, longer than the 8 frame sound
	m.mix(out)
	if calls != 1 {
		t.Fatalf("OnDone ran %d times, want 1", calls)
	}
	m.mix(out)
	if calls != 1 {
		t.Fatalf("OnDone ran again: %d", calls)
	}
	if m.Playing() != 0 {
		t.Errorf("finished voice still listed")
	}
}
