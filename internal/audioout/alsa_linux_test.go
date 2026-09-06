package audioout

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"
)

func TestALSAHintEnumeration(t *testing.T) {
	rows := []map[string]string{
		{"NAME": "default", "DESC": "Desktop\nAudio"},
		{"NAME": "mic", "DESC": "Microphone", "IOID": "Input"},
		{"NAME": "phones", "DESC": "Headphones", "IOID": "Output"},
		{"NAME": "phones", "DESC": "Duplicate", "IOID": "Output"},
		{"DESC": "Unnamed"},
	}
	keys := make([]int, len(rows))
	hints := make([]unsafe.Pointer, len(rows)+1)
	for i := range rows {
		keys[i] = i
		hints[i] = unsafe.Pointer(&keys[i])
	}
	allocated, freed, lists := 0, 0, 0
	var strings [][]byte
	lib := &alsa{
		hints: func(card int32, iface *byte, out *unsafe.Pointer) int32 {
			if card != -1 || cstr(iface) != "pcm" {
				t.Fatal("wrong hint query")
			}
			*out = unsafe.Pointer(&hints[0])
			return 0
		},
		getHint: func(h unsafe.Pointer, key *byte) *byte {
			s, ok := rows[*(*int)(h)][cstr(key)]
			if !ok {
				return nil
			}
			b := append([]byte(s), 0)
			strings = append(strings, b)
			allocated++
			return &b[0]
		},
		free:      func(unsafe.Pointer) { freed++ },
		freeHints: func(unsafe.Pointer) int32 { lists++; return 0 },
	}
	for _, tc := range []struct {
		direction string
		want      []string
	}{{"Output", []string{"default", "phones"}}, {"Input", []string{"default", "mic"}}} {
		got, err := lib.devices(tc.direction)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, d := range got {
			ids = append(ids, d.ID)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Fatal(ids, tc.want)
		}
		if got[0].Name != "Desktop Audio" || !got[0].Default {
			t.Fatal(got[0])
		}
	}
	if freed != allocated || lists != 2 {
		t.Fatal("hint strings/list leaked", allocated, freed, lists)
	}
	runtime.KeepAlive(strings)
}

func TestALSAOpenUsesSelectedPCM(t *testing.T) {
	var names []string
	closed := 0
	storage := 0
	lib := &alsa{
		open: func(pcm *unsafe.Pointer, name *byte, stream, mode int32) int32 {
			names = append(names, cstr(name))
			*pcm = unsafe.Pointer(&storage)
			return 0
		},
		setParams: func(unsafe.Pointer, int32, int32, uint32, uint32, int32, uint32) int32 { return -1 },
		close:     func(unsafe.Pointer) int32 { closed++; return 0 },
		strerror:  func(int32) *byte { return nil },
	}
	for _, id := range []string{"", "hw:CARD=Headset"} {
		if _, err := lib.openPCM(id, sndPCMStreamPlayback, 48000, 2, 40000); err == nil {
			t.Fatal("format failure ignored")
		}
	}
	if !reflect.DeepEqual(names, []string{"default", "hw:CARD=Headset"}) || closed != 2 {
		t.Fatal(names, closed)
	}
}

