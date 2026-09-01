package vk

import "fmt"

// MakeAPIVersion packs a version the way VK_MAKE_API_VERSION does.
func MakeAPIVersion(variant, major, minor, patch uint32) uint32 {
	return variant<<29 | major<<22 | minor<<12 | patch
}

// Version components, as VK_API_VERSION_MAJOR and friends.
func VersionMajor(v uint32) uint32 { return v >> 22 & 0x7f }
func VersionMinor(v uint32) uint32 { return v >> 12 & 0x3ff }
func VersionPatch(v uint32) uint32 { return v & 0xfff }

// FormatVersion renders a packed version as major.minor.patch.
func FormatVersion(v uint32) string {
	return fmt.Sprintf("%d.%d.%d", VersionMajor(v), VersionMinor(v), VersionPatch(v))
}

var (
	API_VERSION_1_0 = MakeAPIVersion(0, 1, 0, 0)
	API_VERSION_1_1 = MakeAPIVersion(0, 1, 1, 0)
	API_VERSION_1_2 = MakeAPIVersion(0, 1, 2, 0)
	API_VERSION_1_3 = MakeAPIVersion(0, 1, 3, 0)
)

// Error wraps a failing VkResult with the command that produced it.
type Error struct {
	Command string
	Result  VkResult
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Command, e.Result)
}

// Check converts a VkResult into an error: nil for VK_SUCCESS and the other
// non-negative results, an *Error otherwise.
func Check(command string, r VkResult) error {
	if r >= 0 {
		return nil
	}
	return &Error{Command: command, Result: r}
}

// CString returns a NUL-terminated byte slice's pointer for a Go string, for
// fields Vulkan reads as const char*. Keep the returned slice alive across the
// call that uses it.
func CString(s string) (*byte, []byte) {
	b := append([]byte(s), 0)
	return &b[0], b
}

// GoString reads a NUL-terminated char array such as VkPhysicalDeviceProperties.DeviceName.
func GoString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
