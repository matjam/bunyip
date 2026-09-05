package gfx

import (
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
type pipeCache struct {
	dev   *render.Device
	desc  render.PipelineDesc
	pipes map[outKey]*render.Pipeline
}

// newPipeCache makes the cache and builds the description's own variant,
// so a broken shader module fails here rather than mid-frame.
func newPipeCache(dev *render.Device, desc render.PipelineDesc) (*pipeCache, error) {
	c := &pipeCache{dev: dev, desc: desc, pipes: map[outKey]*render.Pipeline{}}
	if _, err := c.at(outKey{}); err != nil {
		return nil, err
	}
	return c, nil
}

// at returns the pipeline for one output, building it on first use.
func (c *pipeCache) at(k outKey) (*render.Pipeline, error) {
	if p, ok := c.pipes[k]; ok {
		return p, nil
	}
	p, err := c.dev.NewPipeline(k.apply(c.desc))
	if err != nil {
		return nil, err
	}
	c.pipes[k] = p
	return p, nil
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
