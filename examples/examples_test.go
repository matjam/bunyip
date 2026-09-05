// Package examples holds the example programs; this test builds and runs
// each one headless for a moment, checks that it drew something, and
// compares the frame against a stored image, so a change to the engine
// that breaks an example is caught without anyone watching a screen. It
// needs a GPU and is skipped with -short.
//
// The stored images are in testdata, downscaled to 320 pixels wide.
// Rewrite them after a deliberate change with
//
//	CGO_ENABLED=0 go test ./examples -run TestExamplesRun -update
//
// and add -docs to rewrite the 640-wide walkthrough screenshots in
// docs/examples from the same run.
package examples

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	xdraw "golang.org/x/image/draw"
)

var (
	update = flag.Bool("update", false, "rewrite the golden images in testdata from this run")
	docs   = flag.Bool("docs", false, "with -update, also rewrite the walkthrough screenshots in docs/examples")
)

const (
	goldenDir = "testdata"
	docsDir   = "../docs/examples"
	// goldenWidth is what the frames are compared at. Small enough that
	// the file is a few tens of kilobytes and a one-pixel difference in
	// a glyph disappears, large enough that a widget moving does not.
	goldenWidth = 320
	// docsWidth is the width the walkthroughs show a screenshot at.
	docsWidth = 640
	// seconds each example runs for before it is asked to quit; the
	// screenshot is taken halfway through.
	seconds = "1.5"
)

// skip lists examples that need something a test machine lacks.
var skip = map[string]string{
	"network": "needs a peer to talk to",
	"clear":   "clears to one colour by design",
	"window":  "the platform layer's own smoke test, without the engine's screenshot",
}

// noGolden lists examples that run and are checked for having drawn
// something, but whose picture is not the same twice and so cannot be
// compared against a stored image.
var noGolden = map[string]string{
	"assets": "counts its runs in a save file and draws the count, and rewrites its own images on disk as it goes",
}

// tolerances the comparison allows. The examples that print a measured
// millisecond figure change a few digits between runs, which is what the
// mean is loose enough to absorb; the blurred comparison throws those
// away entirely and so is held much tighter, because what is left of a
// blurred frame is where things are rather than what they say.
const (
	meanTolerance = 0.02 // mean absolute channel difference, 0 to 1
	blurTolerance = 0.06 // largest channel difference after blurring, 0 to 1
	blurWidth     = 40   // the width both frames are reduced to first
)

func TestExamplesRun(t *testing.T) {
	if testing.Short() {
		t.Skip("examples need a GPU")
	}
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
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
			cmd := exec.Command("go", "run", "./"+name, "-seconds", seconds, "-shot", shot)
			// The fixed clock is what makes the frame reproducible: the
			// clock counts frames rather than reading the wall clock, so
			// the same frame is drawn however long the machine takes.
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "BUNYIP_HEADLESS=1", "BUNYIP_FIXED_CLOCK=1")
			start := time.Now()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed after %v: %v\n%s", name, time.Since(start).Round(time.Millisecond), err, out)
			}
			img, err := readPNG(shot)
			if err != nil {
				t.Fatalf("%s: %v\n%s", name, err, out)
			}
			if blank(img) {
				t.Fatalf("%s drew a blank frame", name)
			}
			if why, ok := noGolden[name]; ok {
				if *update {
					t.Logf("%s: no golden, %s", name, why)
				}
				return
			}
			got := scaleTo(img, goldenWidth)
			if *update {
				if err := writePNG(filepath.Join(goldenDir, name+".png"), got); err != nil {
					t.Fatal(err)
				}
				if *docs {
					if err := writePNG(filepath.Join(docsDir, name+".png"), scaleTo(img, docsWidth)); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			compare(t, name, got)
		})
	}
}

// compare checks a frame against the stored image for an example.
func compare(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".png")
	golden, err := readPNG(path)
	if err != nil {
		t.Fatalf("%s: %v\n\trun the test again with -update to record what the example draws now", name, err)
	}
	want := toRGBA(golden)
	if want.Bounds() != got.Bounds() {
		t.Fatalf("%s: the frame is %v and %s is %v; the example's window size changed, so rerun with -update",
			name, got.Bounds().Size(), path, want.Bounds().Size())
	}
	mean := meanDiff(got, want)
	blur := maxDiff(scaleTo(got, blurWidth), scaleTo(want, blurWidth))
	if mean <= meanTolerance && blur <= blurTolerance {
		return
	}
	dir, derr := os.MkdirTemp("", "bunyip-golden-"+name+"-")
	if derr != nil {
		t.Fatalf("%s: differs from %s and the diff could not be written: %v", name, path, derr)
	}
	// The directory is deliberately left behind: the point of the
	// message is that somebody can look at the three images.
	for file, img := range map[string]*image.RGBA{
		"got.png":  got,
		"want.png": want,
		"diff.png": diffImage(got, want),
	} {
		if err := writePNG(filepath.Join(dir, file), img); err != nil {
			t.Errorf("writing %s: %v", file, err)
		}
	}
	t.Errorf("%s does not match %s:\n"+
		"\tmean channel difference %.4f (allowed %.4f), blurred %.4f (allowed %.4f)\n"+
		"\tgot, want and diff written to %s\n"+
		"\tif the change is deliberate, rerun with -update",
		name, path, mean, meanTolerance, blur, blurTolerance, dir)
}

