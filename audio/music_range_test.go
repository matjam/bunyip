package audio

import (
	"bytes"
	"math"
	"sync"
	"testing"
	"time"
)

func rangeMusic(t *testing.T, loop bool) (*Mixer, *Music) {
	t.Helper()
	m := NewMixer(1000)
	samples := []float32{0, 0.125, 0.25, 0.375, 0.5, 0.625, 0.75, 0.875}
	mu, err := m.OpenMusic(bytes.NewReader(encodeWAV16(PCM{Samples: samples, Channels: 1, Rate: 1000})), loop)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mu.Close)
	return m, mu
}

func readRange(t *testing.T, mu *Music, count int) []float32 {
	t.Helper()
	mu.wait(func() bool { return mu.count >= count*2 })
	out := make([]float32, count*2)
	if n := mu.Read(out); n != count {
		t.Fatalf("read %d want %d", n, count)
	}
	return out
}

func assertMusicFrame(t *testing.T, out []float32, i, frame int) {
	t.Helper()
	want := float32(frame) / 8
	if math.Abs(float64(out[i*2]-want)) > 0.0001 {
		t.Fatalf("output %d: %v want source frame %d (%v)", i, out[i*2], frame, want)
	}
}

func TestMusicLoopRangeAndSeek(t *testing.T) {
	_, mu := rangeMusic(t, true)
	if err := mu.SetLoopRange(2*time.Millisecond, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	a, b := mu.LoopRange()
	if a != 2*time.Millisecond || b != 5*time.Millisecond || !mu.Looping() {
		t.Fatalf("range %v,%v looping %v", a, b, mu.Looping())
	}
	out := readRange(t, mu, 19)
	for i := range 19 {
		assertMusicFrame(t, out, i, 2+i%3)
	}
	if err := mu.Seek(0.004); err != nil {
		t.Fatal(err)
	}
	out = readRange(t, mu, 5)
	for i, frame := range []int{4, 2, 3, 4, 2} {
		assertMusicFrame(t, out, i, frame)
	}
	if err := mu.Seek(0.007); err != nil {
		t.Fatal(err)
	}
	out = readRange(t, mu, 1)
	assertMusicFrame(t, out, 0, 2)
	if err := mu.SetLoopRange(0, 0); err != nil {
		t.Fatal(err)
	}
	out = readRange(t, mu, 10)
	for i := range 10 {
		assertMusicFrame(t, out, i, i%8)
	}
}

func TestMusicLoopRangeValidation(t *testing.T) {
	_, mu := rangeMusic(t, true)
	for _, r := range [][2]time.Duration{{-1, 2}, {1, 0}, {2, 2}, {time.Millisecond, 9 * time.Millisecond}, {time.Nanosecond, 2 * time.Nanosecond}} {
		if err := mu.SetLoopRange(r[0], r[1]); err == nil {
			t.Fatalf("accepted %v", r)
		}
	}
	if err := mu.Seek(math.NaN()); err == nil {
		t.Fatal("NaN seek accepted")
	}
	mu.Close()
	if err := mu.SetLoopRange(0, 0); err == nil {
		t.Fatal("closed range update accepted")
	}
}

func TestMusicLoopingKeepsUnplayedShortTrack(t *testing.T) {
	m, mu := rangeMusic(t, false)
	mu.wait(func() bool { return mu.ended }) // all eight frames decoded, none played
	v := m.PlayStream(mu, PlayOptions{Pan: -1})
	out := make([]float32, 4)
	m.mix(out)
	assertMusicFrame(t, out, 1, 1)
	mu.SetLooping(true)
	mu.wait(func() bool { return mu.count >= 16 })
	m.mix(out)
	assertMusicFrame(t, out, 0, 2)
	assertMusicFrame(t, out, 1, 3)
	if v.State() != PlaybackPlaying {
		t.Fatal(v.State())
	}
	mu.SetLooping(false)
	mu.wait(func() bool { return mu.ended })
	m.mix(out)
	assertMusicFrame(t, out, 0, 4)
	assertMusicFrame(t, out, 1, 5)
}

func TestMusicLoopingAfterEOFNeedsNewVoice(t *testing.T) {
	m, mu := rangeMusic(t, false)
	mu.wait(func() bool { return mu.ended })
	v := m.PlayStream(mu, PlayOptions{Pan: -1})
	m.mix(make([]float32, 32))
	if v.State() != PlaybackStopped {
		t.Fatal("voice did not end")
	}
	mu.SetLooping(true)
	mu.wait(func() bool { return mu.count >= 16 })
	if v.State() != PlaybackStopped {
		t.Fatal("ended voice revived")
	}
	m.PlayStream(mu, PlayOptions{Pan: -1})
	out := make([]float32, 4)
	m.mix(out)
	assertMusicFrame(t, out, 0, 0)
	assertMusicFrame(t, out, 1, 1)
}

func TestMusicOneFrameLoopRange(t *testing.T) {
	_, mu := rangeMusic(t, true)
	if err := mu.SetLoopRange(3*time.Millisecond, 4*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	out := readRange(t, mu, 20)
	for i := range 20 {
		assertMusicFrame(t, out, i, 3)
	}
}

func TestMusicLoopToggleAfterUnderrun(t *testing.T) {
	for _, frames := range []int{1, 2, 20, 511, 512, 513} {
		m := NewMixer(1000)
		// No worker: two real frames followed by deterministic underrun silence.
		mu := &Music{dec: &memoryDecoder{pcm: PCM{Rate: 1000, Channels: 2}}, rate: 1000,
			ring: []float32{0.25, 0.25, 0.5, 0.5}, count: 4, seek: -1, length: 1000}
		mu.cond = sync.NewCond(&mu.mu)
		m.PlayStream(mu, PlayOptions{})
		m.mix(make([]float32, frames*2))
		mu.SetLooping(true)
		if want := float64(min(frames, 2)); mu.playFrame != want {
			t.Fatalf("after %d output frames: restarted at %v, want %v actual source frames", frames, mu.playFrame, want)
		}
	}
}

func TestMusicLoopRangeFrameRoundTrip(t *testing.T) {
	mu := &Music{dec: &memoryDecoder{pcm: PCM{Rate: 44100, Channels: 2}}, length: 44100}
	mu.cond = sync.NewCond(&mu.mu)
	for frame := int64(1); frame < 1000; frame++ {
		a := time.Duration(math.Ceil(float64(frame) / 44100 * float64(time.Second)))
		b := time.Duration(math.Ceil(float64(frame+1) / 44100 * float64(time.Second)))
		if err := mu.SetLoopRange(a, b); err != nil {
			t.Fatal(err)
		}
		a, b = mu.LoopRange()
		if err := mu.SetLoopRange(a, b); err != nil {
			t.Fatal(err)
		}
		if mu.loopStart != frame || mu.loopEnd != frame+1 || mu.playFrame != float64(frame) {
			t.Fatalf("frame %d round trip: %d..%d play %v", frame, mu.loopStart, mu.loopEnd, mu.playFrame)
		}
	}
}

func TestMusicOldRevisionDoesNotConsumeNewSeek(t *testing.T) {
	_, mu := rangeMusic(t, false)
	old := mu.streamRevision()
	if err := mu.Seek(0.004); err != nil {
		t.Fatal(err)
	}
	mu.wait(func() bool { return mu.ended })
	out := make([]float32, 4)
	if n, real := mu.readSource(out, old); n != 2 || real != 0 {
		t.Fatalf("old revision returned %d frames, %d real", n, real)
	}
	if n, real := mu.readSource(out, mu.streamRevision()); n != 2 || real != 2 {
		t.Fatalf("new revision returned %d frames, %d real", n, real)
	}
	assertMusicFrame(t, out, 0, 4)
}
