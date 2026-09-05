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
// which nothing else reads, and this pass marches a ray through the depth
// buffer for those pixels and blends what it finds over them. Where a ray
// leaves the screen or hits nothing the pixel keeps the environment or
// reflection probe the mesh shader already gave it, so the two fit
// together rather than adding up.

// initReflections builds the screen-space reflection pipeline. It runs
// after the post pass, whose three-sampler layout it binds, and after the
// mesh pass, whose frame block it reads.
func (g *Graphics) initReflections() error {
	p := &g.post
	pipe, err := g.r.Device.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SSRFrag, ColorFormat: hdrFormat,
		Blend:            true, // premultiplied: the ray's colour replaces its share of the surface
		PushConstantSize: uint32(unsafe.Sizeof(postPush{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{p.triples.Layout, g.meshes.uniformLayout.Layout},
	})
	if err != nil {
		return err
	}
	p.reflect = pipe
	return nil
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

// drawReflections traces the reflections over the finished opaque scene,
// reading the scene copy and the depth image and blending into the HDR
// colour. It runs in its own pass without the depth attachment, so the
// shader can sample the depth image the opaque pass wrote.
func (g *Graphics) drawReflections(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, t *sceneTargets) {
	p := &g.post
	pass := render.PassDesc{Target: t.hdr, LoadColor: true, NoDepth: true}
	render.BeginTargetPass(cb, pass)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.reflect.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
	p.fullscreen(cb, p.reflect, t.reflectSet, postPush{
		a: [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), 0, 0},
	})
	render.EndTargetPassDesc(cb, pass)
}
