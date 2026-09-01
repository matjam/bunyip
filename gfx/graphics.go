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
	R           *render.Renderer
	descriptors *render.DescriptorSets
	nearest     vk.VkSampler
	linear      vk.VkSampler
	spritePipe  *render.Pipeline
	sdfPipe     *render.Pipeline
	sprites     spriteBatch
	meshes      meshPass
	white       *Texture
	frame       *render.Frame
	proj        lin.Mat4
	clear       Color
	viewW       float32
	viewH       float32
}

// New builds the drawing context over a renderer.
func New(r *render.Renderer) (*Graphics, error) {
	g := &Graphics{R: r}
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
	ext := r.Swapchain.Extent
	g.SetView(float32(ext.Width), float32(ext.Height))
	return g, nil
}

// SetView sets the 2D coordinate space: (0,0) top-left to (width,height)
// bottom-right, whatever the framebuffer's pixel size.
func (g *Graphics) SetView(width, height float32) {
	g.viewW, g.viewH = width, height
	g.proj = lin.Ortho2D(width, height)
}

// View returns the current 2D coordinate space size.
func (g *Graphics) View() (float32, float32) { return g.viewW, g.viewH }

// Begin starts a frame cleared to clear. ok is false when the swapchain
// was rebuilt and the frame should be skipped.
func (g *Graphics) Begin(clear Color) (ok bool, err error) {
	g.clear = clear
	g.frame, ok, err = g.R.BeginFrame()
	if err != nil || !ok {
		return ok, err
	}
	g.sprites.reset()
	g.meshes.reset()
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
	g.sprites.add(tex, s)
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
	g.R.BeginSwapchainPass(fr, g.clear.premultiplied())
	if err := g.flushMeshes(fr); err != nil {
		return nil, err
	}
	if err := g.flushSprites(fr); err != nil {
		return nil, err
	}
	return g.R.EndFrame(fr, capture)
}

func (g *Graphics) flushSprites(fr *render.Frame) error {
	if len(g.sprites.instances) == 0 {
		return nil
	}
	if err := g.sprites.upload(g.R.Device, fr.Slot); err != nil {
		return err
	}
	cb := fr.CB
	var offset vk.VkDeviceSize
	vk.VkCmdBindVertexBuffers(cb, 0, 1, &g.sprites.buffers[fr.Slot].Handle, &offset)
	var bound *render.Pipeline
	for _, d := range g.sprites.draws {
		pipe := g.spritePipe
		if d.tex.sdf {
			pipe = g.sdfPipe
		}
		if pipe != bound {
			bound = pipe
			vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
			vk.VkCmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT,
				0, uint32(unsafe.Sizeof(g.proj)), unsafe.Pointer(&g.proj))
		}
		vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &d.tex.set, 0, nil)
		vk.VkCmdDraw(cb, 6, d.count, 0, d.first)
	}
	return nil
}

// Destroy releases everything the context created. Textures made from it
// must be destroyed first or are leaked with the device.
func (g *Graphics) Destroy() {
	_ = g.R.Device.WaitIdle()
	g.sprites.destroy()
	g.meshes.destroy()
	if g.white != nil {
		g.white.Destroy()
	}
	if g.spritePipe != nil {
		g.spritePipe.Destroy()
	}
	if g.sdfPipe != nil {
		g.sdfPipe.Destroy()
	}
	dev := g.R.Device.Handle
	vk.VkDestroySampler(dev, g.nearest, nil)
	vk.VkDestroySampler(dev, g.linear, nil)
	g.descriptors.Destroy()
}
