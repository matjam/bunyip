package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Sprite is one textured quad. Size is in view units; UV0 and UV1 select
// the texture region in 0..1; Origin is the rotation pivot as a fraction
// of Size.
type Sprite struct {
	Pos      lin.Vec2
	Size     lin.Vec2
	UV0, UV1 lin.Vec2
	Color    Color
	Rotation float32
	Origin   lin.Vec2
}

// spriteInstance is the GPU layout of a sprite; see sprite.vert.
type spriteInstance struct {
	pos      lin.Vec2
	size     lin.Vec2
	uv0, uv1 lin.Vec2
	color    [4]float32
	rotation float32
	origin   lin.Vec2
	_        float32 // pad to 64 bytes
}

const spriteInstanceSize = 64

type spriteDraw struct {
	tex          *Texture
	first, count uint32
}

// spriteBatch collects sprites for a frame and issues one draw per run of
// the same texture.
type spriteBatch struct {
	instances []spriteInstance
	draws     []spriteDraw
	buffers   [render.FramesInFlight]*render.Buffer
	capacity  int
}

const initialSpriteCapacity = 4096

func (b *spriteBatch) add(tex *Texture, s Sprite) {
	if n := len(b.draws); n > 0 && b.draws[n-1].tex == tex {
		b.draws[n-1].count++
	} else {
		b.draws = append(b.draws, spriteDraw{tex: tex, first: uint32(len(b.instances)), count: 1})
	}
	b.instances = append(b.instances, spriteInstance{
		pos: s.Pos, size: s.Size, uv0: s.UV0, uv1: s.UV1,
		color: s.Color.premultiplied(), rotation: s.Rotation, origin: s.Origin,
	})
}

func (b *spriteBatch) reset() {
	b.instances = b.instances[:0]
	b.draws = b.draws[:0]
}

// upload copies this frame's instances into the slot's buffer, growing every
// slot's buffer when the batch outgrew them.
func (b *spriteBatch) upload(dev *render.Device, slot int) error {
	if len(b.instances) > b.capacity {
		if err := dev.WaitIdle(); err != nil {
			return err
		}
		newCap := max(b.capacity*2, initialSpriteCapacity)
		for newCap < len(b.instances) {
			newCap *= 2
		}
		for i := range b.buffers {
			if b.buffers[i] != nil {
				b.buffers[i].Destroy()
			}
			buf, err := dev.NewBuffer(vk.VkDeviceSize(newCap*spriteInstanceSize), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
				vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
			if err != nil {
				return err
			}
			b.buffers[i] = buf
		}
		b.capacity = newCap
	}
	if len(b.instances) == 0 {
		return nil
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&b.instances[0])), len(b.instances)*spriteInstanceSize)
	return b.buffers[slot].Write(0, data)
}

func (b *spriteBatch) destroy() {
	for i := range b.buffers {
		if b.buffers[i] != nil {
			b.buffers[i].Destroy()
			b.buffers[i] = nil
		}
	}
}

// spriteVertexLayout describes the per-instance vertex binding.
func spriteVertexLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings := []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: spriteInstanceSize, InputRate: vk.VK_VERTEX_INPUT_RATE_INSTANCE}}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 8},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 16},
		{Location: 3, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
		{Location: 4, Binding: 0, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 32},
		{Location: 5, Binding: 0, Format: vk.VK_FORMAT_R32_SFLOAT, Offset: 48},
		{Location: 6, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 52},
	}
	return bindings, attrs
}
