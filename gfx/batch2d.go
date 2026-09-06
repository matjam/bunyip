package gfx

import (
	"cmp"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// vertex2D is the GPU layout of the 2D stream; see sprite.vert.
type vertex2D struct {
	pos   lin.Vec2
	uv    lin.Vec2
	color [4]float32
}

const vertex2DSize = 32

// push2D is the 2D push-constant block: the projection and frame data.
type push2D struct {
	proj                   lin.Mat4
	frame                  lin.Vec4 // time, view width, view height, pixels per view unit
	transformX, transformY lin.Vec4
}

const push2DSize = 112

// state2D is everything a run of 2D vertices needs to be drawn. Runs
// with equal state become one draw call.
type state2D struct {
	set         vk.VkDescriptorSet // the texture and the shader's images
	shader      *Shader
	uniform     int32 // arena offset of the shader's uniforms, -1 for none
	blend       Blend
	customBlend customBlend
	stencil     stencil2D
	group       uint32
	clip        lin.Rect
	proj        *lin.Mat4
	transform   lin.Affine
	frame       lin.Vec4
}

// item2D is a submitted run of vertices with its sort keys.
type item2D struct {
	state        state2D
	first, count int32
	layer        int32
	key          float32 // order within the layer; equal keys keep submission order
	seq          int32
	geometry     *geometry2DData
	// breaks says an instanced particle batch was submitted just before
	// this run, so it must start a draw of its own however well it
	// matches the one before: the batch is recorded between them.
	breaks bool
}

type draw2D struct {
	state        state2D
	first, count uint32
	// layer is the layer of the run's first item, which is its lowest
	// because items are in layer order by the time draws are built.
	// seq is that item's submission sequence, which orders
	// it against everything else submitted in the same layer. flush2D
	// uses the pair to interleave instanced particles, which carry the
	// same two numbers.
	layer    int32
	seq      int32
	geometry *geometry2DData
}

// stream2D collects a queue's 2D vertices for a frame and turns them into
// draw runs in layer order.
type stream2D struct {
	verts   []vertex2D // as submitted
	items   []item2D
	ordered []vertex2D // in draw order, aliasing verts when the order already matches
	// orderedBuf owns the reordered copy. ordered points at it only when
	// the items had to be reordered, so a frame that draws in submission
	// order uploads straight out of verts.
	orderedBuf []vertex2D
	sortBuf    []item2D // scratch for the counting sort
	counts     []int32  // items per layer, then the start of each layer's run
	draws      []draw2D
	buffers    [render.FramesInFlight]*render.Buffer
	capacity   int
	projs      []lin.Mat4 // projections referenced this frame, at stable addresses
	sorted     bool
	keyed      bool // some item has a sort key, so layers need an inner sort
	// breakRun stops the next run merging into the last one, which the
	// instanced particle path sets so a batch can be recorded between
	// two sprite draws that would otherwise become one.
	breakRun bool
	sequence int32
	group    uint32 // ordered phases, separated by stencil operations
}

const initialVertexCapacity = 6 * 4096

// proj returns a stable pointer to a projection, so states compare by value.
func (s *stream2D) proj(m lin.Mat4) *lin.Mat4 {
	for i := range s.projs {
		if s.projs[i] == m {
			return &s.projs[i]
		}
	}
	if len(s.projs) == cap(s.projs) {
		// Growing would move earlier entries that items already point to.
		grown := make([]lin.Mat4, len(s.projs), max(2*cap(s.projs), 8))
		copy(grown, s.projs)
		for i := range s.items {
			for j := range s.projs {
				if s.items[i].state.proj == &s.projs[j] {
					s.items[i].state.proj = &grown[j]
				}
			}
		}
		s.projs = grown
	}
	s.projs = append(s.projs, m)
	return &s.projs[len(s.projs)-1]
}

// add appends vertices under a state, merging with the previous item
// when nothing changed.
func (s *stream2D) add(st state2D, layer int32, key float32, verts []vertex2D) {
	seq := s.nextSequence()
	first := int32(len(s.verts))
	s.verts = append(s.verts, verts...)
	// A run that would merge across an instanced particle batch has to
	// start afresh instead, or the batch could not be recorded between
	// the two halves and would draw under both.
	breaks := s.breakRun
	s.breakRun = false
	if n := len(s.items); !breaks && n > 0 {
		last := &s.items[n-1]
		if last.geometry == nil && last.state == st && last.layer == layer && last.key == key && last.first+last.count == first {
			last.count += int32(len(verts))
			return
		}
	}
	s.items = append(s.items, item2D{state: st, first: first, count: int32(len(verts)), layer: layer, key: key, breaks: breaks, seq: seq})
	if layer != 0 {
		s.sorted = false
	}
	if key != 0 {
		s.sorted, s.keyed = false, true
	}
}

func (s *stream2D) nextSequence() int32 {
	seq := s.sequence
	s.sequence++
	return seq
}

func (s *stream2D) barrier() {
	s.group++
	s.breakRun = true
}

func (s *stream2D) addGeometry(st state2D, layer int32, key float32, data *geometry2DData) {
	s.items = append(s.items, item2D{state: st, first: int32(len(s.verts)), layer: layer, key: key, seq: s.nextSequence(), geometry: data, breaks: s.breakRun})
	s.breakRun = false
	if layer != 0 {
		s.sorted = false
	}
	if key != 0 {
		s.sorted, s.keyed = false, true
	}
}

func (s *stream2D) reset() {
	s.verts = s.verts[:0]
	s.items = s.items[:0]
	s.ordered = nil // drop any alias of verts before verts is appended to again
	s.orderedBuf = s.orderedBuf[:0]
	s.draws = s.draws[:0]
	s.projs = s.projs[:0]
	s.sorted = true
	s.keyed = false
	s.breakRun = false
	s.sequence = 0
	s.group = 0
}

// maxLayerSpread is how many layers wide a frame may be before the
// counting sort's bucket array costs more than a comparison sort. Games
// use a handful of layers; a frame that spreads them over millions falls
// back to the stable sort.
const maxLayerSpread = 1 << 16

// sortItems puts the items in layer order, keeping submission order
// within a layer. Layers are small integers, so a counting sort over the
// layer range does it in two passes and is stable by construction.
func (s *stream2D) sortItems() {
	if s.group != 0 {
		slices.SortStableFunc(s.items, func(x, y item2D) int {
			if c := cmp.Compare(x.state.group, y.state.group); c != 0 {
				return c
			}
			if c := cmp.Compare(x.layer, y.layer); c != 0 {
				return c
			}
			return cmp.Compare(x.key, y.key)
		})
		return
	}
	lo, hi := s.items[0].layer, s.items[0].layer
	for i := range s.items {
		lo = min(lo, s.items[i].layer)
		hi = max(hi, s.items[i].layer)
	}
	span := int64(hi) - int64(lo) + 1
	if span > maxLayerSpread || span > int64(4*len(s.items)+64) {
		// cmp.Compare, not a subtraction: layers at opposite ends of int32
		// would wrap.
		slices.SortStableFunc(s.items, func(x, y item2D) int {
			if c := cmp.Compare(x.layer, y.layer); c != 0 {
				return c
			}
			return cmp.Compare(x.key, y.key)
		})
		return
	}
	if cap(s.counts) < int(span) {
		s.counts = make([]int32, span)
	}
	counts := s.counts[:span]
	clear(counts)
	for i := range s.items {
		counts[s.items[i].layer-lo]++
	}
	// Prefix sums turn the counts into where each layer's run starts.
	var start int32
	for i := range counts {
		n := counts[i]
		counts[i] = start
		start += n
	}
	if cap(s.sortBuf) < len(s.items) {
		s.sortBuf = make([]item2D, len(s.items))
	}
	out := s.sortBuf[:len(s.items)]
	for i := range s.items {
		k := s.items[i].layer - lo
		out[counts[k]] = s.items[i]
		counts[k]++
	}
	s.items, s.sortBuf = out, s.items[:0]
	if s.keyed {
		// Each layer's run is sorted by key on its own, stably, so the
		// keys order within a layer and submission order breaks ties.
		for i := 0; i < len(s.items); {
			j := i + 1
			for j < len(s.items) && s.items[j].layer == s.items[i].layer {
				j++
			}
			slices.SortStableFunc(s.items[i:j], func(x, y item2D) int { return cmp.Compare(x.key, y.key) })
			i = j
		}
	}
}

// build orders items by layer (stable) and groups them into draw runs.
func (s *stream2D) build() {
	if !s.sorted && len(s.items) > 1 {
		s.sortItems()
	}
	s.sorted = true
	s.draws = s.draws[:0]
	// Items still in submission order cover verts end to end, so the
	// upload can read verts directly instead of copying it.
	inOrder, at := true, int32(0)
	for i := range s.items {
		if s.items[i].first != at {
			inOrder = false
			break
		}
		at += s.items[i].count
	}
	if inOrder && int(at) == len(s.verts) {
		s.ordered = s.verts
		for i := range s.items {
			it := &s.items[i]
			if it.geometry != nil {
				s.draws = append(s.draws, draw2D{state: it.state, count: it.geometry.count, layer: it.layer, seq: it.seq, geometry: it.geometry})
			} else if n := len(s.draws); n > 0 && s.draws[n-1].geometry == nil && !it.breaks && s.draws[n-1].state == it.state {
				s.draws[n-1].count += uint32(it.count)
			} else {
				s.draws = append(s.draws, draw2D{state: it.state, first: uint32(it.first), count: uint32(it.count), layer: it.layer, seq: it.seq})
			}
		}
		return
	}
	s.orderedBuf = s.orderedBuf[:0]
	for i := range s.items {
		it := &s.items[i]
		if it.geometry != nil {
			s.draws = append(s.draws, draw2D{state: it.state, count: it.geometry.count, layer: it.layer, seq: it.seq, geometry: it.geometry})
			continue
		}
		if n := len(s.draws); n > 0 && s.draws[n-1].geometry == nil && !it.breaks && s.draws[n-1].state == it.state {
			s.draws[n-1].count += uint32(it.count)
		} else {
			s.draws = append(s.draws, draw2D{state: it.state, first: uint32(len(s.orderedBuf)), count: uint32(it.count), layer: it.layer, seq: it.seq})
		}
		s.orderedBuf = append(s.orderedBuf, s.verts[it.first:it.first+it.count]...)
	}
	s.ordered = s.orderedBuf
}

// upload copies this frame's vertices into the slot's buffer, growing every
// slot's buffer when the stream outgrew them.
func (s *stream2D) upload(g *Graphics, slot int) error {
	if len(s.ordered) > s.capacity {
		newCap := max(s.capacity*2, initialVertexCapacity)
		for newCap < len(s.ordered) {
			newCap *= 2
		}
		if err := g.growStream(&s.buffers, vk.VkDeviceSize(newCap*vertex2DSize)); err != nil {
			return err
		}
		s.capacity = newCap
	}
	if len(s.ordered) == 0 {
		return nil
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&s.ordered[0])), len(s.ordered)*vertex2DSize)
	return s.buffers[slot].Write(0, data)
}

