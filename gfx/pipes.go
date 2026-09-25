package gfx

import (
	"crypto/sha256"
	"sync"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// outKey is the attachment set a pipeline renders into: the colour
// format it writes, whether the pass carries a depth attachment, and how
// many samples the attachments have. The zero value keeps the pipeline
// description's own formats at one sample, which is the screen and every
// pass that predates multisampling.
//
// A pipeline is only valid in a pass whose attachments match it, so
// every pipeline that can render into more than one kind of output is
// built once per outKey and cached under it.
type outKey struct {
	color   vk.VkFormat              // zero keeps the description's colour format
	noDepth bool                     // the pass has no depth attachment
	samples vk.VkSampleCountFlagBits // zero means one
}

// sampleKey normalises a sample count for an outKey: one sample and no
// multisampling at all are the same pipeline, so both key as zero and a
// single-sample render texture shares the screen's pipelines.
func sampleKey(n vk.VkSampleCountFlagBits) vk.VkSampleCountFlagBits {
	if n <= vk.VK_SAMPLE_COUNT_1_BIT {
		return 0
	}
	return n
}

// apply returns the description with the output's attachments in it.
func (k outKey) apply(d render.PipelineDesc) render.PipelineDesc {
	if k.color != 0 {
		d.ColorFormat = k.color
	}
	if k.noDepth {
		d.DepthFormat = vk.VK_FORMAT_UNDEFINED
	}
	d.Samples = k.samples
	return d
}

// pipeCache builds one pipeline description at each output it is asked
// for and keeps them for the life of the context. The engine's fixed
// pipelines (the sky, outlines, decals, debug lines, the composite) go
// through it, because a scene renders at whatever sample count the post
// settings ask for and a render texture may have a colour format of its
// own.
//
// A variant may still be building on a worker goroutine, either because
// newGraphics collected it into its batch or because start began it
// ahead of use; at waits for it.
type pipeCache struct {
	dev   *render.Device
	desc  render.PipelineDesc
	pipes map[outKey]*render.Pipeline
}

// newPipeCache makes the cache and builds the description's own variant,
// so a broken shader module fails early rather than mid-frame. Inside the
// device's pipeline batch the variant builds on a worker and a broken
// module fails when the batch is waited for.
func newPipeCache(dev *render.Device, desc render.PipelineDesc) (*pipeCache, error) {
	c := &pipeCache{dev: dev, desc: desc, pipes: map[outKey]*render.Pipeline{}}
	p, err := dev.NewPipeline(desc)
	if err != nil {
		return nil, err
	}
	c.pipes[outKey{}] = p
	return c, nil
}

// at returns the pipeline for one output, building it on first use and
// waiting for it when it is still building.
func (c *pipeCache) at(k outKey) (*render.Pipeline, error) {
	p, ok := c.pipes[k]
	if !ok {
		var err error
		if p, err = c.dev.NewPipeline(k.apply(c.desc)); err != nil {
			return nil, err
		}
		c.pipes[k] = p
	}
	if err := p.Wait(); err != nil {
		delete(c.pipes, k)
		p.Destroy()
		return nil, err
	}
	return p, nil
}

// start begins building the variant for one output on a worker goroutine
// when the cache has none, so a later at finds it built or nearly so. A
// failure to start is left for at to report.
func (c *pipeCache) start(k outKey) {
	if c == nil {
		return
	}
	if _, ok := c.pipes[k]; ok {
		return
	}
	if p, err := c.dev.StartPipeline(k.apply(c.desc)); err == nil {
		c.pipes[k] = p
	}
}

// destroy frees every variant. It must not be in use by a frame in flight.
func (c *pipeCache) destroy() {
	if c == nil {
		return
	}
	for _, p := range c.pipes {
		p.Destroy()
	}
	c.pipes = nil
}

// engineShaderKey names the set of engine programs the pipeline cache
// file is built from, so an engine with changed shaders starts a new
// file rather than growing the old one with programs nothing uses.
var engineShaderKey = sync.OnceValue(func() []byte {
	h := sha256.New()
	for _, spv := range engineShaders() {
		h.Write(spv)
	}
	return h.Sum(nil)
})

// engineShaders is every SPIR-V program package shaders embeds.
// TestEngineShadersComplete checks that none is missing.
func engineShaders() [][]byte {
	return [][]byte{
		shaders.SpriteVert, shaders.SpriteFrag, shaders.MatrixFrag, shaders.LitFrag, shaders.SDFFrag,
		shaders.TextOutlineFrag, shaders.SkyFrag, shaders.SkyParamFrag, shaders.LineVert, shaders.LineFrag,
		shaders.ParticleVert, shaders.ParticleFrag, shaders.Particle3DVert, shaders.Particle3DFrag,
		shaders.OutlineVert, shaders.SolidFrag, shaders.DecalVert, shaders.DecalFrag, shaders.PBRFrag,
		shaders.PBROITFrag, shaders.TerrainFrag, shaders.PBRVert, shaders.PBRSkinVert, shaders.ShadowVert,
		shaders.ShadowSkinVert, shaders.ShadowFrag, shaders.PostVert, shaders.PostFrag, shaders.BrightFrag,
		shaders.BlurFrag, shaders.FXAAFrag, shaders.SSAOFrag, shaders.SSRFrag, shaders.AOBlurFrag,
		shaders.VelocityVert, shaders.VelocitySkinVert, shaders.VelocityFrag, shaders.TAAFrag,
		shaders.DOFFrag, shaders.MotionBlurFrag, shaders.GodRaysFrag, shaders.OITFrag,
		shaders.SSRApplyFrag, shaders.SSRBlendFrag, shaders.DepthHalfFrag, shaders.DOFCombineFrag,
	}
}

// waitPipelines waits for the pipelines newGraphics started building on
// workers: the 3D scene's and the post chain's. Everything that records
// a command with one of them calls it first; the first call waits and the
// rest return at once.
func (g *Graphics) waitPipelines() error {
	b := g.pending
	if b == nil {
		return nil
	}
	g.pending = nil
	return b.Wait()
}

// startSampleVariants begins building, on workers, the variants of the
// scene's pipelines for a sample count the post settings have just asked
// for, so the frame that first renders at it waits for the slowest of
// them rather than building each in turn as it records.
func (g *Graphics) startSampleVariants(samples vk.VkSampleCountFlagBits) {
	if g.prewarmed == samples {
		return
	}
	g.prewarmed = samples
	out := outKey{samples: sampleKey(samples)}
	mp := &g.meshes
	for _, c := range []*pipeCache{mp.skyPipe, mp.skyParamPipe, mp.outlinePipe, mp.xrayPipe, mp.decalPipe, g.linePipe} {
		c.start(out)
	}
	for _, c := range g.particles.scene {
		c.start(out)
	}
	if s := mp.defaultShader; s != nil {
		s.start(pipeKey{blend: BlendReplace, out: out})
		s.start(pipeKey{blend: BlendAlpha, out: out})
	}
}

// startSceneVariants begins building, on workers, the lit and shadow
// pipelines the queue's materials need and their shaders do not have
// yet. The frame that first draws a new material or shader then waits
// for the slowest of its variants rather than building each in turn as
// its draws are recorded. Once every variant exists it only looks them
// up.
func (g *Graphics) startSceneVariants(q *drawQueue) {
	// The same test prepareDraws makes for a draw's pass.
	oit := g.post.settings.OrderIndependent && g.r.Device.IndependentBlend()
	for i := range q.mats {
		fm := &q.mats[i]
		s := fm.shader
		key := fm.key
		key.out = g.sceneOut
		s.start(key)
		if fm.blended && oit && !fm.transmissive && s.orderIndependent() {
			key.out, key.oit = outKey{}, true
			s.start(key)
		}
		if !fm.blended && (q.light.Shadows || q.shadow.any()) {
			s.start(pipeKey{shadow: true})
		}
	}
}
