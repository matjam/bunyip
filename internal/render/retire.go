package render

// deferred is one retired object: the frame it was retired in and what
// frees it once no frame in flight can still reference it.
type deferred struct {
	frame   uint64
	destroy func()
}

// Retire schedules fn to free a GPU object once every frame that may
// still reference it has finished. Use it instead of WaitIdle for
// anything a recorded frame has already bound: a replaced buffer, a
// descriptor pool whose sets a submitted frame holds. The frame ring
// runs fn from a later BeginFrame, after that frame's fence has been
// waited on, so retiring costs no stall. A device with no frames running
// frees everything retired when it is destroyed.
func (d *Device) Retire(fn func()) {
	if fn == nil {
		return
	}
	d.retired = append(d.retired, deferred{frame: d.frameNo, destroy: fn})
}

// RetireBuffers schedules a ring of per-frame buffers for destruction
// once no frame in flight can still reference them. A stream that
// outgrew its buffers allocates a new ring and retires the old one, so
// growing costs no wait. Nil entries are ignored.
func (d *Device) RetireBuffers(bufs [FramesInFlight]*Buffer) {
	empty := true
	for _, b := range bufs {
		if b != nil {
			empty = false
		}
	}
	if empty {
		return
	}
	d.Retire(func() {
		for _, b := range bufs {
			if b != nil {
				b.Destroy()
			}
		}
	})
}

// nextFrame counts a frame and frees what is now safe to free. The
// renderer calls it from BeginFrame once the slot's fence has been
// waited on, which is when the work submitted FramesInFlight frames ago
// has finished.
func (d *Device) nextFrame() {
	d.frameNo++
	keep := d.retired[:0]
	for _, r := range d.retired {
		if r.frame+FramesInFlight <= d.frameNo {
			r.destroy()
			continue
		}
		keep = append(keep, r)
	}
	d.retired = keep
}

// flushRetired frees everything still pending. The caller must have
// waited for the device.
func (d *Device) flushRetired() {
	for _, r := range d.retired {
		r.destroy()
	}
	d.retired = nil
}
