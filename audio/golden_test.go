package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// The golden mixes pin the mixer's output against a reference recorded
// from the mixer before the denormal flushing and the binaural rework.
// Each reference keeps every goldenStride-th frame of a scene as
// little-endian float32, and the test requires every kept sample to be
// within goldenTolerance of it. A tolerance rather than exact bits lets
// one reference serve every architecture and compiler: arm64 fuses
// multiplies and adds and amd64 does not, and a toolchain may change which
// operations it fuses, which moves samples by float32 rounding and is not
// a regression. To record the references again, set
// BUNYIP_UPDATE_GOLDEN=1, and only on a commit whose output is known good.

const goldenSeconds = 5

// goldenStereo is a sound whose channels differ, so a stereo path cannot
// pass for a mono one.
func goldenStereo(t *testing.T, m *Mixer) *Sound {
	t.Helper()
	l, r := Sine(440, 2, benchRate), Sine(663, 2, benchRate)
	s := make([]float32, len(l.Samples)*2)
	for i := range l.Samples {
		s[i*2], s[i*2+1] = l.Samples[i], r.Samples[i]*0.7
	}
	snd, err := m.NewSound(PCM{Samples: s, Channels: 2, Rate: benchRate})
	if err != nil {
		t.Fatal(err)
	}
	return snd
}

func goldenMono(t *testing.T, m *Mixer, freq float64) *Sound {
	t.Helper()
	snd, err := m.NewSound(Sine(freq, 1.5, 44100))
	if err != nil {
		t.Fatal(err)
	}
	return snd
}

// goldenScene renders goldenSeconds of one scene and returns the output.
// "pan" plays stereo and mono sounds, filtered, occluded, positional and
// not, with the shared reverb, a bus reverb, Doppler and two streams, one
// at pitch 1. "binaural_mono" puts mono sounds through the head model.
// "binaural_stereo" puts stereo sounds through it.
func goldenScene(t *testing.T, scene string) []float32 {
	t.Helper()
	m := NewMixer(benchRate)
	m.SetReverb(ReverbSettings{RoomSize: 0.7, Wet: 0.25})
	m.SetDoppler(1)
	stereo := goldenStereo(t, m)
	mono := goldenMono(t, m, 330)
	mono2 := goldenMono(t, m, 517)
	var vs []*Voice
	switch scene {
	case "pan":
		m.Dialogue().SetReverb(ReverbSettings{RoomSize: 0.4, Damping: 0.2, Wet: 0.3})
		vs = append(vs,
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.3, Pan: -0.3, Reverb: 0.3}),
			m.Play(mono, PlayOptions{Loop: true, Volume: 0.4, LowPass: 1200, Positional: true,
				Position: lin.Vec3{X: 3, Z: -2}, Velocity: lin.Vec3{X: -2}, Reverb: 0.2}),
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.3, Occlusion: 0.5, Positional: true,
				Position: lin.Vec3{X: -4, Y: 1, Z: 1}, Bus: m.Dialogue(), Reverb: 0.5, Pitch: 1.1}),
			m.Play(mono2, PlayOptions{Loop: true, Volume: 0.3, Occlusion: 0.4, LowPass: 800}),
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.2, LowPass: 2500, Pitch: 0.9, Reverb: 0.1}),
			m.PlayStream(&perfStream{step: 2 * math.Pi * 220 / benchRate}, PlayOptions{Volume: 0.2, Reverb: 0.2}),
			m.PlayStream(&perfStream{step: 2 * math.Pi * 150 / benchRate}, PlayOptions{Volume: 0.2, Pitch: 1.25, LowPass: 3000}),
		)
	case "binaural_mono":
		m.SetSpatial(SpatialSettings{Binaural: true})
		vs = append(vs,
			m.Play(mono, PlayOptions{Loop: true, Volume: 0.4, LowPass: 1200, Positional: true,
				Position: lin.Vec3{X: 3, Z: -2}, Velocity: lin.Vec3{X: -2}, Reverb: 0.2}),
			m.Play(mono2, PlayOptions{Loop: true, Volume: 0.4, Occlusion: 0.6, Positional: true,
				Position: lin.Vec3{X: -2, Y: 1, Z: 2}, Reverb: 0.4}),
			m.Play(mono, PlayOptions{Loop: true, Volume: 0.3, Positional: true, Position: lin.Vec3{Y: -3, Z: -1}, Pitch: 1.2}),
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.2, Pan: 0.5}),
		)
	case "binaural_stereo":
		m.SetSpatial(SpatialSettings{Binaural: true})
		vs = append(vs,
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.4, LowPass: 1200, Positional: true,
				Position: lin.Vec3{X: 3, Z: -2}, Velocity: lin.Vec3{X: -2}, Reverb: 0.2}),
			m.Play(stereo, PlayOptions{Loop: true, Volume: 0.4, Occlusion: 0.6, Positional: true,
				Position: lin.Vec3{X: -2, Y: 1, Z: 2}, Reverb: 0.4, Pitch: 0.8}),
			m.PlayStream(&perfStream{step: 2 * math.Pi * 220 / benchRate}, PlayOptions{Volume: 0.3, Positional: true,
				Position: lin.Vec3{X: 1, Z: 1}, Occlusion: 0.3}),
		)
	default:
		t.Fatalf("unknown scene %q", scene)
	}
	blocks := goldenSeconds * benchRate / benchBlock
	all := make([]float32, 0, blocks*benchBlock*2)
	out := make([]float32, benchBlock*2)
	for b := range blocks {
		// Move the sources and change settings as a game would, on block
		// boundaries so the scene is the same every run.
		if b%8 == 0 {
			a := float32(b) / 40
			for i, v := range vs {
				if i%2 == 0 {
					v.SetPosition(lin.Vec3{X: 4 * float32(math.Cos(float64(a+float32(i)))), Y: 0.5, Z: 4 * float32(math.Sin(float64(a+float32(i))))})
				}
			}
			m.SetListener(Listener{Position: lin.Vec3{X: a / 10}, Forward: lin.Vec3{Z: -1}, Up: lin.Vec3{Y: 1}, Velocity: lin.Vec3{X: 0.5}})
		}
		if b == 100 {
			vs[1].SetOcclusion(0.8)
			vs[0].SetLowPass(600)
		}
		if b == 200 {
			vs[len(vs)-1].Stop()
		}
		if b == 260 {
			vs[0].FadeTo(0.1, 0.5)
		}
		m.mix(out)
		all = append(all, out...)
	}
	return all
}

