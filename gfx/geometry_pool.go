package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// Mesh.Update replaces a mesh's vertex and index buffers every time it is
// called, and a mesh a game rebuilds each frame (a soft body, a cloth, a
// morph blended on the processor) would otherwise allocate two buffers
// and free two a frame. The replaced buffers go to a pool instead, and
// the next update of the same size takes them back and writes into them.
//
// A replaced buffer may still be read by a frame in flight, so it waits
// on its frame slot's list until that slot's fence has been waited on,
// exactly as a destroyed resource does, and only then joins the pool.
// That makes the pool at least FramesInFlight+1 buffers deep for a mesh
// updated every frame, which is where it settles.

// geometryIdleFrames is how many frames a pooled buffer may go unused
// before it is freed, so geometry that stops changing gives its memory
// back.
const geometryIdleFrames = 120

// geometryPoolMax bounds the buffers the pool keeps.
const geometryPoolMax = 64

// pooledBuffer is a device-local vertex or index buffer no frame reads.
type pooledBuffer struct {
	buf   *render.Buffer
	usage vk.VkBufferUsageFlags
	since uint64 // the frame it joined the pool
}

// geometryPool is the Graphics' spare mesh buffers.
type geometryPool struct {
	free []pooledBuffer
	// retired holds, by frame slot, buffers an update replaced that a
	// frame in flight may still read.
	retired [render.FramesInFlight][]pooledBuffer
	// slot is the slot of the last frame begun, where an update outside
	// a frame retires what it replaced: that frame is the last to read it.
	slot int
	// scratch is where Update packs vertices for the GPU, reused.
	scratch []gpuVertex
}

// take returns a pooled buffer with the usage that holds size bytes, the
// smallest that does, or nil. A buffer much larger than asked for is left
// for a larger update, so a small mesh does not hold a large one's memory.
func (p *geometryPool) take(size vk.VkDeviceSize, usage vk.VkBufferUsageFlags) *render.Buffer {
	best := -1
	for i, e := range p.free {
		if e.usage != usage || e.buf.Size < size || e.buf.Size > max(2*size, size+64<<10) {
			continue
		}
		if best < 0 || e.buf.Size < p.free[best].buf.Size {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	buf := p.free[best].buf
	last := len(p.free) - 1
	p.free[best] = p.free[last]
	p.free[last] = pooledBuffer{}
	p.free = p.free[:last]
	return buf
}

// retire puts a replaced buffer on a slot's list, for the pool once the
// slot's fence has been waited on.
func (p *geometryPool) retire(slot int, buf *render.Buffer, usage vk.VkBufferUsageFlags) {
	if buf != nil {
		p.retired[slot] = append(p.retired[slot], pooledBuffer{buf: buf, usage: usage})
	}
}

// begin runs at a frame's start, after the slot's fence has been waited
// on: the slot's retired buffers join the pool, and buffers idle too long
// are freed.
func (p *geometryPool) begin(slot int, frame uint64) {
	p.slot = slot
	for _, e := range p.retired[slot] {
		e.since = frame
		p.free = append(p.free, e)
	}
	clear(p.retired[slot])
	p.retired[slot] = p.retired[slot][:0]
	keep := p.free[:0]
	for i, e := range p.free {
		if frame-e.since > geometryIdleFrames || len(p.free)-i > geometryPoolMax {
			e.buf.Destroy()
			continue
		}
		keep = append(keep, e)
	}
	clear(p.free[len(keep):])
	p.free = keep
}

// destroy frees every buffer. The device must be idle.
func (p *geometryPool) destroy() {
	for _, e := range p.free {
		e.buf.Destroy()
	}
	for slot := range p.retired {
		for _, e := range p.retired[slot] {
			e.buf.Destroy()
		}
		p.retired[slot] = nil
	}
	p.free, p.scratch = nil, nil
}

// packVertices packs vertices for the GPU into the pool's scratch and
// returns its bytes, which are only valid until the next call.
func (g *Graphics) packVertices(verts []Vertex) []byte {
	p := &g.geometry
	p.scratch = grow(p.scratch, len(verts))
	for i, v := range verts {
		p.scratch[i] = v.gpu()
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&p.scratch[0])), len(verts)*vertexSize)
}

// geometryBuffer returns a device-local buffer holding data: a pooled one
// written in place when one fits, or a new one.
func (g *Graphics) geometryBuffer(data []byte, usage vk.VkBufferUsageFlags) (*render.Buffer, error) {
	buf := g.geometry.take(vk.VkDeviceSize(len(data)), usage)
	if buf == nil {
		return g.uploadGeometry(data, usage)
	}
	var err error
	if fr := g.frame; fr != nil {
		var staging *render.Buffer
		var offset vk.VkDeviceSize
		if staging, offset, err = g.stage(data); err == nil {
			render.RecordBufferUpload(fr.CB, buf, staging, offset, vk.VkDeviceSize(len(data)))
		}
	} else {
		err = g.r.Device.UploadToBuffer(buf, data)
	}
	if err != nil {
		// Nothing was recorded into it, so it can go straight back.
		g.geometry.free = append(g.geometry.free, pooledBuffer{buf: buf, usage: usage, since: g.frameNo})
		return nil, err
	}
	return buf, nil
}

// recycleGeometry hands buffers an update replaced to the pool once no
// frame in flight reads them: the frame being recorded, or outside a
// frame the last one begun.
func (g *Graphics) recycleGeometry(vbuf, ibuf *render.Buffer) {
	slot := g.geometry.slot
	if fr := g.frame; fr != nil {
		slot = fr.Slot
	}
	g.geometry.retire(slot, vbuf, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT)
	g.geometry.retire(slot, ibuf, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT)
}
