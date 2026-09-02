package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestMixLoopAndStop(t *testing.T) {
	m := NewMixer(1000)
	snd, err := m.NewSound(PCM{Samples: []float32{0.5, 0.5, 0.5, 0.5}, Channels: 1, Rate: 1000})
	if err != nil {
		t.Fatal(err)
	}
	v := m.Play(snd, PlayOptions{Loop: true, Volume: 0.5})
	out := make([]float32, 20)
	m.mix(out)
	// Centre pan is sqrt(1/2) per side; 0.5 * 0.5 * 0.707 = 0.177 on every frame because it loops.
	for i, s := range out {
		if math.Abs(float64(s)-0.1767) > 0.002 {
			t.Fatalf("sample %d = %v, want 0.177", i, s)
		}
	}
	v.Stop()
	m.mix(out)
	if out[0] != 0 || m.Playing() != 0 {
		t.Errorf("voice kept playing after Stop: %v, %d voices", out[0], m.Playing())
	}
	one := m.Play(snd, PlayOptions{})
	m.mix(out) // 10 frames, sound is 4: it ends within this call
	if one.Playing() || m.Playing() != 0 {
		t.Error("one-shot voice did not finish")
	}
	if out[8] != 0 {
		t.Errorf("frame after the end should be silent, got %v", out[8])
	}
}

func TestPanAndClamp(t *testing.T) {
	m := NewMixer(100)
	snd, _ := m.NewSound(PCM{Samples: []float32{1, 1}, Channels: 1, Rate: 100})
	m.Play(snd, PlayOptions{Pan: 1})
	m.Play(snd, PlayOptions{Pan: 1})
	out := make([]float32, 4)
	m.mix(out)
	if out[0] != 0 || out[1] != 1 {
		t.Errorf("hard-right pan gave L=%v R=%v; want 0 and 1 (clamped)", out[0], out[1])
	}
}

func TestResample(t *testing.T) {
	m := NewMixer(48000)
	snd, err := m.NewSound(Sine(440, 0.5, 44100))
	if err != nil {
		t.Fatal(err)
	}
	if got := snd.Frames(); got < 23990 || got > 24010 {
		t.Errorf("resampled to %d frames, want about 24000", got)
	}
}

func TestDecodeWAV(t *testing.T) {
	var b []byte
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 36+8)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)     // PCM
	b = binary.LittleEndian.AppendUint16(b, 2)     // stereo
	b = binary.LittleEndian.AppendUint32(b, 22050) // rate
	b = binary.LittleEndian.AppendUint32(b, 22050*4)
	b = binary.LittleEndian.AppendUint16(b, 4)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, 8)
	for _, s := range []int16{16384, -16384, 32767, 0} {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	p, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.Channels != 2 || p.Rate != 22050 || len(p.Samples) != 4 {
		t.Fatalf("decoded %+v", p)
	}
	if p.Samples[0] != 0.5 || p.Samples[1] != -0.5 || p.Samples[3] != 0 {
		t.Errorf("samples %v", p.Samples)
	}
}
