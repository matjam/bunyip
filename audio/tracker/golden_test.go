package tracker

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// goldenStride keeps every 64th stereo frame of a render in its
// reference, which is enough to catch a change without a large file.
const goldenStride = 64

// goldenTolerance is the largest difference a kept sample may have from
// its reference. Float32 rounding and fusion move the render by well
// under 1e-6; a real change to the arithmetic moves it by far more.
const goldenTolerance = 1e-5

// TestGoldenRender renders five seconds of the 32-channel module, with
// resonant filters on half its channels and the Amiga filter on, and
// compares every 64th frame with a reference recorded before the filters
// flushed their denormal state. Flushing only touches state below 1e-15,
// so the largest difference is 0 today on the architecture the reference
// was recorded on. A tolerance rather than exact bits lets one reference
// serve every architecture and compiler, because arm64 fuses multiplies
// and adds and amd64 does not. To record it again, set
// BUNYIP_UPDATE_GOLDEN=1, and only on a commit whose output is known good.
func TestGoldenRender(t *testing.T) {
	for _, cubic := range []bool{false, true} {
		name := "linear"
		if cubic {
			name = "cubic"
		}
		t.Run(name, func(t *testing.T) {
			p := NewPlayer(perf32(), 48000)
			p.Loop = true
			p.Cubic = cubic
			p.AmigaFilter, p.amigaFilter = true, true
			out := make([]float32, 512*2)
			var kept []float32
			frame := 0
			var peak float32
			for range 5 * 48000 / 512 {
				p.Read(out)
				for i := 0; i < len(out); i += 2 {
					if frame%goldenStride == 0 {
						kept = append(kept, out[i], out[i+1])
						peak = max(peak, out[i], -out[i], out[i+1], -out[i+1])
					}
					frame++
				}
			}
			if peak < 0.05 {
				t.Fatalf("render is nearly silent (peak %g)", peak)
			}
			path := filepath.Join("testdata", "golden_"+name+".f32")
			if os.Getenv("BUNYIP_UPDATE_GOLDEN") != "" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
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
				t.Fatalf("reference has %d samples, render has %d", len(b)/4, len(kept))
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
		})
	}
}
