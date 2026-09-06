// Package render is the Vulkan backend: instance, device, swapchain, frame
// pacing, resource uploads and readback. It knows nothing about windows
// beyond a VkSurfaceKHR handed to it, and nothing about what is drawn.
package render

import (
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Config selects instance-level behaviour.
type Config struct {
	AppName    string
	Validation bool // enable VK_LAYER_KHRONOS_validation and debug messages when available
	Log        *slog.Logger
}

// Instance owns the VkInstance and, when validation is on, the debug messenger.
type Instance struct {
	Handle     vk.VkInstance
	log        *slog.Logger
	messenger  vk.VkDebugUtilsMessengerEXT
	debugUtils bool
	owner      *instanceOwner
	extensions []string
}

// NewInstance creates an instance with the given surface extensions, adding
// portability enumeration (needed to see MoltenVK) and debug utils when
// the loader offers them.
func createInstance(cfg Config, surfaceExts []string) (*Instance, error) {
	if err := vk.Load(); err != nil {
		return nil, err
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	if cfg.Validation {
		vk.PrepareLayers()
	}
	available, err := enumerateInstanceExtensions()
	if err != nil {
		return nil, err
	}
	layers, err := enumerateLayers()
	if err != nil {
		return nil, err
	}
	inst := &Instance{log: log}
	success := false
	defer func() {
		if !success && inst.Handle != 0 {
			inst.destroyInstance()
		}
	}()
	exts := slices.Clone(surfaceExts)
	var flags vk.VkInstanceCreateFlags
	if slices.Contains(available, vk.VK_KHR_PORTABILITY_ENUMERATION_EXTENSION_NAME) {
		exts = append(exts, vk.VK_KHR_PORTABILITY_ENUMERATION_EXTENSION_NAME)
		flags |= vk.VK_INSTANCE_CREATE_ENUMERATE_PORTABILITY_BIT_KHR
	}
	var enabledLayers []string
	if cfg.Validation {
		if slices.Contains(layers, validationLayer) {
			enabledLayers = append(enabledLayers, validationLayer)
		} else {
			log.Warn("render: validation requested but VK_LAYER_KHRONOS_validation is not installed")
		}
		if slices.Contains(available, vk.VK_EXT_DEBUG_UTILS_EXTENSION_NAME) {
			exts = append(exts, vk.VK_EXT_DEBUG_UTILS_EXTENSION_NAME)
			inst.debugUtils = true
		}
	}
	for _, e := range surfaceExts {
		if !slices.Contains(available, e) {
			return nil, fmt.Errorf("render: instance extension %s is not available", e)
		}
	}
	inst.extensions = slices.Clone(exts)

	appName := newCStrings([]string{cfg.AppName, "bunyip"})
	extNames := newCStrings(exts)
	layerNames := newCStrings(enabledLayers)
	app := vk.VkApplicationInfo{
		SType:            vk.VK_STRUCTURE_TYPE_APPLICATION_INFO,
		PApplicationName: appName.ptrs[0],
		PEngineName:      appName.ptrs[1],
		ApiVersion:       vk.API_VERSION_1_3,
	}
	info := vk.VkInstanceCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
		Flags:                   flags,
		PApplicationInfo:        &app,
		EnabledLayerCount:       layerNames.count(),
		PpEnabledLayerNames:     layerNames.first(),
		EnabledExtensionCount:   extNames.count(),
		PpEnabledExtensionNames: extNames.first(),
	}
	var debugInfo vk.VkDebugUtilsMessengerCreateInfoEXT
	if inst.debugUtils {
		debugInfo = inst.debugMessengerInfo()
		info.PNext = unsafe.Pointer(&debugInfo) // covers create/destroy of the instance itself
	}
	if err := vk.Check("vkCreateInstance", vk.VkCreateInstance(&info, nil, &inst.Handle)); err != nil {
		return nil, err
	}
	runtime.KeepAlive(appName)
	runtime.KeepAlive(extNames)
	runtime.KeepAlive(layerNames)
	if err := vk.LoadInstance(inst.Handle); err != nil {
		return nil, err
	}
	if inst.debugUtils {
		if err := vk.Check("vkCreateDebugUtilsMessengerEXT",
			vk.VkCreateDebugUtilsMessengerEXT(inst.Handle, &debugInfo, nil, &inst.messenger)); err != nil {
			return nil, err
		}
	}
	log.Debug("render: instance created", "extensions", exts, "layers", enabledLayers)
	success = true
	return inst, nil
}

// Destroy releases the instance. Every object created from it must already be gone.
func (i *Instance) destroyInstance() {
	if i.messenger != 0 {
		vk.VkDestroyDebugUtilsMessengerEXT(i.Handle, i.messenger, nil)
	}
	vk.VkDestroyInstance(i.Handle, nil)
	i.Handle = 0
}

const validationLayer = "VK_LAYER_KHRONOS_validation"

func enumerateInstanceExtensions() ([]string, error) {
	var count uint32
	if err := vk.Check("vkEnumerateInstanceExtensionProperties", vk.VkEnumerateInstanceExtensionProperties(nil, &count, nil)); err != nil {
		return nil, err
	}
	props := make([]vk.VkExtensionProperties, max(count, 1))
	if count > 0 {
		if err := vk.Check("vkEnumerateInstanceExtensionProperties", vk.VkEnumerateInstanceExtensionProperties(nil, &count, &props[0])); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, count)
	for _, p := range props[:count] {
		names = append(names, vk.GoString(p.ExtensionName[:]))
	}
	return names, nil
}

func enumerateLayers() ([]string, error) {
	var count uint32
	if err := vk.Check("vkEnumerateInstanceLayerProperties", vk.VkEnumerateInstanceLayerProperties(&count, nil)); err != nil {
		return nil, err
	}
	props := make([]vk.VkLayerProperties, max(count, 1))
	if count > 0 {
		if err := vk.Check("vkEnumerateInstanceLayerProperties", vk.VkEnumerateInstanceLayerProperties(&count, &props[0])); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, count)
	for _, p := range props[:count] {
		names = append(names, vk.GoString(p.LayerName[:]))
	}
	return names, nil
}
