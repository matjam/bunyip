package audio

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// dc is a looping sound that holds a steady level, so gains can be read
// straight off the output.
func dc(m *Mixer) *Sound {
	snd, _ := m.NewSound(PCM{Samples: []float32{1, 1, 1, 1}, Channels: 1, Rate: m.Rate()})
	return snd
}

func TestBusVolumeRamps(t *testing.T) {
	m := NewMixer(1000)
	if m.Bus("music") != m.Music() || m.Bus("effects") != m.Effects() || m.Bus("dialogue") != m.Dialogue() {
		t.Fatal("default buses not found by name")
	}
	if m.Bus("nope") != nil {
		t.Fatal("unknown bus should be nil")
	}
	ui := m.NewBus("ui")
	if ui.Name() != "ui" || m.Bus("ui") != ui || m.NewBus("ui") != ui {
		t.Fatal("NewBus did not register the bus")
	}
	m.Play(dc(m), PlayOptions{Loop: true, Bus: ui})
	out := make([]float32, 20)
	m.mix(out)
	centre := float32(math.Sqrt(0.5))
	if math.Abs(float64(out[0]-centre)) > 1e-3 {
		t.Fatalf("unity bus gave %v, want %v", out[0], centre)
	}
	ui.SetVolume(0.5)
	if ui.Volume() != 0.5 {
		t.Fatal("Volume did not read back")
	}
	m.mix(out) // the ramp block: falling, never jumping
	for i := 2; i < len(out); i += 2 {
		if out[i] >= out[i-2] {
			t.Fatalf("frame %d did not fall: %v then %v", i/2, out[i-2], out[i])
		}
	}
	m.mix(out)
	for i := 0; i < len(out); i += 2 {
		if math.Abs(float64(out[i]-centre*0.5)) > 1e-3 {
			t.Fatalf("frame %d = %v, want %v", i/2, out[i], centre*0.5)
		}
	}
}

func TestBusAndMixerPause(t *testing.T) {
	m := NewMixer(1000)
	v := m.Play(dc(m), PlayOptions{Loop: true, Bus: m.Effects()})
	other := m.Play(dc(m), PlayOptions{Loop: true, Bus: m.Music()})
	out := make([]float32, 20)
	m.Effects().SetPaused(true)
	if !m.Effects().Paused() {
		t.Fatal("Paused did not read back")
	}
	m.mix(out)
	if l, r := rms(out); math.Abs(l-math.Sqrt(0.5)) > 1e-3 || l != r {
		t.Fatalf("only the music bus should play: %.3f/%.3f", l, r)
	}
	if !v.Playing() || v.Position() != 0 {
		t.Fatal("paused voice moved or ended")
	}
	m.SetPaused(true)
	if !m.Paused() {
		t.Fatal("mixer Paused did not read back")
	}
	m.mix(out) // the pause block fades out rather than cutting
	for i := 2; i < len(out); i += 2 {
		if out[i] >= out[i-2] {
			t.Fatalf("pause did not fade: frame %d %v then %v", i/2, out[i-2], out[i])
		}
	}
	if out[len(out)-2] > 0.1 {
		t.Fatalf("pause fade ended at %v, want near silence", out[len(out)-2])
	}
	m.mix(out)
	if l, r := rms(out); l+r != 0 {
		t.Fatalf("paused mixer produced %.3f/%.3f", l, r)
	}
	if !other.Playing() {
		t.Fatal("voice ended while the mixer was paused")
	}
	m.SetPaused(false)
	m.Effects().SetPaused(false)
	m.mix(out) // ramps back in from silence
	m.mix(out)
	if l, _ := rms(out); l < 1 { // two unity voices, clamped
		t.Fatalf("resumed output %.3f, want both voices", l)
	}
}

func TestPositionAndSeek(t *testing.T) {
	m := NewMixer(44100)
	tone, _ := m.NewSound(Sine(440, 1, 44100))
	if d := tone.Duration(); math.Abs(d-1) > 1e-6 {
		t.Fatalf("Duration %v, want 1", d)
	}
	v := m.Play(tone, PlayOptions{})
	m.mix(make([]float32, 1024*2))
	if p := v.Position(); math.Abs(p-1024.0/44100) > 1e-9 {
		t.Fatalf("Position %v after 1024 frames", p)
	}
	if err := v.Seek(0.9); err != nil {
		t.Fatal(err)
	}
	if p := v.Position(); math.Abs(p-0.9) > 1e-6 {
		t.Fatalf("Position %v after Seek(0.9)", p)
	}
	out := mixUntilDone(m, v, 1000)
	if frames := len(out) / 2; frames > 4410+1024 {
		t.Fatalf("played %d frames after seeking to 0.9 s, want about 4410", frames)
	}
	v = m.Play(tone, PlayOptions{})
	v.Seek(-5)
	v.Seek(99)
	m.mix(make([]float32, 64))
	if v.Playing() {
		t.Fatal("seeking past the end should end the voice")
	}
}

