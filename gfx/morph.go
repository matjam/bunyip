package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// Morph targets blend on the GPU when few enough of them are active. The
// deltas of every target of every mesh in a model go into one storage
// buffer at load, six floats to a vertex of a target: the position delta
// then the normal delta. A draw then names up to MaxGPUMorphTargets of
// them in its instance record, with the weight each carries, and the
// vertex prelude adds them before skinning. Nothing is uploaded when the
// weights change, so a face can be driven every frame for the cost of a
// few reads a vertex.
//
// A mesh with more active targets than the cap falls back to the blend
// on the processor, which uploads the result: correct at any count, and
// what a model whose targets did not fit in the buffer uses as well.

// MaxGPUMorphTargets is how many of a mesh's morph targets can carry a
// weight at once before the blend moves back to the processor. Eight is
// what the instance record holds, and more than a face usually needs at
// one time: a mesh with twenty targets is fine so long as no more than
// eight of them are open.
const MaxGPUMorphTargets = 8

// morphFloats is the floats one vertex of one target takes in the
// buffer, matching MORPH_STRIDE in vert_common.wgsl.
const morphFloats = 6

// morphStore is a model's morph target deltas: one device buffer holding
// every one of its morph meshes' blocks, and the descriptor set the mesh
// pipelines bind as set 5.
type morphStore struct {
	buf  *render.Buffer
	set  vk.VkDescriptorSet
	g    *Graphics
	data []float32 // built while the model loads, then uploaded and dropped
}

// reserve adds one mesh's targets to the store and returns the element
// the block starts at. The deltas are laid out target by target, and
// within a target vertex by vertex, which is the order the shader
// indexes them in.
func (s *morphStore) reserve(mm *morphMesh) int {
	base := len(s.data)
	count := mm.vertices()
	for _, t := range mm.targets {
		for i := range count {
			var pos, normal [3]float32
			if i < len(t.Positions) {
				p := t.Positions[i]
				pos = [3]float32{p.X, p.Y, p.Z}
			}
			if i < len(t.Normals) {
				n := t.Normals[i]
				normal = [3]float32{n.X, n.Y, n.Z}
			}
			s.data = append(s.data, pos[0], pos[1], pos[2], normal[0], normal[1], normal[2])
		}
	}
	return base
}

// upload puts the collected deltas on the device and makes the set the
// draws bind. It is called once, when the model has finished loading; a
// model with no deltas gets no buffer and its draws bind the empty set.
func (s *morphStore) upload() error {
	if len(s.data) == 0 {
		return nil
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&s.data[0])), len(s.data)*4)
	buf, err := s.g.r.Device.NewDeviceLocalBuffer(bytes, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT)
	if err != nil {
		return err
	}
	set, err := s.g.meshes.morphDesc.AllocateBuffer(buf, vk.VkDeviceSize(len(bytes)))
	if err != nil {
		buf.Destroy()
		return err
	}
	s.buf, s.set = buf, set
	s.data = nil
	return nil
}

// destroy retires the buffer and its set together, after queued and
// submitted draws have finished reading them. Capture both resources
// before clearing the store so repeated destruction is harmless.
func (s *morphStore) destroy() {
	if s == nil || s.buf == nil {
		return
	}
	buf, set, desc := s.buf, s.set, s.g.meshes.morphDesc
	s.buf, s.set = nil, 0
	s.g.deferDestroy(func() {
		desc.Free(set)
		buf.Destroy()
	})
}

// vertices is how many vertices a morph mesh has, which is the stride
// between one target's deltas and the next.
func (mm *morphMesh) vertices() int {
	if mm.skin != nil {
		return len(mm.skin)
	}
	return len(mm.base)
}

// set records the weights a draw of this mesh uses. It picks the largest
// weights that are not zero, up to the cap, and leaves them for the
// instance stream; with more than the cap it blends on the processor and
// uploads instead, and with the GPU path taking over from the processor
// it restores the rest geometry first, since the shader adds its deltas
// to whatever is uploaded.
func (mm *morphMesh) set(weights []float32) error {
	mm.current = append(mm.current[:0], weights...)
	for len(mm.current) < len(mm.targets) {
		mm.current = append(mm.current, 0)
	}
	mm.active = mm.active[:0]
	if mm.gpuBase >= 0 {
		for i, w := range weights {
			if i >= len(mm.targets) || i > 255 || w == 0 {
				continue
			}
			mm.active = append(mm.active, morphWeight{target: uint8(i), weight: w})
		}
		if len(mm.active) <= MaxGPUMorphTargets {
			if mm.blended {
				// The uploaded geometry is a blend from before; put the rest
				// pose back, since the shader adds to what it is given.
				if err := mm.apply(make([]float32, len(mm.targets))); err != nil {
					return err
				}
				mm.blended = false
			}
			return nil
		}
	}
	mm.active = mm.active[:0]
	mm.blended = true
	return mm.apply(weights)
}

// orSet is a draw's own descriptor set, or the fallback when it has none.
func orSet(set, fallback vk.VkDescriptorSet) vk.VkDescriptorSet {
	if set == 0 {
		return fallback
	}
	return set
}

// morphWeight is one active target of a draw.
type morphWeight struct {
	target uint8
	weight float32
}

// morphDraw is the immutable morph block captured when a draw is queued.
type morphDraw struct {
	info    [4]float32
	weights [MaxGPUMorphTargets]float32
	indices [2]uint32
}

// snapshot captures both the shader weights and the uploaded geometry.
// The original mesh owns the buffers and retires them after pending draws;
// this cached view only keeps their values. Unchanged GPU geometry reuses
// the view, so changing shader weights does not allocate per draw or frame.
func (mm *morphMesh) snapshot(d *meshDraw) {
	if mm == nil {
		return
	}
	m := mm.mesh
	if old := mm.drawn; old == nil || old.vbuf != m.vbuf || old.ibuf != m.ibuf ||
		old.Min != m.Min || old.Max != m.Max || old.boundsFixed != m.boundsFixed {
		view := *m
		mm.drawn = &view
	}
	d.mesh = mm.drawn
	if len(mm.active) == 0 {
		return
	}
	d.morph.info = [4]float32{float32(mm.gpuBase), float32(mm.vertices()), float32(len(mm.active)), 0}
	for k, a := range mm.active {
		d.morph.weights[k] = a.weight
		d.morph.indices[k/4] |= uint32(a.target) << (8 * (k % 4))
	}
}

func (d morphDraw) instance(in *meshInstance) {
	in.morph, in.morphW, in.morphIdx = d.info, d.weights, d.indices
}