// blank reports whether every sampled pixel of a frame is the same
// colour, which is what an example that drew nothing produces.
func blank(img image.Image) bool {
	b := img.Bounds()
	first := img.At(b.Min.X, b.Min.Y)
	r0, g0, b0, _ := first.RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 8 {
		for x := b.Min.X; x < b.Max.X; x += 8 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bl != b0 {
				return false
			}
		}
	}
	return true
}

// scaleTo resamples an image to a width, keeping its proportions.
func scaleTo(src image.Image, width int) *image.RGBA {
	b := src.Bounds()
	height := max(b.Dy()*width/b.Dx(), 1)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

// toRGBA returns an image as an RGBA, copying only when it is not one.
func toRGBA(src image.Image) *image.RGBA {
	if img, ok := src.(*image.RGBA); ok {
		return img
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(dst, dst.Bounds(), src, b.Min, xdraw.Src)
	return dst
}

// meanDiff is the mean absolute difference of the colour channels of two
// images of the same size, from 0 (identical) to 1.
func meanDiff(a, b *image.RGBA) float64 {
	var total float64
	n := 0
	for i := 0; i < len(a.Pix); i += 4 {
		for c := range 3 {
			total += float64(abs8(a.Pix[i+c], b.Pix[i+c]))
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n) / 255
}

// maxDiff is the largest single-channel difference between two images of
// the same size, from 0 to 1.
func maxDiff(a, b *image.RGBA) float64 {
	var worst uint8
	for i := 0; i < len(a.Pix); i += 4 {
		for c := range 3 {
			worst = max(worst, abs8(a.Pix[i+c], b.Pix[i+c]))
		}
	}
	return float64(worst) / 255
}

func abs8(x, y uint8) uint8 {
	if x > y {
		return x - y
	}
	return y - x
}

// diffImage paints where two frames differ: black where they agree,
// rising through red to white where they do not.
func diffImage(a, b *image.RGBA) *image.RGBA {
	out := image.NewRGBA(a.Bounds())
	for i := 0; i < len(a.Pix); i += 4 {
		d := 0
		for c := range 3 {
			d = max(d, int(abs8(a.Pix[i+c], b.Pix[i+c])))
		}
		// Amplified, because a difference worth looking at is often a
		// few levels rather than a few hundred.
		v := min(d*4, 255)
		out.Pix[i] = uint8(v)
		out.Pix[i+1] = uint8(max(v-128, 0) * 2)
		out.Pix[i+2] = uint8(max(v-128, 0) * 2)
		out.Pix[i+3] = 255
	}
	return out
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no screenshot: %w", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("bad screenshot: %w", err)
	}
	return img, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// TestGoldensExist checks that every example that is compared has a
// stored image, so one added without recording a golden fails here
// rather than the first time somebody runs the comparison.
func TestGoldensExist(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, ok := skip[name]; ok {
			continue
		}
		if _, ok := noGolden[name]; ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(name, "main.go")); err != nil {
			continue
		}
		path := filepath.Join(goldenDir, name+".png")
		img, err := readPNG(path)
		if err != nil {
			t.Errorf("examples/%s has no golden image: %v\n\trecord one with: go test ./examples -run TestExamplesRun -update", name, err)
			continue
		}
		if img.Bounds().Dx() != goldenWidth {
			t.Errorf("%s is %d pixels wide, want %d", path, img.Bounds().Dx(), goldenWidth)
		}
	}
}

// TestDiffImageIsBlackWhenTheFramesAgree checks the comparison itself:
// two identical frames differ by nothing, and one changed pixel shows up.
func TestDiffImageIsBlackWhenTheFramesAgree(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range a.Pix {
		a.Pix[i] = 200
	}
	// A copy, not toRGBA: that returns an RGBA image unchanged, so the
	// two names would be one image and the changed pixel below would
	// change both.
	b := image.NewRGBA(a.Bounds())
	copy(b.Pix, a.Pix)
	if got := meanDiff(a, b); got != 0 {
		t.Errorf("identical frames differ by %v", got)
	}
	if got := maxDiff(a, b); got != 0 {
		t.Errorf("identical frames differ by %v at worst", got)
	}
	d := diffImage(a, b)
	for i := 0; i < len(d.Pix); i += 4 {
		if d.Pix[i] != 0 || d.Pix[i+1] != 0 || d.Pix[i+2] != 0 {
			t.Fatalf("the diff of two identical frames is not black at %d", i)
		}
	}
	b.Set(1, 1, color.RGBA{0, 0, 0, 255})
	if meanDiff(a, b) == 0 || maxDiff(a, b) == 0 {
		t.Error("a changed pixel did not show up in the comparison")
	}
}
