package gfx

import (
	"fmt"
	"image"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// RenderTexture is an offscreen surface that draws like the screen and is
// then used like a texture: minimaps, portraits, mirrors, picture-in-picture.
type RenderTexture struct {
	Width, Height int
	tex           *Texture
	target        *render.Target
	scene         *sceneTargets
	queue         *drawQueue
	out           outKey      // the surface's colour format, depth and samples
	format        ColorFormat // what Read has to decode
	g             *Graphics
}

// ColorFormat is the pixel format of a render texture's colour image.
type ColorFormat int

const (
	// ColorScreen is the window's own format, eight bits a channel with
	// sRGB encoding. It is the default and what a texture drawn back onto
	// the screen wants.
	ColorScreen ColorFormat = iota
	// ColorHDR is sixteen-bit floating point RGBA: values above 1 survive,
	// so a render texture can hold light rather than a tone-mapped
	// picture. Feed one to a material or grade it later.
	ColorHDR
	// ColorMask is one eight-bit channel, for a mask, a height field or a
	// coverage buffer. Only the red channel is stored; sampling it gives
	// that value in red and one in alpha.
	ColorMask
)

// vkFormat is the Vulkan format for a colour format, given the window's.
func (f ColorFormat) vkFormat(screen vk.VkFormat) vk.VkFormat {
	switch f {
	case ColorHDR:
		return hdrFormat
	case ColorMask:
		return vk.VK_FORMAT_R8_UNORM
	}
	return screen
}

// RenderTextureOptions says how a render texture is made and how it
// samples when it is drawn.
type RenderTextureOptions struct {
	// Nearest keeps a low-resolution scene's pixels sharp when it is
	// scaled up (a pixel-art game rendering at 320 by 180).
	Nearest bool
	// Repeat tiles the texture instead of clamping at its edges.
	Repeat bool
	// Format is the colour format; the default matches the window.
	Format ColorFormat
	// NoDepth leaves out the depth buffer of the surface's own pass, which
	// nothing tests against: the 3D scene has its own depth buffer and
	// composites through it, and 2D drawing never uses one. Set it to save
	// the memory on a target that is only ever drawn to.
	NoDepth bool
	// Samples multisamples the surface itself: 1 (the default), 2, 4 or 8,
	// clamped to what the GPU supports and reported by Graphics.MaxSamples.
	// Every edge drawn into it, including 2D paths and triangles, is
	// resolved from that many coverage samples. It is separate from
	// PostSettings.Samples, which multisamples the 3D scene behind the
	// composite, here as on screen.
	Samples int
}

// NewRenderTexture creates an offscreen surface in pixels. It has the
// full 3D pipeline (shadows, bloom, post) but no anti-aliasing pass, and
// samples with linear filtering; NewRenderTextureOptions chooses.
func (g *Graphics) NewRenderTexture(width, height int) (*RenderTexture, error) {
	return g.NewRenderTextureOptions(width, height, RenderTextureOptions{})
}

// NewRenderTextureOptions is NewRenderTexture with a choice of sampling,
// colour format, depth and multisampling.
func (g *Graphics) NewRenderTextureOptions(width, height int, opts RenderTextureOptions) (*RenderTexture, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("gfx: render texture needs a positive size")
	}
	sampler := g.linear
	switch {
	case opts.Nearest && opts.Repeat:
		sampler = g.nearestRep
	case opts.Nearest:
		sampler = g.nearest
	case opts.Repeat:
		sampler = g.linearRep
	}
	extent := vk.VkExtent2D{Width: uint32(width), Height: uint32(height)}
	format := opts.Format.vkFormat(g.r.Swapchain.Format)
	depthFormat := g.r.DepthFormat
	if opts.NoDepth {
		depthFormat = vk.VK_FORMAT_UNDEFINED
	}
	samples := g.r.Device.SampleCount(opts.Samples)
	rt := &RenderTexture{Width: width, Height: height, format: opts.Format, g: g}
	// The window's own format and a single sample are the zero key, so a
	// plain render texture draws with the screen's pipelines.
	rt.out = outKey{noDepth: opts.NoDepth, samples: sampleKey(samples)}
	if format != g.r.Swapchain.Format {
		rt.out.color = format
	}
	var err error
	if rt.target, err = g.r.Device.NewTargetDesc(render.TargetDesc{
		Extent: extent, ColorFormat: format, DepthFormat: depthFormat, Samples: samples,
		ColorUsage: vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
	}); err != nil {
		return nil, err
	}
	if rt.scene, err = g.newSceneTargets(extent, g.sceneSamples()); err != nil {
		rt.Destroy()
		return nil, err
	}
	if rt.queue, err = g.newQueue(float32(width), float32(height)); err != nil {
		rt.Destroy()
		return nil, err
	}
	rt.queue.out = rt.out
	set, err := g.textureSet(rt.target.Color.View, sampler)
	if err != nil {
		rt.Destroy()
		return nil, err
	}
	rt.tex = &Texture{Width: width, Height: height, img: rt.target.Color, set: set, g: g, external: true}
	if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, rt.target.Color) }); err != nil {
		rt.Destroy()
		return nil, err
	}
	// A render texture holds its colour image, a depth buffer and the
	// scene targets behind it, which is about four times a plain colour
	// image at the same size.
	g.track(rt, Resource{Kind: ResourceRenderTexture, Width: width, Height: height, Bytes: width * height * 16})
	return rt, nil
}

