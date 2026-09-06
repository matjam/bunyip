package audioout

import (
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWASAPIEndpointEnumerationAndSelection(t *testing.T) {
	oldFree, oldClear := freeCOMMemory, clearProperty
	t.Cleanup(func() { freeCOMMemory, clearProperty = oldFree, oldClear })
	freed, cleared, released := 0, 0, 0
	freeCOMMemory = func(uintptr) { freed++ }
	clearProperty = func(*propVariant) { cleared++ }
	makeObject := func() *comObject {
		o := &comObject{vtbl: new([64]uintptr)}
		o.vtbl[2] = syscall.NewCallback(func(uintptr) uintptr { released++; return 0 })
		return o
	}
	id, _ := syscall.UTF16FromString("opaque-endpoint")
	name, _ := syscall.UTF16FromString("USB Headset")
	props := makeObject()
	props.vtbl[5] = syscall.NewCallback(func(_ uintptr, key, out unsafe.Pointer) uintptr {
		if *(*propertyKey)(key) != friendlyNameKey {
			t.Error("wrong property")
		}
		v := (*propVariant)(out)
		v.Kind = 31
		v.Data[0] = uintptr(unsafe.Pointer(&name[0]))
		return 0
	})
	dev := makeObject()
	dev.vtbl[5] = syscall.NewCallback(func(_ uintptr, out unsafe.Pointer) uintptr {
		*(*unsafe.Pointer)(out) = unsafe.Pointer(&id[0])
		return 0
	})
	dev.vtbl[4] = syscall.NewCallback(func(_ uintptr, mode uintptr, out unsafe.Pointer) uintptr {
		if mode != 0 {
			t.Error("property store writable")
		}
		*(**comObject)(out) = props
		return 0
	})
	collection := makeObject()
	collection.vtbl[3] = syscall.NewCallback(func(_ uintptr, out unsafe.Pointer) uintptr { *(*uint32)(out) = 1; return 0 })
	collection.vtbl[4] = syscall.NewCallback(func(_ uintptr, i uintptr, out unsafe.Pointer) uintptr {
		if i != 0 {
			t.Error("wrong index")
		}
		*(**comObject)(out) = dev
		return 0
	})
	enum := makeObject()
	enum.vtbl[3] = syscall.NewCallback(func(_ uintptr, flow, state uintptr, out unsafe.Pointer) uintptr {
		if flow != eRender || state != 1 {
			t.Error("wrong endpoint filter")
		}
		*(**comObject)(out) = collection
		return 0
	})
	enum.vtbl[4] = syscall.NewCallback(func(_ uintptr, flow, role uintptr, out unsafe.Pointer) uintptr {
		if flow != eRender || role != eConsole {
			t.Error("wrong default role")
		}
		*(**comObject)(out) = dev
		return 0
	})
	enum.vtbl[5] = syscall.NewCallback(func(_ uintptr, selected, out unsafe.Pointer) uintptr {
		if wideString((*uint16)(selected)) != "opaque-endpoint" {
			t.Error("selection ID changed")
		}
		*(**comObject)(out) = dev
		return 0
	})
	got, err := enumerateEndpoints(enum, eRender)
	if err != nil || !reflect.DeepEqual(got, []DeviceInfo{{ID: "opaque-endpoint", Name: "USB Headset", Default: true}}) {
		t.Fatal(got, err)
	}
	selected, err := endpoint(enum, "opaque-endpoint", eRender)
	if err != nil || selected != dev {
		t.Fatal(selected, err)
	}
	selected.release()
	if freed != 2 || cleared != 1 || released != 5 {
		t.Fatal("COM cleanup", freed, cleared, released)
	}
	runtime.KeepAlive(id)
	runtime.KeepAlive(name)
}

func TestCOMCallKeepsPointerArgumentsAlive(t *testing.T) {
	o := &comObject{vtbl: new([64]uintptr)}
	var grow func(int) int
	grow = func(depth int) int {
		var pad [1024]byte
		pad[depth%len(pad)] = byte(depth)
		if depth == 0 {
			runtime.GC()
			return int(pad[0])
		}
		return grow(depth-1) + int(pad[depth%len(pad)])
	}
	o.vtbl[3] = syscall.NewCallback(func(_ uintptr, out unsafe.Pointer) uintptr {
		grow(64)
		*(*uint64)(out) = 0x123456789abcdef0
		return 0
	})
	var value uint64
	o.call(3, uintptr(unsafe.Pointer(&value)))
	if value != 0x123456789abcdef0 {
		t.Fatalf("pointer argument lost after GC/stack growth: %#x", value)
	}
}

func TestWASAPIRejectsFailedInitializationAndPrime(t *testing.T) {
	oldEnum, oldCreate, oldClose := createWASAPIEnumerator, createAudioEvent, closeAudioEvent
	t.Cleanup(func() { createWASAPIEnumerator, createAudioEvent, closeAudioEvent = oldEnum, oldCreate, oldClose })
	for _, failure := range []string{"size_error", "size_zero", "padding_error", "padding_overflow", "get_buffer", "null_buffer", "release_buffer", "start"} {
		t.Run(failure, func(t *testing.T) {
			const failed = uintptr(0x80004005)
			releases := map[string]int{}
			object := func(name string) *comObject {
				o := &comObject{vtbl: new([64]uintptr)}
				o.vtbl[2] = syscall.NewCallback(func(uintptr) uintptr { releases[name]++; return 0 })
				return o
			}
			render := object("render")
			data := make([]float32, 8)
			render.vtbl[3] = syscall.NewCallback(func(_ uintptr, _ uintptr, out unsafe.Pointer) uintptr {
				if failure == "get_buffer" {
					return failed
				}
				if failure != "null_buffer" {
					*(**float32)(out) = &data[0]
				}
				return 0
			})
			render.vtbl[4] = syscall.NewCallback(func(uintptr, uintptr, uintptr) uintptr {
				if failure == "release_buffer" {
					return failed
				}
				return 0
			})
			client := object("client")
			client.vtbl[3] = syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 })
			client.vtbl[4] = syscall.NewCallback(func(_ uintptr, out unsafe.Pointer) uintptr {
				if failure == "size_error" {
					return failed
				}
				if failure != "size_zero" {
					*(*uint32)(out) = 4
				}
				return 0
			})
			client.vtbl[6] = syscall.NewCallback(func(_ uintptr, out unsafe.Pointer) uintptr {
				if failure == "padding_error" {
					return failed
				}
				if failure == "padding_overflow" {
					*(*uint32)(out) = 5
				}
				return 0
			})
			starts := 0
			client.vtbl[10] = syscall.NewCallback(func(uintptr) uintptr { starts++; return failed })
			client.vtbl[13] = syscall.NewCallback(func(uintptr, uintptr) uintptr { return 0 })
			client.vtbl[14] = syscall.NewCallback(func(_ uintptr, _ unsafe.Pointer, out unsafe.Pointer) uintptr { *(**comObject)(out) = render; return 0 })
			dev := object("device")
			dev.vtbl[3] = syscall.NewCallback(func(_ uintptr, _ unsafe.Pointer, _ uintptr, _ unsafe.Pointer, out unsafe.Pointer) uintptr {
				*(**comObject)(out) = client
				return 0
			})
			enum := object("enum")
			enum.vtbl[4] = syscall.NewCallback(func(_ uintptr, _, _ uintptr, out unsafe.Pointer) uintptr { *(**comObject)(out) = dev; return 0 })
			createWASAPIEnumerator = func() (*comObject, error) { return enum, nil }
			events, closed := 0, 0
			createAudioEvent = func() uintptr { events++; return 77 }
			closeAudioEvent = func(e uintptr) {
				if e != 77 {
					t.Error(e)
				}
				closed++
			}
			d, err := openWASAPI("", 48000, func(out []float32) { clear(out) })
			if d != nil || err == nil {
				t.Fatal("failed replacement accepted", d, err)
			}
			if releases["client"] != 1 || releases["device"] != 1 || releases["enum"] != 1 || closed != events {
				t.Fatal("initialization leaked resources", releases, events, closed)
			}
			if events == 1 && releases["render"] != 1 {
				t.Fatal("prime failure leaked render client", releases)
			}
			if failure != "start" && starts != 0 {
				t.Fatal("started after failed prime")
			}
			runtime.KeepAlive(data)
		})
	}
}