func TestOnDone(t *testing.T) {
	m := NewMixer(44100)
	tone, _ := m.NewSound(Sine(440, 0.01, 44100))
	var chained *Voice
	v := m.Play(tone, PlayOptions{})
	v.OnDone(func() { chained = m.Play(tone, PlayOptions{}) }) // must not deadlock
	m.mix(make([]float32, 2048))
	if v.Playing() || chained == nil || !chained.Playing() || m.Playing() != 1 {
		t.Fatal("OnDone did not run when the voice played out")
	}
	ran := false
	v.OnDone(func() { ran = true })
	if !ran {
		t.Fatal("OnDone on an ended voice should run at once")
	}
	m.SetMaxVoices(1)
	stolen := false
	chained.OnDone(func() { stolen = true })
	m.Play(tone, PlayOptions{Priority: 1})
	if !stolen {
		t.Fatal("OnDone did not run when the voice lost its slot")
	}
	stopped := false
	v = m.Play(tone, PlayOptions{Priority: 2})
	v.OnDone(func() { stopped = true })
	m.StopAll()
	if !stopped {
		t.Fatal("OnDone did not run on StopAll")
	}
}

type silence struct{}

func (silence) Read(out []float32) int { return len(out) / 2 }

func TestMusicSeekAndDuration(t *testing.T) {
	const rate = 44100
	m := NewMixer(rate)
	music, err := m.OpenMusic(bytes.NewReader(encodeWAV16(Sine(440, 0.5, rate))), false)
	if err != nil {
		t.Fatal(err)
	}
	defer music.Close()
	if d := music.Duration(); math.Abs(d-0.5) > 1e-6 {
		t.Fatalf("Duration %v, want 0.5", d)
	}
	ended := func() { music.wait(func() bool { return false }) }
	ended() // the whole track is buffered
	if err := music.Seek(0.4); err != nil {
		t.Fatal(err)
	}
	ended()
	v := m.PlayStream(music, PlayOptions{})
	out := mixUntilDone(m, v, 1000)
	if frames := len(out) / 2; math.Abs(float64(frames-rate/10)) > 1024 {
		t.Fatalf("played %d frames from 0.4 s of 0.5, want about %d", frames, rate/10)
	}
	if music.Err() != nil {
		t.Fatal(music.Err())
	}
	// Seeking through the voice keeps Position in step.
	if err := music.Seek(0); err != nil {
		t.Fatal(err)
	}
	ended()
	v = m.PlayStream(music, PlayOptions{})
	m.mix(make([]float32, 1024))
	if err := v.Seek(0.45); err != nil {
		t.Fatal(err)
	}
	if p := v.Position(); math.Abs(p-0.45) > 1e-6 {
		t.Fatalf("Position %v after Seek(0.45)", p)
	}
	ended()
	out = mixUntilDone(m, v, 1000)
	if frames := len(out) / 2; math.Abs(float64(frames-rate/20)) > 1024 {
		t.Fatalf("played %d frames from 0.45 s of 0.5, want about %d", frames, rate/20)
	}
	if err := v.Seek(0); !errors.Is(err, nil) {
		t.Fatalf("Seek on an ended stream voice: %v", err)
	}
	plain := m.PlayStream(silence{}, PlayOptions{})
	m.mix(make([]float32, 200))
	if p := plain.Position(); math.Abs(p-100.0/rate) > 1e-9 {
		t.Fatalf("stream Position %v after 100 frames", p)
	}
	if err := plain.Seek(1); !errors.Is(err, ErrNotSeekable) {
		t.Fatalf("Seek on a plain stream: %v, want ErrNotSeekable", err)
	}
}

func TestDecodersTakeBytes(t *testing.T) {
	if _, err := DecodeOGG([]byte("OggS not really")); err == nil {
		t.Fatal("garbage ogg decoded")
	}
	if _, err := DecodeMP3([]byte{0xFF, 0xFB, 0, 0}); err == nil {
		t.Fatal("garbage mp3 decoded")
	}
}
