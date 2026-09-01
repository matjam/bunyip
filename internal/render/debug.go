package render

import (
	"log/slog"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/matjam/bunyip/internal/vk"
)

// debugCallback is created once; purego callbacks are never freed.
var (
	debugCallback uintptr
	debugLog      *slog.Logger
)

func (i *Instance) debugMessengerInfo() vk.VkDebugUtilsMessengerCreateInfoEXT {
	debugLog = i.log
	if debugCallback == 0 {
		debugCallback = purego.NewCallback(onDebugMessage)
	}
	return vk.VkDebugUtilsMessengerCreateInfoEXT{
		SType: vk.VK_STRUCTURE_TYPE_DEBUG_UTILS_MESSENGER_CREATE_INFO_EXT,
		MessageSeverity: vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_VERBOSE_BIT_EXT | vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_INFO_BIT_EXT |
			vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_WARNING_BIT_EXT | vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_ERROR_BIT_EXT,
		MessageType: vk.VK_DEBUG_UTILS_MESSAGE_TYPE_GENERAL_BIT_EXT | vk.VK_DEBUG_UTILS_MESSAGE_TYPE_VALIDATION_BIT_EXT |
			vk.VK_DEBUG_UTILS_MESSAGE_TYPE_PERFORMANCE_BIT_EXT,
		PfnUserCallback: vk.PFN_vkDebugUtilsMessengerCallbackEXT(debugCallback),
	}
}

// onDebugMessage is called by the validation layer on its own thread; it
// only logs, which slog handles safely.
func onDebugMessage(severity vk.VkDebugUtilsMessageSeverityFlagsEXT, _ vk.VkDebugUtilsMessageTypeFlagsEXT,
	data *vk.VkDebugUtilsMessengerCallbackDataEXT, _ unsafe.Pointer) vk.VkBool32 {
	if data == nil || debugLog == nil {
		return vk.VK_FALSE
	}
	msg := cString(data.PMessage)
	id := cString(data.PMessageIdName)
	switch {
	case severity&vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_ERROR_BIT_EXT != 0:
		debugLog.Error("vulkan: "+msg, "id", id)
	case severity&vk.VK_DEBUG_UTILS_MESSAGE_SEVERITY_WARNING_BIT_EXT != 0 && id != "Loader Message":
		debugLog.Warn("vulkan: "+msg, "id", id)
	default:
		// Loader and driver chatter (INFO and VERBOSE) only shows at debug level.
		debugLog.Debug("vulkan: "+msg, "id", id)
	}
	return vk.VK_FALSE
}

// cString copies a NUL-terminated C string.
func cString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
