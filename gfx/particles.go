package gfx

import (
	"cmp"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// ParticleQuad is one particle handed to the instanced draw path: where
// it is, how big it is, which part of the texture it shows and what
// colour it is tinted. DrawParticles and DrawParticles3D take a whole
// slice of them and draw it as one instanced call, so hundreds of
// thousands cost one draw rather than one draw each.
//
// The struct is the GPU's instance layout, so a slice of them uploads
// without being converted or copied field by field. Keep it that way:
// the particle package fills a slice of these directly. Color is
// straight alpha, not premultiplied, and the shader premultiplies it.
type ParticleQuad struct {
	// Pos is the quad's centre: view units for DrawParticles, world
	// units for DrawParticles3D, whose Z is ignored in 2D.
	Pos lin.Vec3
	// Rotation turns the quad about its centre, in radians. In 3D it
	// turns about the axis facing the camera.
	Rotation float32
	// Size is the width and height. Zero draws nothing.
	Size lin.Vec2
	// UV0 and UV1 are the texture's top-left and bottom-right corners,
	// so one atlas serves a whole effect. Both zero shows the top-left
	// texel alone; use Region.UV0 and Region.UV1, or (0,0) and (1,1).
	UV0, UV1 lin.Vec2
	// Color tints the texture. Zero is transparent, not white, because
	// this is a raw GPU record rather than an options struct.
	Color Color
}

// particleQuadSize is the instance stride; the layout below depends on it.
const particleQuadSize = 56

// Particles3D is how a batch of 3D particles is drawn.
type Particles3D struct {
	// Blend combines the particles with the scene. Zero is alpha
	// blending; BlendAdd suits fire, sparks and magic.
	Blend Blend
	// Soft fades a particle out over this many world units as it
	// approaches the geometry behind it, which hides the hard line a
	// quad otherwise cuts where it meets the ground. Zero is a hard
	// edge. One or two units suits smoke.
	Soft float32
}

// particleBatch is one instanced draw: a run of the queue's quads with
// the texture, blend and layer they were submitted with.
type particleBatch struct {
	set   vk.VkDescriptorSet
	blend Blend
	layer int32
	// seq is how far the 2D stream had got when the batch was
	// submitted, which orders it against the sprite draws of the same
	// layer: a batch drawn before a sprite in the game's code is drawn
	// before it on screen, as it would be through the sprite path.
	seq  int32
	soft float32
	// proj is the 2D projection in force when the batch was submitted,
	// held by value because the stream's projection table is rebuilt
	// each frame and moves.
	proj         lin.Mat4
	first, count uint32
}

// particleStream collects a queue's particle instances for a frame. The
// 2D and 3D batches index the same buffer, so one upload serves both.
type particleStream struct {
	quads    []ParticleQuad
	flat     []particleBatch // 2D, drawn with the sprite stream
	scene    []particleBatch // 3D, drawn over the finished scene
	buffers  [render.FramesInFlight]*render.Buffer
	capacity int
	slot     int
	// prepared says the frame's instances are uploaded and the 2D
	// batches are in layer order. The 3D pass and the 2D flush both ask
	// for it, and the first one to run does the work.
	prepared bool
}

func (s *particleStream) reset() {
	s.quads = s.quads[:0]
	s.flat = s.flat[:0]
	s.scene = s.scene[:0]
	s.prepared = false
}

// add appends a run of quads and returns the batch covering it.
func (s *particleStream) add(quads []ParticleQuad) (first, count uint32) {
	first = uint32(len(s.quads))
	s.quads = append(s.quads, quads...)
	return first, uint32(len(quads))
}

// upload copies this frame's instances into the slot's buffer, growing
// every slot's buffer when the stream outgrew them.
func (s *particleStream) upload(g *Graphics, slot int) error {
	s.slot = slot
	if len(s.quads) > s.capacity {
		newCap := max(s.capacity*2, 4096)
		for newCap < len(s.quads) {
			newCap *= 2
		}
		if err := g.growStream(&s.buffers, vk.VkDeviceSize(newCap*particleQuadSize)); err != nil {
			return err
		}
		s.capacity = newCap
	}
	if len(s.quads) == 0 {
		return nil
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&s.quads[0])), len(s.quads)*particleQuadSize)
	return s.buffers[slot].Write(0, data)
}

func (s *particleStream) destroy() {
	for i := range s.buffers {
		if s.buffers[i] != nil {
			s.buffers[i].Destroy()
			s.buffers[i] = nil
		}
	}
}

// particlePass owns the instanced particle pipelines, one per blend mode
// built on first use, and the push blocks the recording commands take by
// pointer.
type particlePass struct {
	// Both are built per blend mode and then per output, since a render
	// texture chooses its own colour format and depth and the scene may
	// be multisampled.
	flat  [blendCount]*pipeCache // 2D, into the sprite stream's pass
	scene [blendCount]*pipeCache // 3D, over the finished scene
	push  push2D
	push3 particle3DPush
	set   vk.VkDescriptorSet
	off   vk.VkDeviceSize
}

