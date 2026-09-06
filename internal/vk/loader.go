package vk

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/ebitengine/purego"
)

// VkGetInstanceProcAddr is the driver's entry-point resolver, bound by Load.
var VkGetInstanceProcAddr func(instance VkInstance, pName string) PFN_vkVoidFunction

var libHandle uintptr

var boundDeviceInstance VkInstance

// LibraryEnv names the environment variable that overrides the library search.
const LibraryEnv = "BUNYIP_VULKAN_LIBRARY"

// ErrNotLoaded is returned when a load step runs before Load.
var ErrNotLoaded = errors.New("vk: Load has not been called")

// Load opens the Vulkan loader (or, on macOS, MoltenVK directly as a
// fallback) and binds the global commands. It is idempotent.
func Load() error {
	if libHandle != 0 {
		return nil
	}
	var errs []error
	for _, name := range libraryCandidates() {
		h, err := openLibrary(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		addr, err := lookupSymbol(h, "vkGetInstanceProcAddr")
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		libHandle = h
		purego.RegisterFunc(&VkGetInstanceProcAddr, addr)
		bindGlobalCommands(func(cmd string) uintptr {
			return uintptr(VkGetInstanceProcAddr(0, cmd))
		})
		return nil
	}
	return fmt.Errorf("vk: no Vulkan library found (set %s to override): %w", LibraryEnv, errors.Join(errs...))
}

// LoadInstance binds the instance-level commands for instance.
func LoadInstance(instance VkInstance) error {
	if libHandle == 0 {
		return ErrNotLoaded
	}
	bindInstanceCommands(func(cmd string) uintptr {
		return uintptr(VkGetInstanceProcAddr(instance, cmd))
	})
	return nil
}

// LoadDevice binds device commands through the owning instance's dispatch.
// These entry points can be used with every device created from that instance;
// device-specific entry points cannot safely back the global command table.
func LoadDevice(instance VkInstance) error {
	if VkGetInstanceProcAddr == nil || instance == 0 {
		return errors.New("vk: LoadInstance must run before LoadDevice")
	}
	if boundDeviceInstance == instance {
		return nil
	}
	bindDeviceCommands(func(cmd string) uintptr {
		return uintptr(VkGetInstanceProcAddr(instance, cmd))
	})
	boundDeviceInstance = instance
	return nil
}

// ClearInstance clears instance and device command tables after the last
// instance owner has been destroyed. Global creation commands remain loaded.
func ClearInstance() {
	boundDeviceInstance = 0
	missing := func(string) uintptr { return 0 }
	bindDeviceCommands(missing)
	bindInstanceCommands(missing)
}

// libraryCandidates lists library paths to try in order: the environment
// override first, then the platform's usual names and locations.
func libraryCandidates() []string {
	if p := os.Getenv(LibraryEnv); p != "" {
		return []string{p}
	}
	return platformLibraries()
}

// bind registers a command variable when the driver exposes the entry point,
// and leaves it nil otherwise so callers can test for support. Commands that
// have an allocation-free wrapper in fastcall.go also get their raw address
// recorded.
func bind(fptr any, addr uintptr) {
	if addr == 0 {
		reflect.ValueOf(fptr).Elem().SetZero()
		if slot, ok := fastAddrs[fptr]; ok {
			*slot = 0
		}
		return
	}
	purego.RegisterFunc(fptr, addr)
	if slot, ok := fastAddrs[fptr]; ok {
		*slot = addr
	}
}