// goldenStride keeps every 64th stereo frame of a scene in its reference,
// which is enough to catch a change in any path without a large file.
const goldenStride = 64

// goldenTolerance is the largest difference a kept sample may have from
// its reference. Float32 rounding and fusion move the mix by well under
// 1e-6; a real change to the arithmetic moves it by far more.
const goldenTolerance = 1e-5

// TestGoldenMix renders each scene and compares it with its reference.
// "pan" and "binaural_mono" came out bit for bit the same after the
// changes on arm64 and amd64, so their largest difference is 0 today on
// the architecture the references were recorded on. "binaural_stereo"
// averages its channels before the voice's filters instead of after, the
// same arithmetic up to float32 rounding.
func TestGoldenMix(t *testing.T) {
	for _, scene := range []string{"pan", "binaural_mono", "binaural_stereo"} {
		t.Run(scene, func(t *testing.T) {
			checkGolden(t, filepath.Join("testdata", "golden_"+scene+".f32"), goldenScene(t, scene))
		})
	}
}

// checkGolden keeps every goldenStride-th frame of out and compares it
// with the reference at path, or records it there when
// BUNYIP_UPDATE_GOLDEN is set.
func checkGolden(t *testing.T, path string, out []float32) {
	t.Helper()
	var kept []float32
	for i := 0; i+1 < len(out); i += 2 * goldenStride {
		kept = append(kept, out[i], out[i+1])
	}
	var peak float32
	for _, v := range kept {
		peak = max(peak, v, -v)
	}
	if peak < 0.05 {
		t.Fatalf("output is nearly silent (peak %g)", peak)
	}
	if os.Getenv("BUNYIP_UPDATE_GOLDEN") != "" {
		b := make([]byte, len(kept)*4)
		for i, v := range kept {
			binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != len(kept)*4 {
		t.Fatalf("reference has %d samples, output has %d", len(b)/4, len(kept))
	}
	var worst float64
	at := 0
	for i, v := range kept {
		w := math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
		if d := math.Abs(float64(v) - float64(w)); d > worst || d != d {
			worst, at = d, i
		}
	}
	t.Logf("largest difference from the reference: %g", worst)
	if !(worst <= goldenTolerance) {
		t.Errorf("sample %d differs from the reference by %g, want at most %g", at, worst, goldenTolerance)
	}
}
