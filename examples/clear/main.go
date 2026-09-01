// Command clear opens a window and clears it to a colour that cycles over
// time. With -shot it also writes one frame to a PNG, which is how renderer
// output is checked without a person looking at the screen.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds (0: until closed)")
	shot := flag.String("shot", "", "write the frame at -seconds/2 (or the first frame) to this PNG")
	validate := flag.Bool("validate", true, "enable validation layers when installed")
	flag.Parse()
	if err := run(*seconds, *shot, *validate); err != nil {
		fmt.Fprintln(os.Stderr, "clear:", err)
		os.Exit(1)
	}
}

func run(seconds float64, shot string, validate bool) error {
	app, err := platform.NewApp()
	if err != nil {
		return err
	}
	win, err := app.NewWindow(platform.Config{Title: "Bunyip clear", Width: 640, Height: 480, Resizable: true})
	if err != nil {
		return err
	}
	pw, ph := win.PixelSize()
	cfg := render.Config{AppName: "clear", Validation: validate, Log: slog.Default()}
	r, err := render.NewRenderer(cfg, platform.RequiredInstanceExtensions(), win.CreateSurface,
		vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, true)
	if err != nil {
		return err
	}
	defer r.Destroy()

	start := time.Now()
	shotAt := time.Duration(math.Inf(1))
	if shot != "" {
		shotAt = 0
		if seconds > 0 {
			shotAt = time.Duration(seconds / 2 * float64(time.Second))
		}
	}
	frames := 0
	for !win.Closed() {
		for _, e := range app.Poll(false) {
			switch {
			case e.Kind == platform.EventClose, e.Kind == platform.EventKeyDown && e.Key == input.KeyEscape:
				win.Close()
			case e.Kind == platform.EventResize:
				r.Resize(e.PixelW, e.PixelH)
			}
		}
		if win.Closed() {
			break
		}
		t := time.Since(start)
		if seconds > 0 && t.Seconds() >= seconds {
			win.Close()
			break
		}
		s := t.Seconds()
		clear := [4]float32{float32(0.5 + 0.5*math.Sin(s)), float32(0.5 + 0.5*math.Sin(s+2)), float32(0.5 + 0.5*math.Sin(s+4)), 1}
		fr, ok, err := r.BeginFrame()
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		r.BeginSwapchainPass(fr, clear)
		capture := shot != "" && t >= shotAt
		img, err := r.EndFrame(fr, capture)
		if err != nil {
			return err
		}
		frames++
		if capture {
			if err := writePNG(shot, img); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%dx%d) at frame %d\n", shot, img.Bounds().Dx(), img.Bounds().Dy(), frames)
			shot = ""
		}
	}
	fmt.Printf("%d frames in %.1fs\n", frames, time.Since(start).Seconds())
	return nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
