package gfx

import (
	"math"
	"slices"
	"unsafe"
)

// The 3D draw order is a 64-bit key per draw, so ordering a frame's
// draws sorts a slice of integers instead of comparing draw records.
// The fields sit in the key in the order they are compared:
//
//	bit 63     blended: every opaque draw before any blended one
//	bit 62     culled: what the camera sees before what it does not
//	blended    bits 61..30 the view depth, farthest first
//	opaque     bit 61 clear for a draw that writes the stencil buffer, so
//	           a mask is drawn before whatever tests it; bit 60 skinned,
//	           then the shader, the shader's uniforms, the material set
//	           and the mesh, each as a dense id
//	bits 19..0 the draw's place in the queue, so that draws which tie
//	           keep the order the game queued them in
//
// The ids are handed out in the order the draws are walked, so the
// groups come out in a different order than a comparison sort on the
// pointers would give. Draws that share a state still land together,
// which is what the instanced runs need.
//
// A frame with more distinct shaders, uniform blocks, material sets or
// meshes than a field holds, or with more than sortMaxDraws draws, is
// ordered by comparing the records instead.
const (
	sortBlendedBit   = 63
	sortCulledBit    = 62
	sortStencilBit   = 61
	sortSkinnedBit   = 60
	sortDepthShift   = 30
	sortShaderShift  = 53
	sortShaderBits   = 7
	sortUniformShift = 46
	sortUniformBits  = 7
	sortSetShift     = 33
	sortSetBits      = 13
	sortMeshShift    = 20
	sortMeshBits     = 13
	sortIndexBits    = 20
	sortIndexMask    = 1<<sortIndexBits - 1
	sortMaxDraws     = 1 << sortIndexBits
)

// depthBits maps a depth to an unsigned integer that orders the same
// way, negative depths included.
func depthBits(f float32) uint32 {
	b := math.Float32bits(f)
	if b&0x8000_0000 != 0 {
		return ^b
	}
	return b | 0x8000_0000
}

// buildKeys fills the queue's sort keys from its draws. It reports
// whether every dense id fitted its field; when one does not, the keys
// are incomplete and the caller falls back to comparing the records.
func (q *drawQueue) buildKeys() bool {
	n := len(q.draws)
	if n > sortMaxDraws {
		return false
	}
	if cap(q.keys) < n {
		q.keys = make([]uint64, n)
	}
	q.keys = q.keys[:n]
	q.shaderIDs.reset(1<<sortShaderBits - 1)
	q.uniformIDs.reset(1<<sortUniformBits - 1)
	q.setIDs.reset(1<<sortSetBits - 1)
	q.meshIDs.reset(1<<sortMeshBits - 1)
	for i := range q.draws {
		d := &q.draws[i]
		key := uint64(i)
		if d.culled {
			key |= 1 << sortCulledBit
		}
		if d.blended {
			key |= 1<<sortBlendedBit | uint64(^depthBits(d.depth))<<sortDepthShift
		} else {
			// A draw that marks the stencil buffer comes first, so a draw
			// that tests the mark sees it however the two were queued.
			if !d.mat.marksStencil() {
				key |= 1 << sortStencilBit
			}
			if d.skinned {
				key |= 1 << sortSkinnedBit
			}
			key |= uint64(q.shaderIDs.id(uintptr(unsafe.Pointer(d.shader)))) << sortShaderShift
			// A uniform offset is -1 for none, so it is shifted past zero,
			// which the tables use for an empty slot.
			key |= uint64(q.uniformIDs.id(uintptr(uint32(d.uniform))+2)) << sortUniformShift
			key |= uint64(q.setIDs.id(uintptr(d.set))) << sortSetShift
			key |= uint64(q.meshIDs.id(uintptr(unsafe.Pointer(d.mesh)))) << sortMeshShift
		}
		q.keys[i] = key
	}
	return !q.shaderIDs.overflow && !q.uniformIDs.overflow && !q.setIDs.overflow && !q.meshIDs.overflow
}

