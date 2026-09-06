package audioout

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestCoreAudioEnumeration(t *testing.T) {
	strings := map[uintptr]string{1: "speaker-uid", 2: "Speakers", 3: "mic-uid", 4: "Microphone"}
	released := 0
	a := &coreAudioAPI{
		size: func(id uint32, p *audioPropertyAddress, _ uint32, _ unsafe.Pointer, n *uint32) int32 {
			if id == audioSystemObject && p.Selector == audioPropertyDevices {
				*n = 8
				return 0
			}
			*n = 0
			if (id == 10 && p.Scope == audioScopeOutput) || (id == 20 && p.Scope == audioScopeInput) {
				*n = 4
			}
			return 0
		},
		data: func(id uint32, p *audioPropertyAddress, _ uint32, _ unsafe.Pointer, n *uint32, out unsafe.Pointer) int32 {
			switch p.Selector {
			case audioDefaultOutput:
				*(*uint32)(out) = 10
			case audioDefaultInput:
				*(*uint32)(out) = 20
			case audioPropertyDevices:
				copy(unsafe.Slice((*uint32)(out), 2), []uint32{10, 20})
				*n = 8
			case audioPropertyUID:
				if id == 10 {
					*(*uintptr)(out) = 1
				} else {
					*(*uintptr)(out) = 3
				}
			case audioPropertyName:
				if id == 10 {
					*(*uintptr)(out) = 2
				} else {
					*(*uintptr)(out) = 4
				}
			default:
				t.Fatalf("unexpected selector %#x", p.Selector)
			}
			return 0
		},
		stringLength:  func(id uintptr) int { return len(strings[id]) },
		stringMaxSize: func(n int, _ uint32) int { return n },
		stringCString: func(id uintptr, out *byte, n int, _ uint32) bool {
			copy(unsafe.Slice(out, n), append([]byte(strings[id]), 0))
			return true
		},
		release: func(uintptr) { released++ },
	}
	for _, tc := range []struct {
		input bool
		want  DeviceInfo
	}{{false, DeviceInfo{"speaker-uid", "Speakers", true}}, {true, DeviceInfo{"mic-uid", "Microphone", true}}} {
		got, err := a.devices(tc.input)
		if err != nil || !reflect.DeepEqual(got, []DeviceInfo{tc.want}) {
			t.Fatal(got, err)
		}
	}
	if released != 4 {
		t.Fatal("CF device strings not released", released)
	}
}

func TestQueueOutputFillsAndStops(t *testing.T) {
	oldEnqueue, oldStop, oldDispose := audioQueueEnqueueBuffer, audioQueueStop, audioQueueDispose
	t.Cleanup(func() { audioQueueEnqueueBuffer, audioQueueStop, audioQueueDispose = oldEnqueue, oldStop, oldDispose })
	enqueued, stopped, disposed := 0, 0, 0
	audioQueueEnqueueBuffer = func(uintptr, *audioQueueBuffer, uint32, unsafe.Pointer) int32 { enqueued++; return 0 }
	audioQueueStop = func(_ uintptr, immediate uint32) int32 {
		if immediate != 1 {
			t.Error("asynchronous stop")
		}
		stopped++
		return 0
	}
	audioQueueDispose = func(_ uintptr, immediate uint32) int32 {
		if immediate != 1 {
			t.Error("asynchronous dispose")
		}
		disposed++
		return 0
	}
	d := &Device{queue: 42, callback: func(out []float32) {
		for i := range out {
			out[i] = 0.25
		}
	}}
	devicesMu.Lock()
	d.id = nextID
	nextID++
	devices[d.id] = d
	devicesMu.Unlock()
	data := make([]float32, 8)
	buffer := audioQueueBuffer{AudioData: unsafe.Pointer(&data[0]), AudioDataBytesCapacity: 32}
	ref := d.id
	onQueueOutput(ref, 42, &buffer)
	if buffer.AudioDataByteSize != 32 || data[0] != 0.25 || enqueued != 1 {
		t.Fatal("queue callback did not fill stereo PCM")
	}
	d.Close()
	d.Close()
	onQueueOutput(ref, 42, &buffer)
	if stopped != 1 || disposed != 1 || enqueued != 1 {
		t.Fatal(stopped, disposed, enqueued)
	}
}
