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
// chain works the same way whatever is on: each pass reads the HDR image
// and writes the scratch one, which is then copied back over the HDR
// image, so every pass has exactly one input set to keep.

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
	set, err := g.post.singles.Allocate(vel.Color.View, g.linear)
	if err != nil {
		vel.Destroy()
		return err
	}
	t.vel, t.velSet = vel, set
	t.velPass = render.Target{Color: vel.Color, Depth: t.hdr.Depth, Extent: t.extent}
	return nil
}

// needPong makes the scratch HDR image every chain pass writes into.
func (t *sceneTargets) needPong(g *Graphics) error {
	if t.pong != nil {
		return nil
	}
	pong, err := g.r.Device.NewTargetCopyable(t.extent, hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	set, err := g.post.singles.Allocate(pong.Color.View, g.linear)
	if err != nil {
		pong.Destroy()
		return err
	}
	t.pong, t.pongSet = pong, set
	return nil
}

// needTemporal makes the history image and the temporal pass's set.
func (t *sceneTargets) needTemporal(g *Graphics) error {
	if t.taaSet != 0 {
		return nil
	}
	if err := t.needVelocity(g); err != nil {
		return err
	}
	if err := t.needPong(g); err != nil {
		return err
	}
	if t.hist == nil {
		hist, err := g.r.Device.NewTargetSampled(t.extent, hdrFormat, vk.VK_FORMAT_UNDEFINED)
		if err != nil {
			return err
		}
		if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, hist.Color) }); err != nil {
			hist.Destroy()
			return err
		}
		t.hist = hist
	}
	set, err := g.post.quads.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear},
		{View: t.hist.Color.View, Sampler: g.linear},
		{View: t.vel.Color.View, Sampler: g.linear},
		{View: t.hdr.Depth.View, Sampler: g.nearest},
	})
	if err != nil {
		return err
	}
	t.taaSet = set
	return nil
}

// needMotionBlur makes the motion blur pass's set.
func (t *sceneTargets) needMotionBlur(g *Graphics) error {
	if t.mbSet != 0 {
		return nil
	}
	if err := t.needVelocity(g); err != nil {
		return err
	}
	if err := t.needPong(g); err != nil {
		return err
	}
	set, err := g.post.triples.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear},
		{View: t.vel.Color.View, Sampler: g.linear},
		{View: t.hdr.Depth.View, Sampler: g.nearest},
	})
	if err != nil {
		return err
	}
	t.mbSet = set
	return nil
}

// needDOF makes the depth of field pass's set. The third binding of the
// layout is the depth image again; the program does not read it.
func (t *sceneTargets) needDOF(g *Graphics) error {
	if t.dofSet != 0 {
		return nil
	}
	if err := t.needPong(g); err != nil {
		return err
	}
	set, err := g.post.triples.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear},
		{View: t.hdr.Depth.View, Sampler: g.nearest},
		{View: t.hdr.Depth.View, Sampler: g.nearest},
	})
	if err != nil {
		return err
	}
	t.dofSet = set
	return nil
}

// needRays makes the half-size image the light shafts are drawn into.
func (t *sceneTargets) needRays(g *Graphics) error {
	if t.rays != nil {
		return nil
	}
	half := vk.VkExtent2D{Width: max(t.extent.Width/2, 1), Height: max(t.extent.Height/2, 1)}
	rays, err := g.r.Device.NewTargetSampled(half, hdrFormat, vk.VK_FORMAT_UNDEFINED)
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

// finalSet returns the composite's descriptor set for a combination of
// bloom and light shafts, making it on first use. A missing input is
// bound to the shared black texture, which contributes nothing.
func (t *sceneTargets) finalSet(g *Graphics, bloom, rays bool) (vk.VkDescriptorSet, error) {
	i := finalIndex(bloom, rays)
	if t.finals[i] != 0 {
		return t.finals[i], nil
	}
	black := g.meshes.black.img.View
	glow, shafts := black, black
	if bloom {
		glow = t.bloomA.Color.View
	}
	if rays {
		if err := t.needRays(g); err != nil {
			return 0, err
		}
		shafts = t.rays.Color.View
	}
	set, err := g.post.quads.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear},
		{View: glow, Sampler: g.linear},
		{View: t.aoB.Color.View, Sampler: g.linear},
		{View: shafts, Sampler: g.linear},
	})
	if err != nil {
		return 0, err
	}
	t.finals[i] = set
	return set, nil
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

// chainPass runs one fullscreen pass from the HDR image into the scratch
// image and copies the result back, so the next pass and the composite
// find their input where they expect it.
func (g *Graphics) chainPass(cb vk.VkCommandBuffer, t *sceneTargets, pipe *render.Pipeline, set vk.VkDescriptorSet, push depthPush) {
	p := &g.post
	p.depth, p.set = push, set
	render.BeginTargetPass(cb, render.PassDesc{Target: t.pong})
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &p.set, 0, nil)
	vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(p.depth)), unsafe.Pointer(&p.depth))
	vk.CmdDraw(cb, 3, 1, 0, 0)
	render.EndTargetPass(cb, t.pong)
	render.CopyColorForSampling(cb, t.pong.Color, t.hdr.Color)
}

// renderTemporal blends this frame with the resolved frames before it and
// leaves the result in the HDR image and in the history for the next one.
func (g *Graphics) renderTemporal(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) {
	s := g.post.settings
	blend := s.TemporalBlend
	if blend <= 0 {
		blend = 0.1
	}
	valid := float32(0)
	if t.histValid && q.hasPrevVP {
		valid = 1
	}
	g.chainPass(cb, t, g.post.taa, t.taaSet, depthPush{
		matrix: q.prevViewProj.Mul(q.invViewProjJ),
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), blend, valid},
	})
	render.CopyColorForSampling(cb, t.pong.Color, t.hist.Color)
	t.histValid = true
}

// renderMotionBlur smears each pixel back along the way it moved.
func (g *Graphics) renderMotionBlur(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) {
	s := g.post.settings
	taps := s.MotionSamples
	if taps <= 0 {
		taps = 8
	}
	g.chainPass(cb, t, g.post.motionBlur, t.mbSet, depthPush{
		matrix: q.prevViewProj.Mul(q.invViewProjJ),
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), s.MotionBlur, float32(taps)},
	})
}

// renderDOF blurs what is not at the focus distance.
func (g *Graphics) renderDOF(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) {
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
	g.chainPass(cb, t, g.post.dof, t.dofSet, depthPush{
		matrix: q.projJ.Inverse(),
		a:      [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), s.FocusDistance, rng},
		b:      [4]float32{radius, float32(taps)},
	})
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

// postChain runs the effects that read the scene and its depth, in the
// order a camera works in: resolve first, then the shutter, then the
// lens. Each leaves its result in the HDR image.
func (g *Graphics) postChain(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) error {
	s := g.post.settings
	temporal := s.TemporalAA
	motion := s.MotionBlur > 0
	if temporal {
		if err := t.needTemporal(g); err != nil {
			return err
		}
		g.renderTemporal(cb, q, t)
	}
	if motion {
		if err := t.needMotionBlur(g); err != nil {
			return err
		}
		g.renderMotionBlur(cb, q, t)
	}
	if s.FocusDistance > 0 {
		if err := t.needDOF(g); err != nil {
			return err
		}
		g.renderDOF(cb, q, t)
	}
	return nil
}
