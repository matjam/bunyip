package gfx

import (
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
	proj  lin.Mat4
	frame lin.Vec4 // time, view width, view height, pixels per view unit
}

const push2DSize = 80

// state2D is everything a run of 2D vertices needs to be drawn. Runs
// with equal state become one draw call.
type state2D struct {
	set     vk.VkDescriptorSet // the texture and the shader's images
	shader  *Shader
	uniform int32 // arena offset of the shader's uniforms, -1 for none
	blend   Blend
	clip    lin.Rect
	proj    *lin.Mat4
}

// item2D is a submitted run of vertices with its sort keys.
type item2D struct {
	state        state2D
	first, count int32
	layer        int32
}

type draw2D struct {
	state        state2D
	first, count uint32
}

// stream2D collects a queue's 2D vertices for a frame and turns them into
// draw runs in layer order.
type stream2D struct {
	verts    []vertex2D // as submitted
	items    []item2D
	ordered  []vertex2D // in draw order
	draws    []draw2D
	buffers  [render.FramesInFlight]*render.Buffer
	capacity int
	projs    []lin.Mat4 // projections referenced this frame, at stable addresses
	sorted   bool
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
func (s *stream2D) add(st state2D, layer int32, verts []vertex2D) {
	first := int32(len(s.verts))
	s.verts = append(s.verts, verts...)
	if n := len(s.items); n > 0 {
		last := &s.items[n-1]
		if last.state == st && last.layer == layer && last.first+last.count == first {
			last.count += int32(len(verts))
			return
		}
	}
	s.items = append(s.items, item2D{state: st, first: first, count: int32(len(verts)), layer: layer})
	if layer != 0 {
		s.sorted = false
	}
}

func (s *stream2D) reset() {
	s.verts = s.verts[:0]
	s.items = s.items[:0]
	s.ordered = s.ordered[:0]
	s.draws = s.draws[:0]
	s.projs = s.projs[:0]
	s.sorted = true
}

// build orders items by layer (stable) and groups them into draw runs.
func (s *stream2D) build() {
	if !s.sorted {
		slices.SortStableFunc(s.items, func(x, y item2D) int { return int(x.layer - y.layer) })
		s.sorted = true
	}
	s.ordered = s.ordered[:0]
	s.draws = s.draws[:0]
	for _, it := range s.items {
		if n := len(s.draws); n > 0 && s.draws[n-1].state == it.state {
			s.draws[n-1].count += uint32(it.count)
		} else {
			s.draws = append(s.draws, draw2D{state: it.state, first: uint32(len(s.ordered)), count: uint32(it.count)})
		}
		s.ordered = append(s.ordered, s.verts[it.first:it.first+it.count]...)
	}
}

// upload copies this frame's vertices into the slot's buffer, growing every
// slot's buffer when the stream outgrew them.
func (s *stream2D) upload(dev *render.Device, slot int) error {
	if len(s.ordered) > s.capacity {
		if err := dev.WaitIdle(); err != nil {
			return err
		}
		newCap := max(s.capacity*2, initialVertexCapacity)
		for newCap < len(s.ordered) {
			newCap *= 2
		}
		for i := range s.buffers {
			if s.buffers[i] != nil {
				s.buffers[i].Destroy()
			}
			buf, err := dev.NewBuffer(vk.VkDeviceSize(newCap*vertex2DSize), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
				vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
			if err != nil {
				return err
			}
			s.buffers[i] = buf
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
	if len(verts) == 0 {
		return
	}
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
	if !q.xform.IsIdentity() {
		for i := range verts {
			verts[i].pos = q.xform.Apply(verts[i].pos)
		}
	}
	st := state2D{shader: shader, uniform: shader.uniformOffset(), blend: q.blend, proj: q.stream.proj(q.spriteProj)}
	if n := len(q.clips); n > 0 {
		st.clip = q.clips[n-1]
	}
	if shader != nil && shader.images != [4]*Texture{} {
		st.set = g.imageSet(tex, shader.images)
	} else {
		st.set = tex.setFor(filter)
	}
	q.stream.add(st, q.layer, verts)
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

// quad appends a sprite's two triangles.
func spriteVertices(s Sprite, out []vertex2D) []vertex2D {
	c := s.Color.premultiplied()
	// Corners relative to the origin, then rotated about it.
	ox, oy := s.Origin.X*s.Size.X, s.Origin.Y*s.Size.Y
	x0, y0, x1, y1 := -ox, -oy, s.Size.X-ox, s.Size.Y-oy
	var p [4]lin.Vec2
	if s.Rotation == 0 {
		p = [4]lin.Vec2{{X: s.Pos.X + x0, Y: s.Pos.Y + y0}, {X: s.Pos.X + x1, Y: s.Pos.Y + y0}, {X: s.Pos.X + x1, Y: s.Pos.Y + y1}, {X: s.Pos.X + x0, Y: s.Pos.Y + y1}}
	} else {
		sn, cs := sin32(s.Rotation), cos32(s.Rotation)
		rot := func(x, y float32) lin.Vec2 { return lin.V2(s.Pos.X+x*cs-y*sn, s.Pos.Y+x*sn+y*cs) }
		p = [4]lin.Vec2{rot(x0, y0), rot(x1, y0), rot(x1, y1), rot(x0, y1)}
	}
	uv := [4]lin.Vec2{s.UV0, {X: s.UV1.X, Y: s.UV0.Y}, s.UV1, {X: s.UV0.X, Y: s.UV1.Y}}
	return append(out,
		vertex2D{p[0], uv[0], c}, vertex2D{p[1], uv[1], c}, vertex2D{p[2], uv[2], c},
		vertex2D{p[0], uv[0], c}, vertex2D{p[2], uv[2], c}, vertex2D{p[3], uv[3], c},
	)
}
