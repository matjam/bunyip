package render

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Span is one section of a frame's GPU work and how long the GPU spent
// in it.
type Span struct {
	Name string
	MS   float64
}

// maxSpans is how many sections one frame may time. A frame that opens
// more drops the rest rather than growing the pool, so the query count
// is fixed for the life of the device.
const maxSpans = 64

// Timestamps times a frame's passes on the GPU with timestamp queries.
// To use one, call Reset at the start of each frame slot's command
// buffer and wrap each pass in Begin and End; Spans reports the newest
// frame whose results have arrived. Results are read back at the slot's
// next Reset, by which point the slot's fence has been waited on, so a
// frame never stalls for them and the figures lag the frame on screen by
// FramesInFlight. Sections with the same name are summed, so a pass that
// runs once per render texture reads as one entry.
//
// NewTimestamps returns nil on a device without timestamp queries, and
// every method here is safe on a nil receiver, so a caller keeps the
// value and gets empty results.
type Timestamps struct {
	dev       *Device
	pool      vk.VkQueryPool
	perSlot   uint32  // queries reserved for each frame slot
	mask      uint64  // the valid bits of a counter reading
	msPerTick float64 // milliseconds one counter tick covers

	slots [FramesInFlight]slotSpans
	cur   int                // the slot being recorded
	cb    vk.VkCommandBuffer // the buffer that reset that slot, and so the only one that may write
	open  []int              // the spans Begin opened and End has not closed, innermost last

	results []uint64 // the readback buffer, reused every frame
	last    []Span   // the newest results
	frameMS float64  // the first span's start to the last span's end
}

// slotSpans is what one frame slot recorded, waiting for its results.
type slotSpans struct {
	spans   []pendingSpan
	next    uint32 // the next unused query in this slot's range
	pending bool   // the slot has written queries that have not been read
}

// pendingSpan is one section's pair of queries, absolute in the pool.
type pendingSpan struct {
	name   string
	a, b   uint32
	closed bool // End wrote the second timestamp
}

// NewTimestamps makes a query pool for timing the frame's passes, or
// returns nil when the device cannot time them: a zero timestamp period,
// or a queue family with no valid timestamp bits. Query pool creation
// failure also returns nil. The caller keeps a nil value and every
// method does nothing. Available queries may still report equal counters
// and zero durations, including on MoltenVK without Metal counter sampling.
func (d *Device) NewTimestamps() *Timestamps {
	period := float64(d.gpu.props.Limits.TimestampPeriod)
	bits := d.timestampValidBits()
	if period <= 0 || bits == 0 {
		d.log.Info("render: GPU timestamps unavailable", "timestampPeriod", period, "validBits", bits)
		return nil
	}
	t := &Timestamps{dev: d, perSlot: 2 * maxSpans, msPerTick: period / 1e6}
	t.mask = ^uint64(0)
	if bits < 64 {
		t.mask = (uint64(1) << bits) - 1
	}
	info := vk.VkQueryPoolCreateInfo{
		SType:      vk.VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO,
		QueryType:  vk.VK_QUERY_TYPE_TIMESTAMP,
		QueryCount: t.perSlot * FramesInFlight,
	}
	if err := vk.Check("vkCreateQueryPool", vk.VkCreateQueryPool(d.Handle, &info, nil, &t.pool)); err != nil {
		d.log.Warn("render: GPU timestamps unavailable", "err", err)
		return nil
	}
	return t
}

// timestampValidBits is how many bits of a timestamp the device's queue
// family fills in; zero means the queue cannot time anything.
func (d *Device) timestampValidBits() uint32 {
	var count uint32
	vk.VkGetPhysicalDeviceQueueFamilyProperties(d.gpu.handle, &count, nil)
	if count == 0 || d.QueueFamily >= count {
		return 0
	}
	families := make([]vk.VkQueueFamilyProperties, count)
	vk.VkGetPhysicalDeviceQueueFamilyProperties(d.gpu.handle, &count, &families[0])
	return families[d.QueueFamily].TimestampValidBits
}

// Reset publishes the results the slot's last frame left, then records
// the reset of that slot's queries into cb. Every frame must call it
// before the first Begin, at the top of the frame's command buffer.
func (t *Timestamps) Reset(cb vk.VkCommandBuffer, slot int) {
	if t == nil {
		return
	}
	t.cur, t.cb = slot, cb
	t.open = t.open[:0]
	s := &t.slots[slot]
	if s.pending {
		t.publish(slot)
		s.pending = false
	}
	s.spans = s.spans[:0]
	s.next = uint32(slot) * t.perSlot
	vk.VkCmdResetQueryPool(cb, t.pool, uint32(slot)*t.perSlot, t.perSlot)
}

