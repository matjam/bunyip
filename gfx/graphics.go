package gfx

import (
	"fmt"
	"image"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Graphics is the drawing context for one window. Begin opens a frame,
// the Draw* calls queue work, and End submits it.
type Graphics struct {
	r           *render.Renderer
	descriptors *render.DescriptorSets
	nearest     vk.VkSampler
	linear      vk.VkSampler
	nearestRep  vk.VkSampler
	linearRep   vk.VkSampler
	spritePipe  *render.Pipeline
	sdfPipe     *render.Pipeline
	meshes      meshPass
	post        postPass
	white       *Texture
	frame       *render.Frame
	main        *drawQueue // the screen
	cur         *drawQueue // where Draw* calls land
	subFrames   []subFrame
}

// New builds the drawing context over a renderer.
func New(r *render.Renderer) (*Graphics, error) {
	g := &Graphics{r: r}
	var err error
	if g.descriptors, err = r.Device.NewTextureDescriptors(1024); err != nil {
		return nil, err
	}
	if g.nearest, err = r.Device.NewSampler(false); err != nil {
		return nil, err
	}
	if g.linear, err = r.Device.NewSampler(true); err != nil {
		return nil, err
	}
	if g.nearestRep, err = r.Device.NewSamplerRepeat(false, true); err != nil {
		return nil, err
	}
	if g.linearRep, err = r.Device.NewSamplerRepeat(true, true); err != nil {
		return nil, err
	}
	bindings, attrs := spriteVertexLayout()
	g.spritePipe, err = r.Device.NewPipeline(render.PipelineDesc{
		Vert: shaders.SpriteVert, Frag: shaders.SpriteFrag,
		ColorFormat: r.Swapchain.Format,
		DepthFormat: r.DepthFormat,
		Bindings:    bindings, Attributes: attrs,
		Blend:            true,
		PushConstantSize: uint32(unsafe.Sizeof(lin.Mat4{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout},
	})
	if err != nil {
		return nil, err
	}
	g.sdfPipe, err = r.Device.NewPipeline(render.PipelineDesc{
		Vert: shaders.SpriteVert, Frag: shaders.SDFFrag,
		ColorFormat: r.Swapchain.Format,
		DepthFormat: r.DepthFormat,
		Bindings:    bindings, Attributes: attrs,
		Blend:            true,
		PushConstantSize: uint32(unsafe.Sizeof(lin.Mat4{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout},
	})
	if err != nil {
		return nil, err
	}
	if g.white, err = g.newTexture(1, 1, []byte{255, 255, 255, 255}, TextureOptions{}); err != nil {
		return nil, err
	}
	if err := g.initMeshPass(); err != nil {
		return nil, err
	}
	if err := g.initPost(); err != nil {
		return nil, err
	}
	ext := r.Swapchain.Extent
	if g.main, err = g.newQueue(float32(ext.Width), float32(ext.Height)); err != nil {
		return nil, err
	}
	g.cur = g.main
	return g, nil
}

// Resize tells the renderer the framebuffer changed size, in pixels.
func (g *Graphics) Resize(width, height int) { g.r.Resize(width, height) }

// SetView sets the 2D coordinate space: (0,0) top-left to (width,height)
// bottom-right, whatever the framebuffer's pixel size.
func (g *Graphics) SetView(width, height float32) { g.main.setView(width, height) }

// View returns the current 2D coordinate space size.
func (g *Graphics) View() (float32, float32) { return g.main.viewW, g.main.viewH }

// Begin starts a frame cleared to clear. ok is false when the swapchain
// was rebuilt and the frame should be skipped.
func (g *Graphics) Begin(clear Color) (ok bool, err error) {
	g.frame, ok, err = g.r.BeginFrame()
	if err != nil || !ok {
		return ok, err
	}
	g.main.reset()
	g.main.clear = clear
	g.cur = g.main
	g.subFrames = g.subFrames[:0]
	return true, nil
}

// Draw queues a sprite. A nil texture draws with a 1x1 white texture, so a
// coloured rectangle is just a tinted sprite.
func (g *Graphics) Draw(tex *Texture, s Sprite) {
	if tex == nil {
		tex = g.white
	}
	if s.UV1 == (lin.Vec2{}) {
		s.UV1 = lin.V2(1, 1)
	}
	if s.Color == (Color{}) {
		s.Color = White
	}
	q := g.cur
	var clip ClipRect
	if n := len(q.clips); n > 0 {
		clip = q.clips[n-1]
	}
	q.sprites.add(tex, s, q.spriteProj, q.layer, clip)
}

// PushClip limits later sprite drawing to a view-space rectangle,
// intersected with any enclosing clip. Pair with PopClip.
func (g *Graphics) PushClip(x, y, w, h float32) {
	q := g.cur
	r := ClipRect{x, y, w, h}
	if n := len(q.clips); n > 0 {
		r = intersectClip(q.clips[n-1], r)
	}
	q.clips = append(q.clips, r)
}

// PopClip restores the previous clip rectangle.
func (g *Graphics) PopClip() {
	q := g.cur
	if len(q.clips) > 0 {
		q.clips = q.clips[:len(q.clips)-1]
	}
}

func intersectClip(a, b ClipRect) ClipRect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return ClipRect{X: x0, Y: y0, W: 0.001, H: 0.001} // fully clipped, but not "no clip"
	}
	return ClipRect{x0, y0, x1 - x0, y1 - y0}
}

// FillRect queues a solid rectangle.
func (g *Graphics) FillRect(x, y, w, h float32, c Color) {
	g.Draw(nil, Sprite{Pos: lin.V2(x, y), Size: lin.V2(w, h), Color: c})
}

// DrawTexture queues a texture at its own size.
func (g *Graphics) DrawTexture(tex *Texture, x, y float32) {
	g.Draw(tex, Sprite{Pos: lin.V2(x, y), Size: lin.V2(float32(tex.Width), float32(tex.Height))})
}

// End flushes queued work, submits and presents. With capture it returns the frame.
func (g *Graphics) End(capture bool) (*image.RGBA, error) {
	if g.frame == nil {
		return nil, fmt.Errorf("gfx: End without Begin")
	}
	fr := g.frame
	g.frame = nil
	g.cur = g.main
	cb := fr.CB
	for _, sf := range g.subFrames {
		if err := g.renderQueue(fr, sf.queue, sf.rt.scene, sf.rt.target); err != nil {
			return nil, err
		}
	}
	if err := g.renderQueue(fr, g.main, g.post.main, nil); err != nil {
		return nil, err
	}
	_ = cb
	return g.r.EndFrame(fr, capture)
}

// renderQueue draws one queue: the 3D scene through the post chain, then
// sprites, into target (a render texture) or the swapchain when nil.
func (g *Graphics) renderQueue(fr *render.Frame, q *drawQueue, t *sceneTargets, target *render.Target) error {
	cb := fr.CB
	has3D := len(q.draws) > 0
	bloom := has3D && g.post.settings.Bloom > 0
	ao := has3D && g.post.settings.AmbientOcclusion > 0
	if has3D {
		if err := g.renderScene(fr, q, t); err != nil {
			return err
		}
		if bloom {
			g.renderBloom(cb, t)
		}
		if ao {
			g.renderAO(cb, q, t)
		}
	}
	clear := q.clear.premultiplied()
	aa := has3D && !g.post.settings.NoAntiAlias && target == nil
	if aa {
		// Composite into the LDR image, then resolve with FXAA on screen.
		render.BeginTargetPass(cb, render.PassDesc{Target: t.ldr, ClearColor: clear, ClearDepth: 1})
		g.composite(cb, t, bloom, ao)
		render.EndTargetPass(cb, t.ldr)
	}
	extent := g.r.Swapchain.Extent
	if target != nil {
		render.BeginTargetPass(cb, render.PassDesc{Target: target, ClearColor: clear, ClearDepth: 1})
		extent = target.Extent
	} else {
		g.r.BeginSwapchainPass(fr, clear)
	}
	switch {
	case aa:
		g.antiAlias(cb, t)
	case has3D:
		g.composite(cb, t, bloom, ao)
	}
	if err := g.flushSprites(fr, q, extent); err != nil {
		return err
	}
	if target != nil {
		render.EndTargetPass(cb, target)
	}
	return nil
}

func (g *Graphics) flushSprites(fr *render.Frame, q *drawQueue, extent vk.VkExtent2D) error {
	if len(q.sprites.items) == 0 {
		return nil
	}
	q.sprites.build()
	if err := q.sprites.upload(g.r.Device, fr.Slot); err != nil {
		return err
	}
	cb := fr.CB
	render.SetViewport(cb, extent)
	var offset vk.VkDeviceSize
	vk.VkCmdBindVertexBuffers(cb, 0, 1, &q.sprites.buffers[fr.Slot].Handle, &offset)
	var bound *render.Pipeline
	var boundProj lin.Mat4
	var boundClip ClipRect
	scaleX, scaleY := float32(extent.Width)/q.viewW, float32(extent.Height)/q.viewH
	for _, d := range q.sprites.draws {
		if d.clip != boundClip {
			boundClip = d.clip
			render.SetScissor(cb, extent, int32(d.clip.X*scaleX), int32(d.clip.Y*scaleY),
				uint32(max(d.clip.W*scaleX, 0)), uint32(max(d.clip.H*scaleY, 0)), d.clip == ClipRect{})
		}
		pipe := g.spritePipe
		if d.tex.sdf {
			pipe = g.sdfPipe
		}
		if pipe != bound {
			bound = pipe
			vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
			boundProj = lin.Mat4{}
		}
		if d.proj != boundProj {
			boundProj = d.proj
			vk.VkCmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT,
				0, uint32(unsafe.Sizeof(d.proj)), unsafe.Pointer(&d.proj))
		}
		vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &d.tex.set, 0, nil)
		vk.VkCmdDraw(cb, 6, d.count, 0, d.first)
	}
	return nil
}

// Destroy releases everything the context created. Textures made from it
// must be destroyed first or are leaked with the device.
func (g *Graphics) Destroy() {
	_ = g.r.Device.WaitIdle()
	if g.main != nil {
		g.main.destroy()
	}
	g.post.destroy(g)
	g.meshes.destroy(g)
	if g.white != nil {
		g.white.Destroy()
	}
	if g.spritePipe != nil {
		g.spritePipe.Destroy()
	}
	if g.sdfPipe != nil {
		g.sdfPipe.Destroy()
	}
	dev := g.r.Device.Handle
	vk.VkDestroySampler(dev, g.nearest, nil)
	vk.VkDestroySampler(dev, g.linear, nil)
	vk.VkDestroySampler(dev, g.nearestRep, nil)
	vk.VkDestroySampler(dev, g.linearRep, nil)
	g.descriptors.Destroy()
}

// sampler picks the shared sampler for a filtering and edge choice.
func (g *Graphics) sampler(linear, repeat bool) vk.VkSampler {
	switch {
	case linear && repeat:
		return g.linearRep
	case linear:
		return g.linear
	case repeat:
		return g.nearestRep
	}
	return g.nearest
}