func (s *stream2D) destroy() {
	for i := range s.buffers {
		if s.buffers[i] != nil {
			s.buffers[i].Destroy()
			s.buffers[i] = nil
		}
	}
}

// vertex2DLayout describes the per-vertex binding.
func vertex2DLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings := []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: vertex2DSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX}}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 8},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 16},
	}
	return bindings, attrs
}

// emit queues 2D triangles under the queue's current shader, blend, clip,
// layer and transform. Positions are in view units (or world units under
// a 2D camera) and pass through the transform stack.
func (g *Graphics) emit(tex *Texture, verts []vertex2D) { g.emitFiltered(tex, verts, FilterDefault) }

// emitFiltered is emit with a per-draw filtering override.
func (g *Graphics) emitFiltered(tex *Texture, verts []vertex2D, filter Filter) {
	g.requireTextureOwner(tex)
	g.requireShaderOwner(g.cur.shader)
	if len(verts) == 0 {
		return
	}
	q := g.cur
	if !q.xform.IsIdentity() {
		for i := range verts {
			verts[i].pos = q.xform.Apply(verts[i].pos)
		}
	}
	q.stream.add(g.state2D(tex, filter), q.layer, q.sortKey, verts)
}

// state2D snapshots shared drawing state for streamed and persistent geometry.
func (g *Graphics) state2D(tex *Texture, filter Filter) state2D {
	g.requireTextureOwner(tex)
	g.requireShaderOwner(g.cur.shader)
	q := g.cur
	if tex == nil {
		tex = g.white
	}
	shader := q.shader
	if shader == nil {
		shader = g.spriteShader
		if tex.sdf {
			shader = g.sdfShader
		}
		if q.colorMatrix != nil && !tex.sdf {
			shader = g.matrixShader
		}
	}
	st := state2D{shader: shader, uniform: shader.uniformOffset(), blend: q.blend, customBlend: q.customBlend, stencil: q.stencil2D, group: q.stream.group, proj: q.stream.proj(q.spriteProj), transform: lin.Identity2(), frame: g.frame2DState()}
	if n := len(q.clips); n > 0 {
		st.clip = q.clips[n-1]
	}
	if shader != nil && shader.images != [4]*Texture{} {
		st.set = g.imageSet(tex, shader.images)
	} else {
		st.set = tex.setFor(filter)
	}
	return st
}

