// Command window opens a native window, creates a Vulkan surface over it and
// prints every event until the window is closed or -seconds elapse. It is the
// smoke test for the platform layer.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
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
	fmt.Printf("window: %dx%d points, %dx%d pixels, scale %.2f\n", w, h, pw, ph, win.Scale())

	instance, surface, err := createSurface(win)
	if err != nil {
		return err
	}
	fmt.Printf("surface: 0x%x on instance 0x%x\n", surface, instance)
	defer func() {
		vk.VkDestroySurfaceKHR(instance, surface, nil)
		vk.VkDestroyInstance(instance, nil)
	}()
	if err := reportSurface(instance, surface); err != nil {
		return err
	}

	deadline := time.Time{}
	if seconds > 0 {
		deadline = time.Now().Add(time.Duration(seconds * float64(time.Second)))
	}
	for !win.Closed() {
		for _, e := range app.Poll(wait && deadline.IsZero()) {
			printEvent(e)
			if e.Kind == platform.EventClose || (e.Kind == platform.EventKeyDown && e.Key == input.KeyEscape) {
				win.Close()
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			win.Close()
		}
		if !wait {
			time.Sleep(4 * time.Millisecond)
		}
	}
	fmt.Println("closed")
	return nil
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
	default:
		fmt.Println(e.Kind)
	}
}

func createSurface(win *platform.Window) (vk.VkInstance, vk.VkSurfaceKHR, error) {
	if err := vk.Load(); err != nil {
		return 0, 0, err
	}
	var keep [][]byte
	cstr := func(s string) *byte {
		p, b := vk.CString(s)
		keep = append(keep, b)
		return p
	}
	names := append(platform.RequiredInstanceExtensions(), vk.VK_KHR_PORTABILITY_ENUMERATION_EXTENSION_NAME)
	enabled := make([]*byte, 0, len(names))
	for _, n := range names {
		enabled = append(enabled, cstr(n))
	}
	app := vk.VkApplicationInfo{SType: vk.VK_STRUCTURE_TYPE_APPLICATION_INFO, PApplicationName: cstr("window"), ApiVersion: vk.API_VERSION_1_3}
	info := vk.VkInstanceCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
		Flags:                   vk.VK_INSTANCE_CREATE_ENUMERATE_PORTABILITY_BIT_KHR,
		PApplicationInfo:        &app,
		EnabledExtensionCount:   uint32(len(enabled)),
		PpEnabledExtensionNames: &enabled[0],
	}
	var instance vk.VkInstance
	if err := vk.Check("vkCreateInstance", vk.VkCreateInstance(&info, nil, &instance)); err != nil {
		return 0, 0, err
	}
	runtime.KeepAlive(keep)
	if err := vk.LoadInstance(instance); err != nil {
		return 0, 0, err
	}
	surface, err := win.CreateSurface(instance)
	return instance, surface, err
}

// reportSurface asks the first physical device what it can present to the
// surface, which exercises the whole path MoltenVK needs for a swapchain.
func reportSurface(instance vk.VkInstance, surface vk.VkSurfaceKHR) error {
	var count uint32 = 1
	var dev vk.VkPhysicalDevice
	r := vk.VkEnumeratePhysicalDevices(instance, &count, &dev)
	if r != vk.VK_SUCCESS && r != vk.VK_INCOMPLETE {
		return vk.Check("vkEnumeratePhysicalDevices", r)
	}
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
