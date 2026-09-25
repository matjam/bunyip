package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// The optional post images are made the first time a setting asks for
// them and live as long as the target set, so a game that never turns
// temporal anti-aliasing or depth of field on pays nothing for them. The
// chain works the same way whatever is on: each pass reads the scene
// image cur names and writes a spare one, which becomes cur, so nothing
// is copied between passes. The descriptor sets that name a scene image
// are made the first time a pass reads that image, one per image.

// needVelocity makes the motion vector image and the pass description
// that pairs it with the scene depth.
func (t *sceneTargets) needVelocity(g *Graphics) error {
	if t.vel != nil {
		return nil
	}
	vel, err := g.r.Device.NewTargetSampled(t.extent, velocityFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, vel.Color) }); err != nil {
		vel.Destroy()
		return err
	}
	t.vel = vel
	t.velPass = render.Target{Color: vel.Color, Depth: t.hdr.Depth, Extent: t.extent}
	return nil
}

// needPong makes the second full-size scene image. A probe bake reads
// the finished scene back from whichever image holds it, so it can be a
// transfer source like hdr's colour.
func (t *sceneTargets) needPong(g *Graphics) error {
	if t.pong != nil {
		return nil
	}
	pong, err := g.r.Device.NewTargetCopyable(t.extent, hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	t.pong = pong
	return nil
}

// needHalfDepth makes the half-size depth image renderScene fills for
// the passes that read depth at half size.
func (t *sceneTargets) needHalfDepth(g *Graphics) error {
	if t.half != nil {
		return nil
	}
	half, err := g.r.Device.NewTarget(halfExtent(t.extent), halfDepthFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	set, err := g.post.singles.Allocate(half.Color.View, g.nearest)
	if err != nil {
		half.Destroy()
		return err
	}
	t.half, t.halfSet = half, set
	return nil
}

// halfExtent is an extent halved, never below one texel.
func halfExtent(e vk.VkExtent2D) vk.VkExtent2D {
	return vk.VkExtent2D{Width: max(e.Width/2, 1), Height: max(e.Height/2, 1)}
}

// needTemporal makes the two history images the temporal pass
// alternates between. Each is cleared, because the pass binds the one it
// reads before any frame has written it.
func (t *sceneTargets) needTemporal(g *Graphics) error {
	if t.hist[1] != nil {
		return nil
	}
	if err := t.needVelocity(g); err != nil {
		return err
	}
	if err := t.needPong(g); err != nil {
		return err
	}
	for i := range t.hist {
		if t.hist[i] != nil {
			continue
		}
		hist, err := g.r.Device.NewTargetSampled(t.extent, hdrFormat, vk.VK_FORMAT_UNDEFINED)
		if err != nil {
			return err
		}
		if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, hist.Color) }); err != nil {
			hist.Destroy()
			return err
		}
		t.hist[i] = hist
	}
	return nil
}

// needMotionBlur makes what the motion blur pass needs.
func (t *sceneTargets) needMotionBlur(g *Graphics) error {
	if err := t.needVelocity(g); err != nil {
		return err
	}
	return t.needPong(g)
}

// needDOF makes the half-size image the depth of field gather writes.
func (t *sceneTargets) needDOF(g *Graphics) error {
	if err := t.needPong(g); err != nil {
		return err
	}
	if err := t.needHalfDepth(g); err != nil {
		return err
	}
	if t.dofHalf != nil {
		return nil
	}
	half, err := g.r.Device.NewTarget(halfExtent(t.extent), hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	t.dofHalf = half
	return nil
}

// needRays makes the half-size image the light shafts are drawn into.
func (t *sceneTargets) needRays(g *Graphics) error {
	if t.rays != nil {
		return nil
	}
	rays, err := g.r.Device.NewTargetSampled(halfExtent(t.extent), hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, rays.Color) }); err != nil {
		rays.Destroy()
		return err
	}
	t.rays = rays
	return nil
}