// sortDraws puts the queue's draws in the order they are recorded in:
// opaque before blended, what the camera sees before what it does not,
// blended draws back to front, and opaque draws grouped by the state a
// run of instances must share. It leaves q.draws where they are and
// orders a permutation instead. Each draw's blended, culled and depth
// fields must already be resolved.
func (q *drawQueue) sortDraws() drawList {
	n := len(q.draws)
	if cap(q.order) < n {
		q.order = make([]int32, n)
	}
	q.order = q.order[:n]
	if !q.buildKeys() {
		return q.sortRecords()
	}
	slices.Sort(q.keys)
	for i, key := range q.keys {
		q.order[i] = int32(key & sortIndexMask)
	}
	return drawList{draws: q.draws, order: q.order}
}

// sortRecords is sortDraws by comparing the draw records themselves. It
// runs when a frame holds more distinct shaders, uniform blocks,
// material sets or meshes than the packed key has room for.
func (q *drawQueue) sortRecords() drawList {
	for i := range q.order {
		q.order[i] = int32(i)
	}
	draws := q.draws
	slices.SortStableFunc(q.order, func(x, y int32) int {
		a, b := &draws[x], &draws[y]
		switch {
		case a.blended != b.blended:
			if a.blended {
				return 1
			}
			return -1
		case a.culled != b.culled: // what the camera sees first
			if a.culled {
				return 1
			}
			return -1
		case a.blended: // farthest first
			if a.depth > b.depth {
				return -1
			}
			if a.depth < b.depth {
				return 1
			}
			return 0
		case a.mat.marksStencil() != b.mat.marksStencil(): // a mask before what tests it
			if a.mat.marksStencil() {
				return -1
			}
			return 1
		case a.skinned != b.skinned:
			if a.skinned {
				return 1
			}
			return -1
		case a.shader != b.shader:
			if uintptr(unsafe.Pointer(a.shader)) < uintptr(unsafe.Pointer(b.shader)) {
				return -1
			}
			return 1
		case a.uniform != b.uniform:
			return int(a.uniform - b.uniform)
		case a.set != b.set:
			if a.set < b.set {
				return -1
			}
			return 1
		case a.mesh != b.mesh:
			if uintptr(unsafe.Pointer(a.mesh)) < uintptr(unsafe.Pointer(b.mesh)) {
				return -1
			}
			return 1
		}
		return 0
	})
	return drawList{draws: draws, order: q.order}
}

// idTable hands out dense ids for the pointers and handles that go into
// a sort key. It is an open-addressed table of power-of-two size, kept
// between frames and emptied by reset. A key of zero marks a free slot,
// so callers shift values that can be zero. Draws usually arrive
// grouped by mesh and material, so the last answer is checked first.
type idTable struct {
	slots    []idSlot
	mask     uintptr
	n        uint32
	limit    uint32 // the largest id the key's field holds
	last     uintptr
	lastID   uint32
	hasLast  bool
	overflow bool // an id past limit was asked for
}

type idSlot struct {
	key uintptr
	id  uint32
}

// reset empties the table for a frame whose key field holds ids up to
// limit.
func (t *idTable) reset(limit uint32) {
	if t.slots == nil {
		t.slots = make([]idSlot, 256)
		t.mask = 255
	} else {
		clear(t.slots)
	}
	t.n, t.limit, t.hasLast, t.overflow = 0, limit, false, false
}

// id returns key's dense id, giving it the next one when it is new.
func (t *idTable) id(key uintptr) uint32 {
	if t.hasLast && key == t.last {
		return t.lastID
	}
	i := t.slot(key)
	id := t.slots[i].id
	if t.slots[i].key == 0 {
		if t.n >= t.limit {
			t.overflow = true
			return 0
		}
		t.n++
		id = t.n
		t.slots[i] = idSlot{key: key, id: id}
		if uintptr(t.n)*4 >= uintptr(len(t.slots))*3 {
			t.grow()
		}
	}
	t.last, t.lastID, t.hasLast = key, id, true
	return id
}

// slot is the index key lives in, or the free slot it would take.
func (t *idTable) slot(key uintptr) uintptr {
	i := uintptr(uint64(key)*0x9E3779B97F4A7C15>>32) & t.mask
	for t.slots[i].key != 0 && t.slots[i].key != key {
		i = (i + 1) & t.mask
	}
	return i
}

// grow doubles the table and rehashes it.
func (t *idTable) grow() {
	old := t.slots
	t.slots = make([]idSlot, len(old)*2)
	t.mask = uintptr(len(t.slots) - 1)
	for _, s := range old {
		if s.key != 0 {
			t.slots[t.slot(s.key)] = s
		}
	}
}
