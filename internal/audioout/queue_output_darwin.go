package audioout

import (
	"fmt"
	"unsafe"
)

// Named endpoints use an Audio Queue, whose converter preserves the mixer's
// sample rate even when the selected hardware runs at another rate.
func openQueueOutput(id string, rate int, cb Callback) (*Device, error) {
	if err := loadAudioQueue(); err != nil {
		return nil, err
	}
	d := &Device{Rate: rate, callback: cb}
	devicesMu.Lock()
	d.id = nextID
	nextID++
	devices[d.id] = d
	devicesMu.Unlock()
	format := audioStreamBasicDescription{SampleRate: float64(rate), FormatID: kAudioFormatLinearPCM,
		FormatFlags: kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked, BytesPerPacket: 8,
		FramesPerPacket: 1, BytesPerFrame: 8, ChannelsPerFrame: 2, BitsPerChannel: 32}
	if st := audioQueueNewOutput(&format, queueOutputCallback, d.id, 0, 0, 0, &d.queue); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: AudioQueueNewOutput: %d", st)
	}
	if err := setQueueDevice(d.queue, id, false); err != nil {
		d.Close()
		return nil, err
	}
	for range 3 {
		var buf *audioQueueBuffer
		if st := audioQueueAllocBuffer(d.queue, 512*8, &buf); st != 0 {
			d.Close()
			return nil, fmt.Errorf("audioout: allocate output buffer: %d", st)
		}
		out := unsafe.Slice((*float32)(buf.AudioData), int(buf.AudioDataBytesCapacity)/4)
		d.callback(out)
		buf.AudioDataByteSize = uint32(len(out) * 4)
		if st := audioQueueEnqueueBuffer(d.queue, buf, 0, nil); st != 0 {
			d.Close()
			return nil, fmt.Errorf("audioout: enqueue output: %d", st)
		}
	}
	if st := audioQueueStart(d.queue, nil); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: start output queue: %d", st)
	}
	return d, nil
}

func onQueueOutput(id uintptr, queue uintptr, buffer *audioQueueBuffer) {
	devicesMu.Lock()
	d := devices[id]
	devicesMu.Unlock()
	if d == nil || buffer == nil || buffer.AudioData == nil {
		return
	}
	out := unsafe.Slice((*float32)(buffer.AudioData), int(buffer.AudioDataBytesCapacity)/4)
	d.callback(out)
	buffer.AudioDataByteSize = uint32(len(out) * 4)
	d.queueMu.Lock()
	if !d.queueStopped {
		audioQueueEnqueueBuffer(queue, buffer, 0, nil)
	}
	d.queueMu.Unlock()
}
