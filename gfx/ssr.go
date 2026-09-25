package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Screen-space reflections give a smooth surface the part of its
// reflection the screen already holds: the bright box standing on a
// polished floor, a sign over wet asphalt. The mesh shader writes how
// much reflection each opaque pixel wants into the frame's alpha channel,
// which nothing else reads. After the opaque draws, a half-size pass
// marches a ray through the half-size depth for those pixels and records
// what it finds; a full-size pass then puts that over the scene. Where a
// ray leaves the screen or hits nothing the pixel keeps the environment
// or reflection probe the mesh shader already gave it, so the two fit
// together rather than adding up.
//
// The full-size half runs one of two ways. When nothing translucent
// follows the opaque draws, the scene pass is not interrupted: the trace
// runs after it, and the pass after the scene's reads the scene and
// writes a new image with the reflections applied, using each pixel's own
// weight. Otherwise the scene pass stops after the opaque draws for the
// trace, and the first draw of the pass that resumes it blends the trace
// over its colour attachment, the weights already folded in at half size.

// initReflections builds the screen-space reflection pipelines. It runs
// after the post pass, whose layouts it binds, and after the mesh pass,
// whose frame block the trace reads.
func (g *Graphics) initReflections() error {
	p := &g.post
	dev := g.r.Device
	push := uint32(unsafe.Sizeof(postPush{}))
	var err error
	if p.reflect, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SSRFrag, ColorFormat: hdrFormat,
		PushConstantSize: push,
		SetLayouts:       []vk.VkDescriptorSetLayout{p.triples.Layout, g.meshes.uniformLayout.Layout},
	}); err != nil {
		return err
	}
	if p.reflectApply, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SSRApplyFrag, ColorFormat: hdrFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.quads.Layout},
	}); err != nil {
		return err
	}
	// The blend draws into the scene pass's own attachments, so it is
	// built once per sample count the post settings ask for. It neither
	// tests nor writes depth.
	p.reflectBlend, err = newPipeCache(dev, render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SSRBlendFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Blend:            true, // premultiplied: the ray's colour replaces its share of the surface
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.singles.Layout},
	})
	return err
}

// reflectParams packs the reflection settings for the frame block, with
// the defaults filled in. A zero strength turns the pass and the weight
// the mesh shaders write off together.
func (g *Graphics) reflectParams() lin.Vec4 {
	s := g.post.settings
	if s.Reflections <= 0 {
		return lin.Vec4{}
	}
	roughness := s.ReflectionRoughness
	if roughness <= 0 {
		roughness = 0.35
	}
	distance := s.ReflectionDistance
	if distance <= 0 {
		distance = 30
	}
	steps := s.ReflectionSteps
	if steps <= 0 {
		steps = 32
	}
	return lin.V4(min(s.Reflections, 1), roughness, distance, float32(steps))
}

// reflections reports whether the queue's opaque draws want the pass.
func (g *Graphics) reflections(seen drawList) bool {
	return g.post.settings.Reflections > 0 && g.post.reflect != nil && seen.len() > 0
}

// needReflections makes the half-size trace image, the half-size depth
// it marches through, and the sets the three reflection passes bind.
func (t *sceneTargets) needReflections(g *Graphics) error {
	if err := t.needHalfDepth(g); err != nil {
		return err
	}
	if err := t.needPong(g); err != nil {
		return err
	}
	if t.refl != nil {
		return nil
	}
	p := &g.post
	refl, err := g.r.Device.NewTarget(halfExtent(t.extent), hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		for _, s := range []struct {
			set  vk.VkDescriptorSet
			pool *render.DescriptorSets
		}{{t.reflSet, p.singles}, {t.traceSet, p.triples}, {t.applySet, p.quads}} {
			if s.set != 0 {
				s.pool.Free(s.set)
			}
		}
		t.reflSet, t.traceSet, t.applySet = 0, 0, 0
		refl.Destroy()
		return err
	}
	if t.reflSet, err = p.singles.Allocate(refl.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.traceSet, err = p.triples.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear}, {View: t.half.Color.View, Sampler: g.nearest},
		{View: t.hdr.Depth.View, Sampler: g.nearest},
	}); err != nil {
		return fail(err)
	}
	if t.applySet, err = p.quads.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.nearest}, {View: refl.Color.View, Sampler: g.nearest},
		{View: t.hdr.Depth.View, Sampler: g.nearest}, {View: t.half.Color.View, Sampler: g.nearest},
	}); err != nil {
		return fail(err)
	}
	t.refl = refl
	return nil
}

// depthRow is the projection's z row as the reflection shaders take it,
// which turns a stored depth back into view-space z for a perspective or
// an orthographic camera alike.
func depthRow(proj lin.Mat4) [4]float32 {
	return [4]float32{proj[10], proj[14], proj[11], proj[15]}
}

// traceReflections marches the half-size reflection rays over the
// opaque scene the scene pass resolved into hdr's colour. weighted folds
// each surface's weight into the result, for blendReflections.
func (g *Graphics) traceReflections(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, t *sceneTargets, weighted bool) {
	p := &g.post
	render.BeginTargetPass(cb, render.PassDesc{Target: t.refl})
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.reflect.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
	p.fullscreen(cb, p.reflect, t.traceSet, postPush{
		a: [4]float32{boolFloat(weighted)},
		b: depthRow(q.projJ),
	})
	render.EndTargetPass(cb, t.refl)
}

// applyReflections records, into an open single-sample pass over a spare
// scene image, the scene with the trace applied by each pixel's own
// weight. It writes every pixel, so the pass need not load its target.
func (g *Graphics) applyReflections(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) {
	p := &g.post
	p.fullscreen(cb, p.reflectApply, t.applySet, postPush{b: depthRow(q.projJ)})
}

// blendReflections records, as the first draw of the pass that resumes
// the scene pass, the weighted trace blended over the colour attachment.
func (g *Graphics) blendReflections(cb vk.VkCommandBuffer, t *sceneTargets) error {
	pipe, err := g.post.reflectBlend.at(g.sceneOut)
	if err != nil {
		return err
	}
	g.post.fullscreen(cb, pipe, t.reflSet, postPush{})
	return nil
}