// particle3DPush is the push block of the 3D particle pipeline. It
// carries the camera's basis rather than reading the shared Frame block,
// so the particle path adds no field to it.
type particle3DPush struct {
	viewProj lin.Mat4
	right    lin.Vec4 // xyz the camera's right axis, w the camera's x
	up       lin.Vec4 // xyz the camera's up axis, w the camera's y
	params   lin.Vec4 // near, far, soft fade distance, the camera's z
	mode     lin.Vec4 // x is 1 for an orthographic camera, 0 for a perspective one
}

// particle3DPushSize is the guaranteed minimum push-constant size, which
// the decal pipeline already spends in full.
const particle3DPushSize = 128

// particleLayout describes the per-instance binding both pipelines read.
func particleLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings := []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: particleQuadSize, InputRate: vk.VK_VERTEX_INPUT_RATE_INSTANCE}}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32_SFLOAT, Offset: 12},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 16},
		{Location: 3, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
		{Location: 4, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 32},
		{Location: 5, Binding: 0, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 40},
	}
	return bindings, attrs
}

// flatPipeline returns the 2D particle pipeline for a blend mode and an
// output, building it on first use. It writes the same formats as the
// sprite pipelines, because it records into the same pass.
func (g *Graphics) flatPipeline(b Blend, out outKey) (*render.Pipeline, error) {
	if g.particles.flat[b] == nil {
		bindings, attrs := particleLayout()
		c, err := newPipeCache(g.r.Device, render.PipelineDesc{
			Vert: shaders.ParticleVert, Frag: shaders.ParticleFrag,
			ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
			Bindings: bindings, Attributes: attrs,
			Blend: true, Factors: b.factors(),
			PushConstantSize: push2DSize,
			SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout},
		})
		if err != nil {
			return nil, err
		}
		g.particles.flat[b] = c
	}
	return g.particles.flat[b].at(out)
}

// scenePipeline returns the 3D particle pipeline for a blend mode and an
// output. It has no depth attachment: the pass it records into leaves the
// depth image readable, and the fragment program tests against it by hand.
func (g *Graphics) scenePipeline(b Blend, out outKey) (*render.Pipeline, error) {
	if g.particles.scene[b] == nil {
		bindings, attrs := particleLayout()
		c, err := newPipeCache(g.r.Device, render.PipelineDesc{
			Vert: shaders.Particle3DVert, Frag: shaders.Particle3DFrag,
			ColorFormat: hdrFormat,
			Bindings:    bindings, Attributes: attrs,
			Blend: true, Factors: b.factors(),
			PushConstantSize: particle3DPushSize,
			SetLayouts:       []vk.VkDescriptorSetLayout{g.post.singles.Layout, g.descriptors.Layout},
		})
		if err != nil {
			return nil, err
		}
		g.particles.scene[b] = c
	}
	return g.particles.scene[b].at(out)
}

func (p *particlePass) destroy() {
	for _, set := range [][blendCount]*pipeCache{p.flat, p.scene} {
		for _, c := range set {
			c.destroy()
		}
	}
	p.flat, p.scene = [blendCount]*pipeCache{}, [blendCount]*pipeCache{}
}

// DrawParticles queues a batch of 2D particles as one instanced draw,
// for the very large counts a sprite-by-sprite path cannot afford. A nil
// texture draws plain quads. The batch takes the queue's current layer
// and blend mode, and draws in the same place a sprite drawn at that
// point would: by layer first, then by the order the calls were made.
// A sort key set with SetSortKey orders sprites within a layer but not
// particle batches, which keep their call order.
//
// The slice is copied into this frame's instance buffer, so it may be
// reused as soon as the call returns. Particles are drawn in the order
// given, without depth sorting, which is what additive effects want; for
// alpha-blended particles that overlap, order the slice yourself.
func (g *Graphics) DrawParticles(tex *Texture, quads []ParticleQuad) {
	g.drawParticlesBlend(tex, quads, g.cur.blend)
}

// drawParticlesBlend is DrawParticles with an explicit blend mode.
func (g *Graphics) drawParticlesBlend(tex *Texture, quads []ParticleQuad, blend Blend) {
	if len(quads) == 0 {
		return
	}
	if tex == nil {
		tex = g.white
	}
	q := g.cur
	first, count := q.parts.add(quads)
	q.parts.flat = append(q.parts.flat, particleBatch{
		set: tex.setFor(FilterDefault), blend: blend, layer: q.layer, seq: int32(len(q.stream.verts)),
		proj: q.spriteProj, first: first, count: count,
	})
	// The sprite run that follows must not merge into the one before, or
	// this batch would have nowhere to go between them.
	q.stream.breakRun = true
}