func TestInvalidDeviceOptionsNeverOpen(t *testing.T) {
	if _, err := OpenDevice("a\x00b", 48000, func([]float32) {}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := OpenCaptureDevice("", 48000, 33, func([]float32) {}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestALSACloseInterruptsNativeIO(t *testing.T) {
	for _, capture := range []bool{false, true} {
		name := "output"
		if capture {
			name = "capture"
		}
		t.Run(name, func(t *testing.T) {
			entered, dropped := make(chan struct{}), make(chan struct{})
			closed, recovered, callbacks := 0, 0, 0
			blockedIO := func(unsafe.Pointer, unsafe.Pointer, uintptr) int {
				close(entered)
				<-dropped
				return -77
			}
			lib := &alsa{
				readi: blockedIO, writei: blockedIO,
				drop:    func(unsafe.Pointer) int32 { close(dropped); return 0 },
				prepare: func(unsafe.Pointer) int32 { recovered++; return -1 },
				resume:  func(unsafe.Pointer) int32 { recovered++; return -1 },
				close:   func(unsafe.Pointer) int32 { closed++; return 0 },
			}
			storage := 0
			stop, done := make(chan struct{}), make(chan struct{})
			var closeDevice func()
			if capture {
				d := &CaptureDevice{Channels: 1, lib: lib, pcm: unsafe.Pointer(&storage), stop: stop, done: done}
				closeDevice = d.Close
				go d.loop(func([]float32) { callbacks++ })
			} else {
				d := &Device{lib: lib, pcm: unsafe.Pointer(&storage), stop: stop, done: done}
				closeDevice = d.Close
				go d.loop(func([]float32) { callbacks++ })
			}
			<-entered
			joined := make(chan struct{})
			go func() { closeDevice(); close(joined) }()
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Fatal("Close waited for blocked I/O without stopping the PCM")
			}
			closeDevice()
			wantCallbacks := 1
			if capture {
				wantCallbacks = 0
			}
			if closed != 1 || recovered != 0 || callbacks != wantCallbacks {
				t.Fatalf("close=%d recover=%d callbacks=%d", closed, recovered, callbacks)
			}
		})
	}
}

func TestALSACloseInterruptsSuspendedRecovery(t *testing.T) {
	for _, capture := range []bool{false, true} {
		name := "output"
		if capture {
			name = "capture"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				resuming := make(chan struct{})
				var notify sync.Once
				resumes, prepares, drops := 0, 0, 0
				lib := &alsa{
					readi:  func(unsafe.Pointer, unsafe.Pointer, uintptr) int { return -int(syscall.ESTRPIPE) },
					writei: func(unsafe.Pointer, unsafe.Pointer, uintptr) int { return -int(syscall.ESTRPIPE) },
					resume: func(unsafe.Pointer) int32 {
						resumes++
						notify.Do(func() { close(resuming) })
						return -int32(syscall.EAGAIN)
					},
					prepare: func(unsafe.Pointer) int32 { prepares++; return 0 },
					drop:    func(unsafe.Pointer) int32 { drops++; return 0 },
					close:   func(unsafe.Pointer) int32 { return 0 },
				}
				storage := 0
				stop, done := make(chan struct{}), make(chan struct{})
				var closeDevice func()
				if capture {
					d := &CaptureDevice{Channels: 1, lib: lib, pcm: unsafe.Pointer(&storage), stop: stop, done: done}
					closeDevice = d.Close
					go d.loop(func([]float32) {})
				} else {
					d := &Device{lib: lib, pcm: unsafe.Pointer(&storage), stop: stop, done: done}
					closeDevice = d.Close
					go d.loop(func([]float32) {})
				}
				<-resuming
				joined := make(chan struct{})
				go func() { closeDevice(); close(joined) }()
				select {
				case <-joined:
				case <-time.After(time.Second):
					t.Fatal("Close could not interrupt suspended recovery")
				}
				if resumes != 1 || prepares != 0 || drops != 1 {
					t.Fatal(resumes, prepares, drops)
				}
				var control sync.Mutex
				if lib.recoverPCM(unsafe.Pointer(&storage), -int(syscall.EPIPE), stop, &control) || prepares != 0 {
					t.Fatal("recovery restarted an already stopped PCM")
				}
			})
		})
	}
}

func TestALSARecoveryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		code                      int
		resume, prepare           int32
		want                      bool
		wantResumes, wantPrepares int
	}{
		{"interrupted", -int(syscall.EINTR), 0, 0, true, 0, 0},
		{"underrun", -int(syscall.EPIPE), 0, 0, true, 0, 1},
		{"prepare_failure", -int(syscall.EPIPE), 0, -1, false, 0, 1},
		{"resume", -int(syscall.ESTRPIPE), 0, 0, true, 1, 0},
		{"resume_fallback", -int(syscall.ESTRPIPE), -1, 0, true, 1, 1},
		{"unhandled", -int(syscall.EIO), 0, 0, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resumes, prepares := 0, 0
			lib := &alsa{
				resume:  func(unsafe.Pointer) int32 { resumes++; return tc.resume },
				prepare: func(unsafe.Pointer) int32 { prepares++; return tc.prepare },
			}
			var control sync.Mutex
			if got := lib.recoverPCM(nil, tc.code, make(chan struct{}), &control); got != tc.want || resumes != tc.wantResumes || prepares != tc.wantPrepares {
				t.Fatal(got, resumes, prepares)
			}
		})
	}
}
