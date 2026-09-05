// Command window opens a native window, presents cleared frames through a
// Vulkan swapchain and prints every event until the window is closed or
// -seconds elapse. It is the smoke test for the platform layer. Presenting
// is part of the test: a Wayland window only appears once a buffer is
// committed to its surface, so a window that opens on every platform must
// draw. The frames clear to one dark colour; the moment the window shows
// it, the whole stack from event loop to present has worked.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds (0: run until closed)")
	wait := flag.Bool("wait", false, "block for events instead of polling (turn-based mode)")
	flag.Parse()
	if err := run(*seconds, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		os.Exit(1)
	}
}

func run(seconds float64, wait bool) error {
	app, err := platform.NewApp()
	if err != nil {
		return err
	}
	win, err := app.NewWindow(platform.Config{Title: "Bunyip window", Width: 800, Height: 600, Resizable: true})
	if err != nil {
		return err
	}
	w, h := win.Size()
	pw, ph := win.PixelSize()
	fmt.Printf("window: %dx%d points, %dx%d pixels, scale %.2f, visible %v\n", w, h, pw, ph, win.Scale(), win.Visible())

	var surface vk.VkSurfaceKHR
	r, err := render.NewRenderer(render.Config{AppName: "window"},
		platform.RequiredInstanceExtensions(),
		func(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
			s, err := win.CreateSurface(instance)
			surface = s
			return s, err
		},
		vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, true)
	if err != nil {
		return err
	}
	defer r.Destroy()
	fmt.Printf("surface: 0x%x on instance 0x%x\n", surface, r.Instance.Handle)
	if err := reportSurface(r.Device.Physical(), surface); err != nil {
		return err
	}
	fmt.Printf("swapchain: %dx%d, format %d, %d images\n",
		r.Swapchain.Extent.Width, r.Swapchain.Extent.Height, r.Swapchain.Format, len(r.Swapchain.Images))

	deadline := time.Time{}
	if seconds > 0 {
		deadline = time.Now().Add(time.Duration(seconds * float64(time.Second)))
	}
	for !win.Closed() {
		// The frame comes first so the window is mapped before the first
		// blocking poll; vsync paces the loop in polling mode.
		if err := present(r); err != nil {
			return err
		}
		for _, e := range app.Poll(wait && deadline.IsZero()) {
			printEvent(e)
			if e.Kind == platform.EventResize {
				r.Resize(e.PixelW, e.PixelH)
			}
			if e.Kind == platform.EventClose || (e.Kind == platform.EventKeyDown && e.Key == input.KeyEscape) {
				win.Close()
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			win.Close()
		}
	}
	fmt.Println("closed")
	return nil
}

// present records one frame that clears the swapchain image and presents
// it. A frame is dropped without error while the swapchain rebuilds after
// a resize.
func present(r *render.Renderer) error {
	fr, ok, err := r.BeginFrame()
	if err != nil || !ok {
		return err
	}
	r.BeginSwapchainPass(fr, [4]float32{0.10, 0.12, 0.16, 1})
	_, err = r.EndFrame(fr, false)
	return err
}

func printEvent(e platform.Event) {
	switch e.Kind {
	case platform.EventMouseMove:
		return // too chatty for a log
	case platform.EventKeyDown, platform.EventKeyUp:
		fmt.Printf("%s key=%s mods=%s repeat=%v\n", e.Kind, e.Key, e.Mods, e.Repeat)
	case platform.EventChar:
		fmt.Printf("%s %q\n", e.Kind, e.Rune)
	case platform.EventMouseDown, platform.EventMouseUp:
		fmt.Printf("%s button=%d at %.0f,%.0f\n", e.Kind, e.Button, e.X, e.Y)
	case platform.EventScroll:
		fmt.Printf("%s dx=%.2f dy=%.2f precise=%v\n", e.Kind, e.DX, e.DY, e.Precise)
	case platform.EventResize:
		fmt.Printf("%s %dx%d points, %dx%d pixels, scale %.2f\n", e.Kind, e.Width, e.Height, e.PixelW, e.PixelH, e.Scale)
	case platform.EventFocus:
		fmt.Printf("%s focused=%v\n", e.Kind, e.Focused)
	case platform.EventVisible:
		fmt.Printf("%s visible=%v\n", e.Kind, e.Visible)
	default:
		fmt.Println(e.Kind)
	}
}

// reportSurface prints what the chosen physical device can present to the
// surface, which exercises the whole path a swapchain needs.
func reportSurface(dev vk.VkPhysicalDevice, surface vk.VkSurfaceKHR) error {
	var caps vk.VkSurfaceCapabilitiesKHR
	if err := vk.Check("vkGetPhysicalDeviceSurfaceCapabilitiesKHR", vk.VkGetPhysicalDeviceSurfaceCapabilitiesKHR(dev, surface, &caps)); err != nil {
		return err
	}
	fmt.Printf("surface: current extent %dx%d, images %d..%d, transforms 0x%x, usage 0x%x\n",
		caps.CurrentExtent.Width, caps.CurrentExtent.Height, caps.MinImageCount, caps.MaxImageCount,
		caps.SupportedTransforms, caps.SupportedUsageFlags)
	var supported vk.VkBool32
	if err := vk.Check("vkGetPhysicalDeviceSurfaceSupportKHR", vk.VkGetPhysicalDeviceSurfaceSupportKHR(dev, 0, surface, &supported)); err != nil {
		return err
	}
	var nfmt uint32
	vk.VkGetPhysicalDeviceSurfaceFormatsKHR(dev, surface, &nfmt, nil)
	formats := make([]vk.VkSurfaceFormatKHR, nfmt)
	if nfmt > 0 {
		vk.VkGetPhysicalDeviceSurfaceFormatsKHR(dev, surface, &nfmt, &formats[0])
	}
	fmt.Printf("surface: queue family 0 present support %d, %d formats (first: format %d colorspace %d)\n",
		supported, nfmt, formats[0].Format, formats[0].ColorSpace)
	return nil
}
