package platform

// NativeParent borrows a parent; backend values match the root NativeBackend.
type NativeParent struct {
	Backend uint8
	Handle  uintptr
}
