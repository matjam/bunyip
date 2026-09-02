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

// NewRenderTexture creates an offscreen surface in pixels. It has the
// full 3D pipeline (shadows, bloom, post) but no anti-aliasing pass.
func (g *Graphics) NewRenderTexture(width, height int) (*RenderTexture, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("gfx: render texture needs a positive size")
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
	set, err := g.textureSet(rt.target.Color.View, g.linear)
	if err != nil {
		rt.Destroy()
		return nil, err
	}
	rt.tex = &Texture{Width: width, Height: height, img: rt.target.Color, set: set, g: g, external: true}
	if err := g.r.Device.OneShot(func(cb vk.VkCommandBuffer) { render.ClearColorForSampling(cb, rt.target.Color) }); err != nil {
		rt.Destroy()
		return nil, err
	}
	return rt, nil
}

// Texture returns the surface for drawing as a sprite or material.
func (rt *RenderTexture) Texture() *Texture { return rt.tex }

// Read copies the last rendered image back from the GPU, after waiting
// for it to finish: thumbnails, saved portraits, tests.
func (rt *RenderTexture) Read() (*image.RGBA, error) { return rt.tex.Read() }

// SetView sets the render texture's 2D coordinate space; default is pixels.
func (rt *RenderTexture) SetView(width, height float32) { rt.queue.setView(width, height) }

// Destroy frees the surface. It must not be in use by a frame in flight.
func (rt *RenderTexture) Destroy() {
	g := rt.g
	_ = g.r.Device.WaitIdle()
	if rt.tex != nil {
		g.forgetTexture(rt.tex)
		g.descriptors.Free(rt.tex.set)
		rt.tex = nil
	}
	if rt.queue != nil {
		rt.queue.destroy()
	}
	if rt.scene != nil {
		rt.scene.destroy(g)
	}
	if rt.target != nil {
		rt.target.Destroy()
	}
}

// DrawTo runs draw with the render texture as the output; every Draw*,
// SetCamera and SetLight call inside it lands on the texture. The texture
// is rendered before the main frame, so it can be drawn in the same frame.
func (g *Graphics) DrawTo(rt *RenderTexture, clear Color, draw func()) {
	if g.frame == nil {
		return
	}
	prev := g.cur
	g.cur = rt.queue
	rt.queue.reset()
	rt.queue.clear = clear
	draw()
	g.cur = prev
	g.subFrames = append(g.subFrames, subFrame{rt: rt, queue: rt.queue})
}
