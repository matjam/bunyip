// Package examples holds the example programs; this test builds and runs
// each one headless for a moment and checks that it drew something, so
// a change to the engine that breaks an example is caught without
// anyone watching a screen. It needs a GPU and is skipped with -short.
package examples

import (
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// skip lists examples that need something a test machine lacks.
var skip = map[string]string{
	"network": "needs a peer to talk to",
	"clear":   "clears to one colour by design",
	"window":  "the platform layer's own smoke test, without the engine's screenshot",
}

func TestExamplesRun(t *testing.T) {
	if testing.Short() {
		t.Skip("examples need a GPU")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if why, ok := skip[name]; ok {
			t.Logf("%s: skipped, %s", name, why)
			continue
		}
		if _, err := os.Stat(filepath.Join(name, "main.go")); err != nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			shot := filepath.Join(t.TempDir(), "shot.png")
			cmd := exec.Command("go", "run", "./"+name, "-seconds", "1.5", "-shot", shot)
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "BUNYIP_HEADLESS=1")
			start := time.Now()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed after %v: %v\n%s", name, time.Since(start).Round(time.Millisecond), err, out)
			}
			f, err := os.Open(shot)
			if err != nil {
				t.Fatalf("%s wrote no screenshot: %v\n%s", name, err, out)
			}
			defer f.Close()
			img, err := png.Decode(f)
			if err != nil {
				t.Fatalf("%s: bad screenshot: %v", name, err)
			}
			// Something must have been drawn: not every pixel the same.
			b := img.Bounds()
			first := img.At(b.Min.X, b.Min.Y)
			r0, g0, b0, _ := first.RGBA()
			for y := b.Min.Y; y < b.Max.Y; y += 8 {
				for x := b.Min.X; x < b.Max.X; x += 8 {
					r, g, bl, _ := img.At(x, y).RGBA()
					if r != r0 || g != g0 || bl != b0 {
						return
					}
				}
			}
			t.Errorf("%s drew a blank frame", name)
		})
	}
}
