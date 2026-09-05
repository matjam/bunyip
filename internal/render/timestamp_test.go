package render

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// These tests replace a Vulkan entry point and must not run in parallel.
func TestTimestampReadback(t *testing.T) {
	original := vk.VkGetQueryPoolResults
	t.Cleanup(func() { vk.VkGetQueryPoolResults = original })
	for _, tt := range []struct {
		name    string
		mask    uint64
		results []uint64 // alternating counter and availability
		result  vk.VkResult
		want    []Span
		frameMS float64
	}{
		{
			name: "positive counters sum repeated passes and include gaps",
			mask: ^uint64(0), results: []uint64{100, 1, 120, 1, 130, 1, 160, 1},
			want: []Span{{Name: "opaque", MS: 0.5}}, frameMS: 0.6,
		},
		{
			name: "available equal counters retain zero duration passes",
			mask: ^uint64(0), results: []uint64{700, 1, 700, 1, 700, 1, 700, 1},
			want: []Span{{Name: "opaque", MS: 0}},
		},
		{
			name: "available zero counters are not unavailable",
			mask: ^uint64(0), results: []uint64{0, 1, 0, 1, 0, 1, 0, 1},
			want: []Span{{Name: "opaque", MS: 0}},
		},
		{
			name: "ignore bits outside the queue family counter width",
			mask: 0xff, results: []uint64{0xff64, 1, 0x78, 1, 0x82, 1, 0xffa0, 1},
			want: []Span{{Name: "opaque", MS: 0.5}}, frameMS: 0.6,
		},
		{
			name: "not ready still publishes complete pairs",
			mask: ^uint64(0), results: []uint64{100, 1, 120, 1, 130, 1, 160, 0},
			result: vk.VK_NOT_READY,
			want:   []Span{{Name: "opaque", MS: 0.2}}, frameMS: 0.2,
		},
		{
			name: "either unavailable endpoint omits the pass",
			mask: ^uint64(0), results: []uint64{100, 0, 120, 1, 130, 1, 160, 0},
			result: vk.VK_NOT_READY, want: []Span{},
		},
		{
			name: "readback error preserves previous figures",
			mask: ^uint64(0), results: []uint64{100, 1, 120, 1, 130, 1, 160, 1},
			result: vk.VK_ERROR_DEVICE_LOST,
			want:   []Span{{Name: "previous", MS: 7}}, frameMS: 7,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Use the second frame slot to check absolute query indices and
			// the result/availability stride, not just the counter arithmetic.
			times := &Timestamps{
				dev: &Device{}, perSlot: 2 * maxSpans, mask: tt.mask, msPerTick: 0.01,
				last: []Span{{Name: "previous", MS: 7}}, frameMS: 7,
			}
			base := times.perSlot
			times.slots[1] = slotSpans{next: base + 4, spans: []pendingSpan{
				{name: "opaque", a: base, b: base + 1, closed: true},
				{name: "opaque", a: base + 2, b: base + 3, closed: true},
				{name: "unclosed", a: base, b: base + 1},
			}}
			called := false
			vk.VkGetQueryPoolResults = func(_ vk.VkDevice, _ vk.VkQueryPool, first, count uint32, size uintptr, data unsafe.Pointer, stride vk.VkDeviceSize, flags vk.VkQueryResultFlags) vk.VkResult {
				called = true
				if first != base || count != 4 || size != 64 || stride != 16 || flags != vk.VK_QUERY_RESULT_64_BIT|vk.VK_QUERY_RESULT_WITH_AVAILABILITY_BIT {
					t.Fatalf("incorrect readback: first=%d count=%d size=%d stride=%d flags=%#x", first, count, size, stride, flags)
				}
				copy(unsafe.Slice((*uint64)(data), 8), tt.results)
				return tt.result
			}
			times.publish(1)
			if !called {
				t.Fatal("timestamp queries were not read")
			}
			if !reflect.DeepEqual(times.Spans(), tt.want) || times.FrameMS() != tt.frameMS {
				t.Fatalf("spans=%v frame=%g, want spans=%v frame=%g", times.Spans(), times.FrameMS(), tt.want, tt.frameMS)
			}
		})
	}
}

func TestTimestampsUnavailable(t *testing.T) {
	var times *Timestamps
	times.Reset(0, 0)
	times.Begin(0, "opaque")
	times.End(0)
	times.Destroy()
	if times.Spans() != nil || times.FrameMS() != 0 {
		t.Fatal("a device without timestamp queries must report no spans and zero frame time")
	}
}