// needLDR2 makes the second swapchain-format image a 2D frame composites
// into before FXAA resolves it.
func (t *sceneTargets) needLDR2(g *Graphics) error {
	if t.ldr2 != nil {
		return nil
	}
	ldr2, err := g.r.Device.NewTarget(t.extent, g.r.Swapchain.Format, g.r.DepthFormat)
	if err != nil {
		return err
	}
	set, err := g.post.singles.Allocate(ldr2.Color.View, g.linear)
	if err != nil {
		ldr2.Destroy()
		return err
	}
	t.ldr2, t.ldr2Set = ldr2, set
	return nil
}

// finalSet returns the composite's descriptor set for the image holding
// the scene and a combination of bloom and light shafts, making it on
// first use. A missing input is bound to the shared black texture, which
// contributes nothing. It makes no image of its own, because the
// composite calls it from inside a render pass, where a clear or a
// barrier would be illegal; the caller asks for the shafts image with
// needRays before it asks for a set that names one.
func (t *sceneTargets) finalSet(g *Graphics, bloom, rays bool) (vk.VkDescriptorSet, error) {
	rays = rays && t.rays != nil
	k := setKey{kind: setFinal, img: t.cur, a: bloom, b: rays}
	if set := t.cachedSet(k); set != 0 {
		return set, nil
	}
	black := g.meshes.black.img.View
	glow, shafts := black, black
	if bloom {
		glow = t.bloomA.Color.View
	}
	if rays {
		shafts = t.rays.Color.View
	}
	return t.makeSet(k, g.post.quads, []render.SamplerBinding{
		{View: t.image(t.cur).View, Sampler: g.linear},
		{View: glow, Sampler: g.linear},
		{View: t.aoB.Color.View, Sampler: g.linear},
		{View: shafts, Sampler: g.linear},
	})
}

// final2DSet is finalSet for a 2D frame, whose scene image is the LDR
// one the 2D stream drew into. Occlusion and light shafts need depth, so
// both are bound to black.
func (t *sceneTargets) final2DSet(g *Graphics, bloom bool) (vk.VkDescriptorSet, error) {
	i := 0
	if bloom {
		i = 1
	}
	if t.finals2D[i] != 0 {
		return t.finals2D[i], nil
	}
	black := g.meshes.black.img.View
	glow := black
	if bloom {
		glow = t.bloomA.Color.View
	}
	set, err := g.post.quads.AllocateMany([]render.SamplerBinding{
		{View: t.ldr.Color.View, Sampler: g.linear},
		{View: glow, Sampler: g.linear},
		{View: g.white.img.View, Sampler: g.linear},
		{View: black, Sampler: g.linear},
	})
	if err != nil {
		return 0, err
	}
	t.finals2D[i] = set
	return set, nil
}

// depthPass runs one fullscreen pass with a depthPush block into target.
func (g *Graphics) depthPass(cb vk.VkCommandBuffer, target *render.Target, pipe *render.Pipeline, set vk.VkDescriptorSet, push depthPush) {
	p := &g.post
	p.depth, p.set = push, set
	render.BeginTargetPass(cb, render.PassDesc{Target: target})
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &p.set, 0, nil)
	vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(p.depth)), unsafe.Pointer(&p.depth))
	vk.CmdDraw(cb, 3, 1, 0, 0)
	render.EndTargetPass(cb, target)
}

// chainPass runs one fullscreen pass from the image holding the scene
// into a spare one, which then holds it.
func (g *Graphics) chainPass(cb vk.VkCommandBuffer, t *sceneTargets, pipe *render.Pipeline, set vk.VkDescriptorSet, push depthPush) {
	dst := t.spare()
	g.depthPass(cb, t.target(dst), pipe, set, push)
	t.cur = dst
}