// Begin starts timing a section named name. Pairs nest, so a pass may
// time the parts inside it. A frame that has used all its queries
// records nothing and the matching End does nothing.
//
// A command buffer that has not called Reset records nothing either, so
// work recorded outside the frame ring, such as a probe bake on a
// one-shot buffer, neither writes into queries it never reset nor
// consumes the open frame's. A one-shot buffer's handle cannot equal a
// frame's, because a frame's is allocated for the life of the device.
func (t *Timestamps) Begin(cb vk.VkCommandBuffer, name string) {
	if t == nil || cb != t.cb {
		return
	}
	s := &t.slots[t.cur]
	base := uint32(t.cur) * t.perSlot
	if s.next+2 > base+t.perSlot {
		t.open = append(t.open, -1)
		return
	}
	q := s.next
	s.next += 2
	s.spans = append(s.spans, pendingSpan{name: name, a: q, b: q + 1})
	s.pending = true
	t.open = append(t.open, len(s.spans)-1)
	vk.VkCmdWriteTimestamp2(cb, vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT, t.pool, q)
}

// End closes the innermost open section. Calling it without a matching
// Begin, or on a buffer that has not called Reset, does nothing.
func (t *Timestamps) End(cb vk.VkCommandBuffer) {
	if t == nil || cb != t.cb || len(t.open) == 0 {
		return
	}
	i := t.open[len(t.open)-1]
	t.open = t.open[:len(t.open)-1]
	if i < 0 {
		return
	}
	s := &t.slots[t.cur]
	sp := &s.spans[i]
	sp.closed = true
	vk.VkCmdWriteTimestamp2(cb, vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT, t.pool, sp.b)
}

// Spans is the newest frame's sections, in the order they were opened,
// summed by name. The slice is reused every time results arrive, so copy
// it to keep it. A span with equal available counters is retained with
// zero duration; it is distinct from an unavailable query, which is omitted.
func (t *Timestamps) Spans() []Span {
	if t == nil {
		return nil
	}
	return t.last
}

// FrameMS is the GPU time from the first timed section's start to the
// last one's end, which is the whole frame when the outermost sections
// cover it. It is zero until results arrive, and also when all available
// counters are equal because of the device's timestamp resolution.
func (t *Timestamps) FrameMS() float64 {
	if t == nil {
		return 0
	}
	return t.frameMS
}

// publish reads a slot's counters and turns them into spans. Results
// that have not landed are skipped rather than waited for, so the
// figures stand still rather than stalling the frame.
func (t *Timestamps) publish(slot int) {
	s := &t.slots[slot]
	base := uint32(slot) * t.perSlot
	n := s.next - base
	if n == 0 {
		t.last, t.frameMS = t.last[:0], 0
		return
	}
	need := int(n) * 2 // a result and its availability, both 64 bits
	if cap(t.results) < need {
		t.results = make([]uint64, need)
	}
	t.results = t.results[:need]
	res := vk.VkGetQueryPoolResults(t.dev.Handle, t.pool, base, n,
		uintptr(need*8), unsafe.Pointer(&t.results[0]), 16,
		vk.VK_QUERY_RESULT_64_BIT|vk.VK_QUERY_RESULT_WITH_AVAILABILITY_BIT)
	if res != vk.VK_SUCCESS && res != vk.VK_NOT_READY {
		return // the previous frame's figures stay
	}
	t.last = t.last[:0]
	var lo, hi uint64
	have := false
	for _, sp := range s.spans {
		if !sp.closed {
			continue
		}
		ai, bi := (sp.a-base)*2, (sp.b-base)*2
		if t.results[ai+1] == 0 || t.results[bi+1] == 0 {
			continue // the counter has not been written back yet
		}
		a, b := t.results[ai]&t.mask, t.results[bi]&t.mask
		if b < a {
			continue // the counter wrapped inside the section
		}
		ms := float64(b-a) * t.msPerTick
		at := -1
		for i := range t.last {
			if t.last[i].Name == sp.name {
				at = i
				break
			}
		}
		if at >= 0 {
			t.last[at].MS += ms
		} else {
			t.last = append(t.last, Span{Name: sp.name, MS: ms})
		}
		if !have || a < lo {
			lo = a
		}
		if !have || b > hi {
			hi = b
		}
		have = true
	}
	t.frameMS = 0
	if have {
		t.frameMS = float64(hi-lo) * t.msPerTick
	}
}

// Destroy frees the query pool.
func (t *Timestamps) Destroy() {
	if t == nil || t.pool == 0 {
		return
	}
	vk.VkDestroyQueryPool(t.dev.Handle, t.pool, nil)
	t.pool = 0
}
