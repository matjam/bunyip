package gfx

import (
	"fmt"
	"image"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// RenderTexture is an offscreen surface that draws like the screen and is
// then used like a texture: minimaps, portraits, mirrors, picture-in-picture.
type RenderTexture struct {
	Width, Height int
	tex           *Texture
	target        *render.Target
	scene         *sceneTargets
	queue         *drawQueue
	g             *Graphics
}

// RenderTextureOptions says how a render texture samples when it is
// drawn: Nearest keeps a low-resolution scene's pixels sharp when it is
// scaled up (a pixel-art game rendering at 320 by 180), Repeat tiles it.
type RenderTextureOptions struct {
	Nearest bool
	Repeat  bool
}

// NewRenderTexture creates an offscreen surface in pixels. It has the
// full 3D pipeline (shadows, bloom, post) but no anti-aliasing pass, and
// samples with linear filtering; NewRenderTextureOptions chooses.
func (g *Graphics) NewRenderTexture(width, height int) (*RenderTexture, error) {
	return g.NewRenderTextureOptions(width, height, RenderTextureOptions{})
}

// NewRenderTextureOptions is NewRenderTexture with sampling options.
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
	rt := &RenderTexture{Width: width, Height: height, g: g}
	var err error
	if rt.target, err = g.r.Device.NewTargetReadable(extent, g.r.Swapchain.Format, g.r.DepthFormat); err != nil {
		return nil, err
	}
	if rt.scene, err = g.newSceneTargets(extent); err != nil {
		rt.Destroy()
		return nil, err
	}
	if rt.queue, err = g.newQueue(float32(width), float32(height)); err != nil {
		rt.Destroy()
		return nil, err
	}
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
// for it to finish: thumbnails, saved portraits, tests.
func (rt *RenderTexture) Read() (*image.RGBA, error) { return rt.tex.Read() }

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