// DrawParticles3D queues a batch of camera-facing particles in the 3D
// scene as one instanced draw: smoke, embers, snow, magic. A nil texture
// draws plain quads. Positions and sizes are in world units.
//
// The particles are drawn over the finished scene, after decals, and are
// hidden by geometry in front of them; opts.Soft fades them out as they
// approach it. They are neither depth sorted against each other nor lit,
// so additive and unlit effects suit them best. The slice is copied, so
// it may be reused as soon as the call returns.
func (g *Graphics) DrawParticles3D(tex *Texture, quads []ParticleQuad, opts Particles3D) {
	if len(quads) == 0 {
		return
	}
	if tex == nil {
		tex = g.white
	}
	q := g.cur
	first, count := q.parts.add(quads)
	q.parts.scene = append(q.parts.scene, particleBatch{
		set: tex.setFor(FilterDefault), blend: opts.Blend, soft: max(opts.Soft, 0), first: first, count: count,
	})
}

// prepareParticles uploads the queue's instances and puts the 2D batches
// in layer order. It runs before the 2D stream is recorded, because both
// passes draw out of the one buffer.
func (g *Graphics) prepareParticles(q *drawQueue, slot int) error {
	if q.parts.prepared || len(q.parts.quads) == 0 {
		return nil
	}
	q.parts.prepared = true
	if err := q.parts.upload(g, slot); err != nil {
		return err
	}
	slices.SortStableFunc(q.parts.flat, func(a, b particleBatch) int {
		if c := cmp.Compare(a.layer, b.layer); c != 0 {
			return c
		}
		return cmp.Compare(a.seq, b.seq)
	})
	return nil
}

// drawFlatParticles records the 2D batches from at that belong under
// the draw d, and returns where it stopped; all records the rest.
// flush2D calls it between draws so particles interleave with sprites
// by layer and, within a layer, by the order they were submitted in. A
// batch carries no clip rectangle, so the first one restores the full
// viewport scissor.
func (g *Graphics) drawFlatParticles(cb vk.VkCommandBuffer, q *drawQueue, at int, d draw2D, all bool, vp vk.VkRect2D) (int, error) {
	batches := q.parts.flat
	pp := &g.particles
	for first := true; at < len(batches); first = false {
		b := batches[at]
		if !all && (b.layer > d.layer || b.layer == d.layer && b.seq > d.seq) {
			break
		}
		if first {
			render.SetScissorRect(cb, vp)
		}
		pipe, err := g.flatPipeline(b.blend, q.out)
		if err != nil {
			return at, err
		}
		pp.off = 0
		pp.push.proj = b.proj
		vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
		vk.CmdBindVertexBuffers(cb, 0, 1, &q.parts.buffers[q.parts.slot].Handle, &pp.off)
		vk.CmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT,
			0, push2DSize, unsafe.Pointer(&pp.push))
		pp.set = b.set
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &pp.set, 0, nil)
		vk.CmdDraw(cb, 6, b.count, 0, b.first)
		g.stats.Draws2D++
		g.stats.Particles += int(b.count)
		at++
	}
	return at, nil
}

// drawSceneParticles records the 3D batches over the finished scene, in
// a pass with no depth attachment so the fragment program can read the
// depth image and fade against it.
func (g *Graphics) drawSceneParticles(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets, aspect float32) error {
	if len(q.parts.scene) == 0 {
		return nil
	}
	cam := q.camera
	up, _, near, far := cam.defaults()
	forward := cam.Target.Sub(cam.Position).Norm()
	right := forward.Cross(up).Norm()
	camUp := right.Cross(forward).Norm()
	pp := &g.particles
	g.timestamps.Begin(cb, "particles")
	defer g.timestamps.End(cb)
	pass := render.PassDesc{Target: t.hdr, LoadColor: true, NoDepth: true}
	render.BeginTargetPass(cb, pass)
	render.SetViewport(cb, t.extent)
	var ortho float32
	if cam.Ortho > 0 {
		ortho = 1
	}
	pp.push3 = particle3DPush{
		viewProj: cam.ViewProj(aspect),
		right:    lin.V4(right.X, right.Y, right.Z, cam.Position.X),
		up:       lin.V4(camUp.X, camUp.Y, camUp.Z, cam.Position.Y),
		mode:     lin.V4(ortho, 0, 0, 0),
	}
	for _, b := range q.parts.scene {
		pipe, err := g.scenePipeline(b.blend, g.sceneOut)
		if err != nil {
			return err
		}
		pp.off = 0
		pp.push3.params = lin.V4(near, far, b.soft, cam.Position.Z)
		vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &t.depthSet, 0, nil)
		pp.set = b.set
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 1, 1, &pp.set, 0, nil)
		vk.CmdBindVertexBuffers(cb, 0, 1, &q.parts.buffers[q.parts.slot].Handle, &pp.off)
		vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, particle3DPushSize, unsafe.Pointer(&pp.push3))
		vk.CmdDraw(cb, 6, b.count, 0, b.first)
		g.stats.Draws3D++
		g.stats.Particles += int(b.count)
	}
	render.EndTargetPassDesc(cb, pass)
	return nil
}