// imageSet returns a descriptor set binding a texture plus a shader's
// extra images, cached per combination.
func (g *Graphics) imageSet(tex *Texture, images [4]*Texture) vk.VkDescriptorSet {
	key := [5]*Texture{tex, images[0], images[1], images[2], images[3]}
	if set, ok := g.imageSets[key]; ok {
		return set
	}
	bindings := make([]render.SamplerBinding, 5)
	for i, t := range key {
		if t == nil {
			t = g.white
		}
		bindings[i] = render.SamplerBinding{View: t.img.View, Sampler: g.sampler(!t.nearest, t.repeat)}
	}
	set, err := g.descriptors.AllocateMany(bindings)
	if err != nil {
		return tex.set
	}
	g.imageSets[key] = set
	return set
}

// textureSet allocates a texture's default descriptor set: itself at
// binding 0 and white in the image slots.
func (g *Graphics) textureSet(view vk.VkImageView, sampler vk.VkSampler) (vk.VkDescriptorSet, error) {
	bindings := make([]render.SamplerBinding, 5)
	bindings[0] = render.SamplerBinding{View: view, Sampler: sampler}
	for i := 1; i < 5; i++ {
		if g.white != nil {
			bindings[i] = render.SamplerBinding{View: g.white.img.View, Sampler: g.nearest}
		} else {
			bindings[i] = bindings[0] // the white texture itself, being created
		}
	}
	return g.descriptors.AllocateMany(bindings)
}

// spriteVertices appends a sprite's two triangles.
func spriteVertices(s Sprite, out []vertex2D) []vertex2D {
	c := s.Color.premultiplied()
	p := s.Corners()
	uv := [4]lin.Vec2{s.UV0, {X: s.UV1.X, Y: s.UV0.Y}, s.UV1, {X: s.UV0.X, Y: s.UV1.Y}}
	return append(out,
		vertex2D{p[0], uv[0], c}, vertex2D{p[1], uv[1], c}, vertex2D{p[2], uv[2], c},
		vertex2D{p[0], uv[0], c}, vertex2D{p[2], uv[2], c}, vertex2D{p[3], uv[3], c},
	)
}
