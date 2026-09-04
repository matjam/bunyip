package audioout

import (
	"fmt"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Capture through an Audio Queue, which is the smallest input path
// AudioToolbox offers: the queue owns its own thread, hands back a
// buffer of recorded frames and takes it again when the callback
// returns. This compiles and is written against the AudioToolbox
// headers, but it has not yet been run on a Mac.
//
// macOS asks the user for microphone access the first time a process
// records. A process that has not been granted it gets a queue that
// starts and delivers silence, and a sandboxed application needs the
// audio-input entitlement, so a game that records says so in its
// Info.plist.

// audioQueueBuffer is AudioQueueBuffer from AudioQueue.h. Go's own
// alignment puts every field where the C compiler does.
type audioQueueBuffer struct {
	_                         structs.HostLayout
	AudioDataBytesCapacity    uint32
	AudioData                 unsafe.Pointer
	AudioDataByteSize         uint32
	UserData                  unsafe.Pointer
	PacketDescriptionCapacity uint32
	PacketDescriptions        unsafe.Pointer
	PacketDescriptionCount    uint32
}

var (
	captureOnce             sync.Once
	captureErr              error
	audioQueueNewInput      func(format *audioStreamBasicDescription, callback uintptr, userData unsafe.Pointer, runLoop uintptr, runLoopMode uintptr, flags uint32, queue *uintptr) int32
	audioQueueAllocBuffer   func(queue uintptr, size uint32, buffer **audioQueueBuffer) int32
	audioQueueEnqueueBuffer func(queue uintptr, buffer *audioQueueBuffer, packets uint32, descs unsafe.Pointer) int32
	audioQueueStart         func(queue uintptr, startTime unsafe.Pointer) int32
	audioQueueStop          func(queue uintptr, immediate uint32) int32
	audioQueueDispose       func(queue uintptr, immediate uint32) int32
	inputCallback           uintptr

	// captures lets the C callback find its device without passing a Go
	// pointer through C memory, as the output side does.
	capturesMu    sync.Mutex
	captures              = map[uintptr]*CaptureDevice{}
	nextCaptureID uintptr = 1
)

func loadAudioQueue() error {
	captureOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			captureErr = fmt.Errorf("audioout: load AudioToolbox: %w", err)
			return
		}
		purego.RegisterLibFunc(&audioQueueNewInput, lib, "AudioQueueNewInput")
		purego.RegisterLibFunc(&audioQueueAllocBuffer, lib, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&audioQueueEnqueueBuffer, lib, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&audioQueueStart, lib, "AudioQueueStart")
		purego.RegisterLibFunc(&audioQueueStop, lib, "AudioQueueStop")
		purego.RegisterLibFunc(&audioQueueDispose, lib, "AudioQueueDispose")
		inputCallback = purego.NewCallback(onCapture)
	})
	return captureErr
}

// CaptureDevice is a running input audio queue.
type CaptureDevice struct {
	Rate     int
	Channels int
	queue    uintptr
	id       uintptr
	callback CaptureCallback

	mu      sync.Mutex
	stopped bool
}

// captureBuffers is how many buffers the queue cycles through, and
// captureFrames how many frames each holds: about 10 ms at 48 kHz, so
// the game hears itself without a noticeable lag.
const (
	captureBuffers = 3
	captureFrames  = 512
)

// OpenCapture starts recording interleaved float32 at rate from the
// default input, calling cb from the queue's own thread.
func OpenCapture(rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	if err := loadAudioQueue(); err != nil {
		return nil, err
	}
	bytesPerFrame := uint32(4 * channels)
	format := audioStreamBasicDescription{
		SampleRate: float64(rate), FormatID: kAudioFormatLinearPCM,
		FormatFlags:    kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
		BytesPerPacket: bytesPerFrame, FramesPerPacket: 1, BytesPerFrame: bytesPerFrame,
		ChannelsPerFrame: uint32(channels), BitsPerChannel: 32,
	}
	d := &CaptureDevice{Rate: rate, Channels: channels, callback: cb}
	capturesMu.Lock()
	d.id = nextCaptureID
	nextCaptureID++
	captures[d.id] = d
	capturesMu.Unlock()
	// A nil run loop and mode put the callback on a thread of the
	// queue's own, which is what a game wants.
	if st := audioQueueNewInput(&format, inputCallback, *(*unsafe.Pointer)(unsafe.Pointer(&d.id)), 0, 0, 0, &d.queue); st != 0 {
		d.forget()
		return nil, fmt.Errorf("audioout: AudioQueueNewInput: %d", st)
	}
	for range captureBuffers {
		var buf *audioQueueBuffer
		if st := audioQueueAllocBuffer(d.queue, captureFrames*bytesPerFrame, &buf); st != 0 {
			d.Close()
			return nil, fmt.Errorf("audioout: AudioQueueAllocateBuffer: %d", st)
		}
		if st := audioQueueEnqueueBuffer(d.queue, buf, 0, nil); st != 0 {
			d.Close()
			return nil, fmt.Errorf("audioout: AudioQueueEnqueueBuffer: %d", st)
		}
	}
	if st := audioQueueStart(d.queue, nil); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: AudioQueueStart: %d", st)
	}
	return d, nil
}

// Close stops recording and releases the queue.
func (d *CaptureDevice) Close() {
	d.mu.Lock()
	stopped, queue := d.stopped, d.queue
	d.stopped, d.queue = true, 0
	d.mu.Unlock()
	if !stopped && queue != 0 {
		audioQueueStop(queue, 1)
		audioQueueDispose(queue, 1)
	}
	d.forget()
}

func (d *CaptureDevice) forget() {
	capturesMu.Lock()
	delete(captures, d.id)
	capturesMu.Unlock()
}

// onCapture runs on the queue's thread with a buffer of recorded frames.
// It hands the samples to the device's callback and gives the buffer
// straight back, unless the device has been closed.
func onCapture(userData unsafe.Pointer, queue uintptr, buffer *audioQueueBuffer, _ unsafe.Pointer, _ uint32, _ unsafe.Pointer) {
	id := *(*uintptr)(unsafe.Pointer(&userData))
	capturesMu.Lock()
	d := captures[id]
	capturesMu.Unlock()
	if d == nil || buffer == nil {
		return
	}
	if buffer.AudioData != nil && buffer.AudioDataByteSize >= 4 {
		d.callback(unsafe.Slice((*float32)(buffer.AudioData), int(buffer.AudioDataByteSize)/4))
	}
	d.mu.Lock()
	stopped := d.stopped
	d.mu.Unlock()
	if !stopped {
		audioQueueEnqueueBuffer(queue, buffer, 0, nil)
	}
}