// Texture returns the surface for drawing as a sprite or material.
func (rt *RenderTexture) Texture() *Texture { return rt.tex }

// Read copies the last rendered image back from the GPU, after waiting
// for it to finish: thumbnails, saved portraits, tests. Whatever the
// surface's colour format, the result is an ordinary image: a ColorHDR
// surface is encoded the way the screen is, so values above 1 clip, and a
// ColorMask surface reads as grey, its one channel copied into red, green
// and blue alike.
func (rt *RenderTexture) Read() (*image.RGBA, error) {
	switch rt.format {
	case ColorHDR:
		return rt.readHDR()
	case ColorMask:
		return rt.readMask()
	}
	return rt.tex.Read()
}

// readHDR converts a half-float surface into an ordinary image, encoded
// like the screen's format.
func (rt *RenderTexture) readHDR() (*image.RGBA, error) {
	pix, err := rt.g.r.Device.ReadImageRaw(rt.target.Color, 8)
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, rt.Width, rt.Height))
	for i := range rt.Width * rt.Height {
		for c := range 3 {
			out.Pix[i*4+c] = linearToSRGB8(getF16(pix[i*8+c*2:]))
		}
		out.Pix[i*4+3] = uint8(lin.Clamp(getF16(pix[i*8+6:])*255+0.5, 0, 255))
	}
	return out, nil
}

// readMask spreads a single-channel surface over the three colour
// channels, so it saves as a readable grey image.
func (rt *RenderTexture) readMask() (*image.RGBA, error) {
	pix, err := rt.g.r.Device.ReadImageRaw(rt.target.Color, 1)
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, rt.Width, rt.Height))
	for i, v := range pix {
		out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = v, v, v, 255
	}
	return out, nil
}

// ReadDepth copies the depth the last 3D scene drawn into this texture
// left behind, one float per pixel, row-major from the top-left corner:
// 0 at the near plane and 1 at the far plane, in the non-linear
// distribution a perspective projection produces. It is the depth the
// engine's own ambient occlusion and decals read, resolved to one sample
// per pixel when the scene is multisampled.
//
// It waits for the GPU and copies the whole image back to the host, so
// it is for tools, tests and one-off queries rather than for every
// frame. A render texture that has had no 3D drawn into it reads back
// all ones.
func (rt *RenderTexture) ReadDepth() ([]float32, error) {
	if rt.scene == nil || rt.scene.hdr == nil || rt.scene.hdr.Depth == nil {
		return nil, fmt.Errorf("gfx: render texture has no depth to read")
	}
	return rt.g.r.Device.ReadDepth(rt.scene.hdr.Depth)
}

// SetView sets the render texture's 2D coordinate space; default is pixels.
func (rt *RenderTexture) SetView(width, height float32) { rt.queue.setView(width, height) }

// Destroy frees the surface. Called inside a frame it costs no wait:
// everything it owns goes on the frame slot's retire list and is freed
// once that frame has finished.
func (rt *RenderTexture) Destroy() {
	g := rt.g
	g.forget(rt)
	if rt.tex != nil {
		// The texture frees both its descriptor sets and marks itself
		// destroyed, so a pointer a game kept from Texture cannot draw
		// with a freed image.
		rt.tex.Destroy()
		rt.tex = nil
	}
	if rt.queue != nil {
		queue := rt.queue
		rt.queue = nil
		g.deferDestroy(queue.destroy)
	}
	if rt.scene != nil {
		scene := rt.scene
		rt.scene = nil
		g.deferDestroy(func() { scene.destroy(g) })
	}
	if rt.target != nil {
		target := rt.target
		rt.target = nil
		g.deferDestroy(target.Destroy)
	}
}

// DrawTo runs draw with the render texture as the output; every Draw*,
// SetCamera and SetLight call inside it lands on the texture. The texture
// is rendered before the main frame, so it can be drawn in the same
// frame. A second DrawTo on the same texture in one frame adds to what
// the first queued, with the first call's clear colour.
func (g *Graphics) DrawTo(rt *RenderTexture, clear Color, draw func()) {
	if g.frame == nil {
		return
	}
	prev := g.cur
	g.cur = rt.queue
	if !g.queuedTo(rt) {
		rt.queue.reset()
		rt.queue.clear = clear
		g.subFrames = append(g.subFrames, subFrame{rt: rt, queue: rt.queue})
	}
	draw()
	g.cur = prev
	// The colour matrix and 2D light blocks live on their shaders, not on
	// the queue, so what the inner draws set is put back for the outer.
	if prev.colorMatrix != nil {
		g.matrixShader.SetUniforms(prev.colorMatrix)
	}
	prev.lightsDirty = true
}

// queuedTo reports whether a render texture already has a pass this frame.
func (g *Graphics) queuedTo(rt *RenderTexture) bool {
	for _, sf := range g.subFrames {
		if sf.rt == rt {
			return true
		}
	}
	return false
}