// renderTemporal blends this frame with the resolved frames before it.
// It reads the history image the last frame wrote and writes the other,
// which then holds the scene and is what the next frame reads.
func (g *Graphics) renderTemporal(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) error {
	s := g.post.settings
	blend := s.TemporalBlend
	if blend <= 0 {
		blend = 0.1
	}
	valid := float32(0)
	if t.histValid && q.hasPrevVP {
		valid = 1
	}
	next := t.histNext
	prev := 1 - next
	k := setKey{kind: setTemporal, img: t.cur, a: prev == 1}
	set := t.cachedSet(k)
	if set == 0 {
		var err error
		if set, err = t.makeSet(k, g.post.quads, []render.SamplerBinding{
			{View: t.image(t.cur).View, Sampler: g.linear},
			{View: t.hist[prev].Color.View, Sampler: g.linear},
			{View: t.vel.Color.View, Sampler: g.linear},
			{View: t.hdr.Depth.View, Sampler: g.nearest},
		}); err != nil {
			return err
		}
	}
	g.depthPass(cb, t.hist[next], g.post.taa, set, depthPush{
		matrix: q.prevViewProj.Mul(q.invViewProjJ),
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), blend, valid},
	})
	t.cur = imgHist0 + sceneImage(next)
	t.histNext = prev
	t.histValid = true
	return nil
}

// renderMotionBlur smears each pixel back along the way it moved.
func (g *Graphics) renderMotionBlur(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) error {
	s := g.post.settings
	taps := s.MotionSamples
	if taps <= 0 {
		taps = 8
	}
	k := setKey{kind: setMotion, img: t.cur}
	set := t.cachedSet(k)
	if set == 0 {
		var err error
		if set, err = t.makeSet(k, g.post.triples, []render.SamplerBinding{
			{View: t.image(t.cur).View, Sampler: g.linear},
			{View: t.vel.Color.View, Sampler: g.linear},
			{View: t.hdr.Depth.View, Sampler: g.nearest},
		}); err != nil {
			return err
		}
	}
	g.chainPass(cb, t, g.post.motionBlur, set, depthPush{
		matrix: q.prevViewProj.Mul(q.invViewProjJ),
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), s.MotionBlur, float32(taps)},
	})
	return nil
}

// renderDOF blurs what is not at the focus distance: a gather at half
// size, then a full-size pass that mixes it with the sharp image.
func (g *Graphics) renderDOF(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) error {
	s := g.post.settings
	rng := s.FocusRange
	if rng <= 0 {
		rng = s.FocusDistance / 4
	}
	radius := s.BokehRadius
	if radius <= 0 {
		radius = 12
	}
	// The radius is quoted for a 1080-high frame, so a smaller output
	// blurs by the same fraction of the image rather than the same pixels.
	radius *= float32(t.extent.Height) / 1080
	taps := s.BokehSamples
	if taps <= 0 {
		taps = 16
	}
	gk := setKey{kind: setDOF, img: t.cur}
	gather := t.cachedSet(gk)
	var err error
	if gather == 0 {
		if gather, err = t.makeSet(gk, g.post.pairs, []render.SamplerBinding{
			{View: t.image(t.cur).View, Sampler: g.linear},
			{View: t.half.Color.View, Sampler: g.nearest},
		}); err != nil {
			return err
		}
	}
	ck := setKey{kind: setDOFCombine, img: t.cur}
	combine := t.cachedSet(ck)
	if combine == 0 {
		if combine, err = t.makeSet(ck, g.post.quads, []render.SamplerBinding{
			{View: t.image(t.cur).View, Sampler: g.linear},
			{View: t.dofHalf.Color.View, Sampler: g.linear},
			{View: t.hdr.Depth.View, Sampler: g.nearest},
			{View: t.half.Color.View, Sampler: g.nearest},
		}); err != nil {
			return err
		}
	}
	invProj := q.projJ.Inverse()
	half := t.dofHalf.Extent
	g.depthPass(cb, t.dofHalf, g.post.dof, gather, depthPush{
		matrix: invProj,
		a:      [4]float32{1 / float32(half.Width), 1 / float32(half.Height), s.FocusDistance, rng},
		b:      [4]float32{radius * float32(half.Height) / float32(t.extent.Height), float32(taps)},
	})
	g.chainPass(cb, t, g.post.dofCombine, combine, depthPush{
		matrix: invProj,
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), s.FocusDistance, rng},
	})
	return nil
}

