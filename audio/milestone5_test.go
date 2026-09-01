package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// encodeWAV16 writes PCM as a 16-bit WAV file.
func encodeWAV16(p PCM) []byte {
	var buf bytes.Buffer
	dataLen := len(p.Samples) * 2
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVEfmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(p.Channels))
	binary.Write(&buf, binary.LittleEndian, uint32(p.Rate))
	binary.Write(&buf, binary.LittleEndian, uint32(p.Rate*p.Channels*2))
	binary.Write(&buf, binary.LittleEndian, uint16(p.Channels*2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	for _, s := range p.Samples {
		binary.Write(&buf, binary.LittleEndian, int16(max(-1, min(1, s))*32767))
	}
	return buf.Bytes()
}

// rms of interleaved stereo samples per channel.
func rms(buf []float32) (l, r float64) {
	n := len(buf) / 2
	if n == 0 {
		return 0, 0
	}
	for i := range n {
		l += float64(buf[i*2] * buf[i*2])
		r += float64(buf[i*2+1] * buf[i*2+1])
	}
	return math.Sqrt(l / float64(n)), math.Sqrt(r / float64(n))
}

// buffered waits until the music holds seconds of audio, or has ended.
// Tests mix far faster than real time, so they let the decoder get ahead
// the way a real device's pace would.
func buffered(mu *Music, seconds float64) {
	mu.wait(func() bool { return float64(mu.count/2) >= seconds*float64(len(mu.ring)/4) })
}

// mixUntilDone runs the mixer in blocks until the voice ends, returning
// everything it produced.
func mixUntilDone(m *Mixer, v *Voice, maxBlocks int) []float32 {
	var all []float32
	block := make([]float32, 512*2)
	for i := 0; i < maxBlocks && v.Playing(); i++ {
		m.Mix(block)
		all = append(all, block...)
	}
	return all
}

func TestMusicStreamsWAV(t *testing.T) {
	const rate = 48000
	m := NewMixer(rate)
	// A 22050 Hz mono file exercises channel and rate conversion.
	src := Sine(440, 0.5, 22050)
	music, err := m.OpenMusic(bytes.NewReader(encodeWAV16(src)), false)
	if err != nil {
		t.Fatal(err)
	}
	defer music.Close()
	buffered(music, 1) // the whole half second
	v := m.PlayStream(music, PlayOptions{})
	out := mixUntilDone(m, v, 1000)
	frames := len(out) / 2
	// The voice runs until the stream ends: half a second at the mixer rate,
	// give or take a block.
	if want := rate / 2; math.Abs(float64(frames-want)) > 1024 {
		t.Fatalf("streamed %d frames, want about %d", frames, want)
	}
	// A full-scale sine is 0.707 RMS, and centre pan puts 0.707 of it in
	// each ear.
	l, r := rms(out[:frames*2-2048])
	if l < 0.45 || l > 0.55 || math.Abs(l-r) > 0.01 {
		t.Fatalf("rms %.3f/%.3f, want about 0.5 in both channels", l, r)
	}
	if music.Err() != nil {
		t.Fatal(music.Err())
	}
}

func TestMusicLoops(t *testing.T) {
	m := NewMixer(44100)
	src := Sine(440, 0.1, 44100)
	music, err := m.OpenMusic(bytes.NewReader(encodeWAV16(src)), true)
	if err != nil {
		t.Fatal(err)
	}
	buffered(music, 1.9)
	v := m.PlayStream(music, PlayOptions{})
	out := mixUntilDone(m, v, 150) // 1.7 seconds: far past one pass, within the buffer
	if !v.Playing() {
		t.Fatal("looping music stopped")
	}
	if l, _ := rms(out); l < 0.4 {
		t.Fatalf("looping music rms %.3f, want a steady tone", l)
	}
	music.Close()
	m.Mix(make([]float32, 1024))
	if v.Playing() {
		t.Fatal("voice survived Close")
	}
}

// TestMusicFiles streams any Ogg or MP3 files in BUNYIP_MUSIC_DIR and
// checks they decode to something audible.
func TestMusicFiles(t *testing.T) {
	dir := os.Getenv("BUNYIP_MUSIC_DIR")
	if dir == "" {
		t.Skip("BUNYIP_MUSIC_DIR not set")
	}
	var files []string
	for _, pat := range []string{"*.ogg", "*.mp3"} {
		got, _ := filepath.Glob(filepath.Join(dir, pat))
		files = append(files, got...)
	}
	if len(files) == 0 {
		t.Skip("no ogg or mp3 files in", dir)
	}
	for _, name := range files {
		t.Run(filepath.Base(name), func(t *testing.T) {
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			m := NewMixer(48000)
			music, err := m.OpenMusic(f, false)
			if err != nil {
				t.Fatal(err)
			}
			defer music.Close()
			buffered(music, 1.9)
			v := m.PlayStream(music, PlayOptions{})
			out := mixUntilDone(m, v, 150) // 1.6 seconds, within the buffer
			if music.Err() != nil {
				t.Fatal(music.Err())
			}
			if l, r := rms(out); l+r < 0.01 {
				t.Fatalf("rms %.4f/%.4f: silent", l, r)
			}
		})
	}
}

func TestPositional(t *testing.T) {
	m := NewMixer(44100)
	tone, _ := m.NewSound(Sine(440, 1, 44100))
	block := make([]float32, 1024)
	at := func(p lin.Vec3) (float64, float64) {
		m.StopAll()
		m.Play(tone, PlayOptions{Positional: true, Position: p, MinDistance: 1, MaxDistance: 100})
		m.Mix(block) // ramp-in block
		m.Mix(block)
		return rms(block)
	}
	l, r := at(lin.Vec3{X: 5})
	if r <= l*1.5 {
		t.Fatalf("source on the right: l=%.3f r=%.3f", l, r)
	}
	l, r = at(lin.Vec3{X: -5})
	if l <= r*1.5 {
		t.Fatalf("source on the left: l=%.3f r=%.3f", l, r)
	}
	nl, nr := at(lin.Vec3{Z: -1})
	fl, fr := at(lin.Vec3{Z: -20})
	if fl+fr >= (nl+nr)*0.5 {
		t.Fatalf("far source not quieter: near %.3f far %.3f", nl+nr, fl+fr)
	}
	if l, r := at(lin.Vec3{Z: -200}); l+r > 1e-4 {
		t.Fatalf("source beyond MaxDistance audible: %.4f", l+r)
	}
	// The 2D listener: +x on screen is the right ear.
	m.SetListener2D(100, 100)
	l, r = at(lin.Vec3{X: 110, Y: 100})
	if r <= l*1.5 {
		t.Fatalf("2D source to the right: l=%.3f r=%.3f", l, r)
	}
}

func TestPriority(t *testing.T) {
	m := NewMixer(44100)
	m.SetMaxVoices(1)
	tone, _ := m.NewSound(Sine(440, 1, 44100))
	low := m.Play(tone, PlayOptions{Priority: 0})
	high := m.Play(tone, PlayOptions{Priority: 5})
	if low.Playing() || !high.Playing() {
		t.Fatal("higher priority did not steal the voice")
	}
	refused := m.Play(tone, PlayOptions{Priority: 1})
	if refused.Playing() || !high.Playing() {
		t.Fatal("lower priority should be refused, not steal")
	}
	if m.Playing() != 1 {
		t.Fatalf("%d voices, want 1", m.Playing())
	}
}

func TestLowPass(t *testing.T) {
	m := NewMixer(44100)
	high, _ := m.NewSound(Sine(8000, 1, 44100))
	low, _ := m.NewSound(Sine(100, 1, 44100))
	block := make([]float32, 4096)
	level := func(s *Sound) float64 {
		m.StopAll()
		m.Play(s, PlayOptions{LowPass: 500})
		m.Mix(block)
		m.Mix(block)
		l, _ := rms(block)
		return l
	}
	if h, l := level(high), level(low); h > l*0.05 {
		t.Fatalf("8 kHz through a 500 Hz low-pass: %.4f vs 100 Hz %.4f", h, l)
	}
}

func TestReverbTail(t *testing.T) {
	m := NewMixer(44100)
	m.SetReverb(ReverbSettings{RoomSize: 0.8, Wet: 1})
	click, _ := m.NewSound(Sine(1000, 0.02, 44100))
	v := m.Play(click, PlayOptions{Reverb: 1})
	block := make([]float32, 4096)
	for v.Playing() {
		m.Mix(block)
	}
	var tail float64
	for range 20 { // almost a second after the click ended
		m.Mix(block)
		l, r := rms(block)
		tail += l + r
	}
	if tail < 0.01 {
		t.Fatalf("no reverb tail after the source ended: %.4f", tail)
	}
	m.SetReverb(ReverbSettings{})
	m.Mix(block)
	if l, r := rms(block); l+r > 0 {
		t.Fatal("reverb still audible after being turned off")
	}
}

func TestFadeAndPitch(t *testing.T) {
	m := NewMixer(44100)
	tone, _ := m.NewSound(Sine(440, 1, 44100))
	v := m.Play(tone, PlayOptions{})
	v.FadeOut(0.1)
	out := mixUntilDone(m, v, 1000)
	if frames := len(out) / 2; frames > 4410+1024 {
		t.Fatalf("fade-out ran %d frames, want about 4410", frames)
	}
	first, _ := rms(out[:1024])
	last, _ := rms(out[len(out)-2048:])
	if last >= first {
		t.Fatalf("fade did not get quieter: %.3f then %.3f", first, last)
	}
	v = m.Play(tone, PlayOptions{Pitch: 2})
	out = mixUntilDone(m, v, 1000)
	if frames := len(out) / 2; math.Abs(float64(frames-22050)) > 1024 {
		t.Fatalf("pitch 2 ran %d frames, want about 22050", frames)
	}
	v = m.Play(tone, PlayOptions{})
	v.SetPaused(true)
	m.Mix(make([]float32, 1024))
	if !v.Playing() {
		t.Fatal("paused voice ended")
	}
}
