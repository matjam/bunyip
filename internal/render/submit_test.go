package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

// TestQueueCallsStayInSubmit checks that the package reaches the queue
// only through the submit goroutine: vkQueueSubmit2 and vkQueuePresentKHR
// appear in submit.go and nowhere else, so no second thread can call them
// while the goroutine does.
func TestQueueCallsStayInSubmit(t *testing.T) {
	banned := map[string]bool{
		"QueueSubmit": true, "QueueSubmit2": true, "VkQueueSubmit": true, "VkQueueSubmit2": true,
		"QueuePresentKHR": true, "VkQueuePresentKHR": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "submit.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "vk" && banned[sel.Sel.Name] {
				t.Errorf("%s: vk.%s outside submit.go; post a submitJob through Device.submit instead", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

// TestSubmitGoroutineFrames records frames with a different clear colour
// each, interleaved with uploads and readbacks outside the frame, and
// checks that a capture returns the frame it was asked of and that the
// readbacks see the uploads made before them. Run with -race it also
// checks the handoff to the submit goroutine.
func TestSubmitGoroutineFrames(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "submit_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()
	d := r.Device
	pixels := make([]byte, 4*4*4)
	for i := range 40 {
		if i%3 == 0 {
			// Outside a frame: an upload through the batch, then a
			// readback through OneShot, which has to run after it.
			for p := range pixels {
				pixels[p] = byte(i + p)
			}
			img, err := d.NewTextureImage(vk.VkExtent2D{Width: 4, Height: 4}, vk.VK_FORMAT_R8G8B8A8_UNORM, pixels, false)
			if err != nil {
				t.Fatalf("NewTextureImage: %v", err)
			}
			got, err := d.ReadImageRaw(img, 4)
			if err != nil {
				t.Fatalf("ReadImageRaw: %v", err)
			}
			if string(got) != string(pixels) {
				t.Fatalf("frame %d: a readback did not see the upload before it", i)
			}
			img.Destroy()
		}
		if i%2 == 0 {
			// The engine's order: wait for the slot before the input is read.
			if err := r.WaitFrame(); err != nil {
				t.Fatalf("WaitFrame: %v", err)
			}
		}
		fr, ok, err := r.BeginFrame()
		if err != nil || !ok {
			t.Fatalf("BeginFrame: ok=%v err=%v", ok, err)
		}
		shade := float32(i%8) / 7
		r.BeginSwapchainPass(fr, [4]float32{shade, 0, 1 - shade, 1})
		capture := i%5 == 4
		img, err := r.EndFrame(fr, capture)
		if err != nil {
			t.Fatalf("EndFrame: %v", err)
		}
		if !capture {
			continue
		}
		c := img.RGBAAt(8, 8)
		want := linearToSRGB8(shade)
		if diff := int(c.R) - int(want); diff < -2 || diff > 2 || c.G != 0 {
			t.Errorf("frame %d: captured %v, want red %d: the capture is of another frame", i, c, want)
		}
	}
	if err := d.WaitIdle(); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	d.q.drain()
	if d.q.done != d.q.posted {
		t.Fatalf("submit goroutine still has %d jobs after WaitIdle", d.q.posted-d.q.done)
	}
}

// linearToSRGB8 encodes a linear channel the way an sRGB attachment does.
func linearToSRGB8(v float32) uint8 {
	var s float64
	x := float64(v)
	if x <= 0.0031308 {
		s = 12.92 * x
	} else {
		s = 1.055*math.Pow(x, 1/2.4) - 0.055
	}
	return uint8(s*255 + 0.5)
}