// sunScreen is where the directional light's source lies in texture
// coordinates, and whether it is in front of the camera at all.
func sunScreen(q *drawQueue) (lin.Vec2, bool) {
	dir := q.light.Direction.Norm()
	if dir == (lin.Vec3{}) {
		return lin.Vec2{}, false
	}
	// The light travels along Direction, so the sun lies the other way, at
	// infinity: a direction is a point with w of zero. A w of zero or less
	// coming out means the sun is beside or behind the camera, or that the
	// camera is orthographic, and there are no shafts either way.
	clip := q.viewProjJ.MulVec4(dir.Mul(-1).Vec4(0))
	if clip.W <= 1e-6 {
		return lin.Vec2{}, false
	}
	uv := lin.V2(clip.X/clip.W*0.5+0.5, clip.Y/clip.W*0.5+0.5)
	// Shafts from a sun far outside the frame reach nothing on screen.
	if uv.X < -1 || uv.X > 2 || uv.Y < -1 || uv.Y > 2 {
		return uv, false
	}
	return uv, true
}

// renderRays draws the light shafts into the half-size rays image, which
// the composite adds to the scene.
func (g *Graphics) renderRays(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets, sun lin.Vec2) {
	p := &g.post
	s := p.settings
	decay := s.GodRayDecay
	if decay <= 0 {
		decay = 0.96
	}
	density := s.GodRayDensity
	if density <= 0 {
		density = 0.6
	}
	taps := s.GodRaySamples
	if taps <= 0 {
		taps = 32
	}
	// The shafts take the light's hue but not its intensity: a sun of 2.4
	// would otherwise make GodRays of 1 blow the sky out, and the setting
	// is what a game tunes.
	c := q.light.Color
	if m := max(c.R, max(c.G, c.B)); m > 0 {
		c = Color{c.R / m, c.G / m, c.B / m, 1}
	} else {
		c = White
	}
	render.BeginTargetPass(cb, render.PassDesc{Target: t.rays})
	p.fullscreen(cb, p.godRays, t.depthSet, postPush{
		a: [4]float32{sun.X, sun.Y, s.GodRays, decay},
		b: [4]float32{float32(taps), density, 1 / float32(taps)},
		c: [4]float32{c.R, c.G, c.B, 0},
	})
	render.EndTargetPass(cb, t.rays)
}

// wantsHalfDepth reports whether a presented frame's post chain reads
// the half-size depth, which only depth of field does. The reflection
// trace asks for it on its own. Ambient occlusion and the light shafts
// read the full depth: at half size the saving was no more than the cost
// of building the image, and their output changed.
func (g *Graphics) wantsHalfDepth() bool {
	return g.post.settings.FocusDistance > 0
}

// buildHalfDepth fills the half-size depth from the scene depth: each
// texel keeps the nearest of the four under it.
func (g *Graphics) buildHalfDepth(cb vk.VkCommandBuffer, t *sceneTargets) {
	g.timestamps.Begin(cb, "half depth")
	render.BeginTargetPass(cb, render.PassDesc{Target: t.half})
	g.post.fullscreen(cb, g.post.depthHalf, t.depthSet, postPush{})
	render.EndTargetPass(cb, t.half)
	g.timestamps.End(cb)
}

// postChain runs the effects that read the scene and its depth, in the
// order a camera works in: resolve first, then the shutter, then the
// lens. Each leaves its result in the image cur names.
func (g *Graphics) postChain(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) error {
	s := g.post.settings
	temporal := s.TemporalAA
	motion := s.MotionBlur > 0
	if temporal {
		if err := t.needTemporal(g); err != nil {
			return err
		}
		g.timestamps.Begin(cb, "temporal")
		err := g.renderTemporal(cb, q, t)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	if motion {
		if err := t.needMotionBlur(g); err != nil {
			return err
		}
		g.timestamps.Begin(cb, "motionblur")
		err := g.renderMotionBlur(cb, q, t)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	if !temporal {
		// Turning it off and on again must not reproject a frame from
		// whenever it was last on against this frame's matrices.
		t.histValid = false
	}
	if s.FocusDistance > 0 {
		if err := t.needDOF(g); err != nil {
			return err
		}
		g.timestamps.Begin(cb, "depthoffield")
		err := g.renderDOF(cb, q, t)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	return nil
}
