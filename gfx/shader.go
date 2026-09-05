package gfx

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Blend is how a draw combines with what is already there. Colours are
// premultiplied throughout, so these are the premultiplied equations.
type Blend uint8

const (
	BlendAlpha    Blend = iota // source over: the default
	BlendAdd                   // add light: glows, fire, particles
	BlendMultiply              // darken by the source: shadows, tinting
	BlendScreen                // the inverse of multiply: brighten
	BlendLighten               // keep the brighter of the two
	BlendDarken                // keep the darker of the two
	BlendReplace               // copy the source, ignoring what was there
	BlendErase                 // cut the source's shape out of what was there
	blendCount
)

// String names the blend mode.
func (b Blend) String() string {
	names := [...]string{"alpha", "add", "multiply", "screen", "lighten", "darken", "replace", "erase"}
	if int(b) < len(names) {
		return names[b]
	}
	return fmt.Sprintf("Blend(%d)", int(b))
}

// ParseBlend reads a blend mode written the way String spells it, so a
// mode can be named in an asset file or on a console line. It reports
// false for anything else, leaving the caller to keep its default.
func ParseBlend(s string) (Blend, bool) {
	for b := Blend(0); b < blendCount; b++ {
		if b.String() == s {
			return b, true
		}
	}
	return BlendAlpha, false
}

func (b Blend) factors() *render.BlendFactors {
	over := vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA
	f := func(sc, dc vk.VkBlendFactor, op vk.VkBlendOp) *render.BlendFactors {
		return &render.BlendFactors{SrcColor: sc, DstColor: dc, ColorOp: op, SrcAlpha: vk.VK_BLEND_FACTOR_ONE, DstAlpha: over, AlphaOp: vk.VK_BLEND_OP_ADD}
	}
	switch b {
	case BlendAdd:
		return f(vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_OP_ADD)
	case BlendMultiply:
		return f(vk.VK_BLEND_FACTOR_DST_COLOR, over, vk.VK_BLEND_OP_ADD)
	case BlendScreen:
		return f(vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_COLOR, vk.VK_BLEND_OP_ADD)
	case BlendLighten:
		return f(vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_OP_MAX)
	case BlendDarken:
		return f(vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_FACTOR_ONE, vk.VK_BLEND_OP_MIN)
	case BlendReplace:
		return &render.BlendFactors{SrcColor: vk.VK_BLEND_FACTOR_ONE, DstColor: vk.VK_BLEND_FACTOR_ZERO, ColorOp: vk.VK_BLEND_OP_ADD,
			SrcAlpha: vk.VK_BLEND_FACTOR_ONE, DstAlpha: vk.VK_BLEND_FACTOR_ZERO, AlphaOp: vk.VK_BLEND_OP_ADD}
	case BlendErase:
		return &render.BlendFactors{SrcColor: vk.VK_BLEND_FACTOR_ZERO, DstColor: over, ColorOp: vk.VK_BLEND_OP_ADD,
			SrcAlpha: vk.VK_BLEND_FACTOR_ZERO, DstAlpha: over, AlphaOp: vk.VK_BLEND_OP_ADD}
	}
	p := render.PremultipliedOver
	return &p
}

// SetBlend sets the blend mode for later 2D drawing in the current
// queue. It is reset to BlendAlpha at the start of each frame.
func (g *Graphics) SetBlend(b Blend) { g.cur.blend = b }

// Blend returns the current 2D blend mode.
func (g *Graphics) Blend() Blend { return g.cur.blend }

// Blended runs draw with the blend mode set, then restores the previous one.
func (g *Graphics) Blended(b Blend, draw func()) {
	prev := g.cur.blend
	g.cur.blend = b
	draw()
	g.cur.blend = prev
}

// PushTransform composes a transform onto the 2D transform stack: later
// sprites, text and paths are mapped through it (after their own
// placement, before the camera). Pair with PopTransform.
func (g *Graphics) PushTransform(m lin.Affine) {
	q := g.cur
	q.xforms = append(q.xforms, q.xform)
	q.xform = q.xform.Mul(m)
}

// PopTransform restores the transform in force before the matching PushTransform.
func (g *Graphics) PopTransform() {
	q := g.cur
	if n := len(q.xforms); n > 0 {
		q.xform = q.xforms[n-1]
		q.xforms = q.xforms[:n-1]
	}
}

// Transformed runs draw with the transform pushed, the closure form of
// PushTransform and PopTransform.
func (g *Graphics) Transformed(m lin.Affine, draw func()) {
	g.PushTransform(m)
	draw()
	g.PopTransform()
}

// Transform returns the current composed 2D transform.
func (g *Graphics) Transform() lin.Affine { return g.cur.xform }

// Shader is a fragment program the game wrote, compiled to SPIR-V with
// bunyip-shader. A sprite shader colours 2D drawing; a mesh shader
// adjusts a surface before the engine lights it. Uniforms and up to four
// extra images ride along with every draw made while it is set.
type Shader struct {
	// VertexBounds is how far a mesh shader's vertex program moves a
	// vertex, as a multiple of the mesh's bounding radius: 0.25 for a flag
	// that ripples a quarter of its own size. Culling grows a draw's
	// radius by 1 + VertexBounds. Zero means the program may put a vertex
	// anywhere, so draws made with the shader are never culled; set it as
	// soon as the displacement has a limit. It is read as each draw is
	// prepared, so a game may change it between draws.
	VertexBounds float32

	g      *Graphics
	frag   []byte
	stages map[shaders.Stage][]byte // a mesh shader's vertex programs, when it hooks vertices
	mesh   bool
	images [4]*Texture
	block  []byte // the latest uniform values
	dirty  bool   // block changed since it was last placed in the arena
	offset uint32 // arena offset of the block for this frame
	frame  uint64 // frame the offset belongs to
	pipes  map[pipeKey]*render.Pipeline
}

// pipeKey selects a pipeline variant of a shader.
type pipeKey struct {
	blend        Blend // 2D: the blend mode; mesh: BlendAlpha for translucent materials, BlendReplace for opaque
	skinned      bool
	doubleSided  bool
	noDepthTest  bool
	noDepthWrite bool
	shadow       bool // the depth-only shadow pass
	stencil      bool // mark the stencil buffer, for outlines
}

// meshKey is the pipeline variant a material needs. It takes a pointer
// because Material is large and this sits in the draw loop.
func meshKey(mat *Material, skinned bool) pipeKey {
	key := pipeKey{blend: BlendReplace, skinned: skinned, doubleSided: mat.DoubleSided, noDepthTest: mat.NoDepthTest, noDepthWrite: mat.NoDepthWrite, stencil: mat.Outline > 0}
	if mat.blended() {
		key.blend = BlendAlpha
	}
	return key
}

// NewShader compiles a sprite (2D) shader from SPIR-V produced by
// bunyip-shader from a source that defines vec4 fragment(vec2 uv, vec4 color).
func (g *Graphics) NewShader(spirv []byte) (*Shader, error) {
	return g.newShader(spirv, false)
}

// NewMeshShader compiles a mesh (surface) shader from SPIR-V produced by
// bunyip-shader -kind mesh from a source that defines void surface(inout Surface s).
func (g *Graphics) NewMeshShader(spirv []byte) (*Shader, error) {
	return g.newShader(spirv, true)
}

func (g *Graphics) newShader(data []byte, mesh bool) (*Shader, error) {
	s := &Shader{g: g, mesh: mesh, pipes: map[pipeKey]*render.Pipeline{}}
	if mesh {
		stages, err := shaders.Unbundle(data)
		if err != nil {
			return nil, fmt.Errorf("gfx: %w", err)
		}
		s.frag = stages[shaders.StageFrag]
		delete(stages, shaders.StageFrag)
		if len(stages) > 0 {
			s.stages = stages
		}
	} else {
		s.frag = data
	}
	for _, spv := range append([][]byte{s.frag}, slices.Collect(maps.Values(s.stages))...) {
		if len(spv) < 20 || len(spv)%4 != 0 || spv[0] != 0x03 || spv[1] != 0x02 || spv[2] != 0x23 || spv[3] != 0x07 {
			return nil, fmt.Errorf("gfx: shader is not SPIR-V (compile it with bunyip-shader)")
		}
	}
	// Build the common variants now so a bad module fails here, not mid-frame.
	keys := []pipeKey{{}}
	if mesh {
		keys = []pipeKey{{blend: BlendReplace}, {shadow: true}}
	}
	for _, key := range keys {
		if _, err := s.pipeline(key); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// vert is the shader's program for a vertex stage, or the engine's.
func (s *Shader) vert(st shaders.Stage) []byte {
	if spv, ok := s.stages[st]; ok {
		return spv
	}
	switch st {
	case shaders.StageSkinVert:
		return shaders.PBRSkinVert
	case shaders.StageShadowVert:
		return shaders.ShadowVert
	case shaders.StageShadowSkinVert:
		return shaders.ShadowSkinVert
	}
	return shaders.PBRVert
}

// pipeline returns the shader's pipeline for a variant, building it on
// first use.
func (s *Shader) pipeline(key pipeKey) (*render.Pipeline, error) {
	if p, ok := s.pipes[key]; ok {
		return p, nil
	}
	g := s.g
	var desc render.PipelineDesc
	if s.mesh && key.shadow {
		desc = g.meshes.shadowPipelineDesc(key.skinned)
		if key.skinned {
			desc.Vert = s.vert(shaders.StageShadowSkinVert)
		} else {
			desc.Vert = s.vert(shaders.StageShadowVert)
		}
	} else if s.mesh {
		desc = g.meshes.pipelineDesc(key.skinned)
		desc.Frag = s.frag
		if key.skinned {
			desc.Vert = s.vert(shaders.StageSkinVert)
		} else {
			desc.Vert = s.vert(shaders.StageVert)
		}
		if key.blend != BlendReplace {
			desc.Blend, desc.DepthWrite, desc.CullMode = true, false, vk.VK_CULL_MODE_NONE
		}
		if key.doubleSided {
			desc.CullMode = vk.VK_CULL_MODE_NONE
		}
		if key.noDepthTest {
			desc.DepthTest = false
		}
		if key.noDepthWrite {
			desc.DepthWrite = false
		}
		if key.stencil {
			desc.Stencil = render.StencilWrite(1)
		}
	} else {
		bindings, attrs := vertex2DLayout()
		desc = render.PipelineDesc{
			Vert: g.spriteVert(), Frag: s.frag,
			ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
			Bindings: bindings, Attributes: attrs,
			Blend: true, Factors: key.blend.factors(),
			PushConstantSize: push2DSize,
			SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout, g.uniforms.Layout},
		}
	}
	p, err := g.r.Device.NewPipeline(desc)
	if err != nil {
		return nil, fmt.Errorf("gfx: shader pipeline: %w", err)
	}
	s.pipes[key] = p
	return p, nil
}

// SetUniforms copies a struct's bytes as the shader's uniform block for
// draws from now on. Lay the struct out by std140 rules: float32, int32,
// lin.Vec2, lin.Vec4 and lin.Mat4 fields are safe; a lin.Vec3 must be
// followed by a float32 of padding, and the block is at most 1024 bytes.
func (s *Shader) SetUniforms(v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		panic("gfx: SetUniforms wants a struct")
	}
	size := int(rv.Type().Size())
	if size == 0 || size > maxUniformBlock {
		panic(fmt.Sprintf("gfx: uniform block of %d bytes; want 1..%d", size, maxUniformBlock))
	}
	// Copy out of an addressable value.
	if !rv.CanAddr() {
		tmp := reflect.New(rv.Type()).Elem()
		tmp.Set(rv)
		rv = tmp
	}
	src := unsafe.Slice((*byte)(rv.Addr().UnsafePointer()), size)
	s.block = append(s.block[:0], src...)
	s.dirty = true
}

// SetImage binds a texture as image0..image3 for draws from now on; nil
// unbinds it (the shader then samples white).
func (s *Shader) SetImage(slot int, t *Texture) {
	if slot < 0 || slot >= len(s.images) {
		panic(fmt.Sprintf("gfx: image slot %d; want 0..3", slot))
	}
	s.images[slot] = t
}

// uniformOffset places the shader's block in this frame's arena when it
// has not been yet, and returns its offset; -1 when the shader has no
// uniforms.
func (s *Shader) uniformOffset() int32 {
	if s == nil || len(s.block) == 0 {
		return -1
	}
	g := s.g
	if s.dirty || s.frame != g.frameNo {
		off, err := g.arena.Add(s.block)
		if err != nil {
			return -1
		}
		s.offset, s.frame, s.dirty = off, g.frameNo, false
	}
	return int32(s.offset)
}

// Reload replaces the shader's program with newly compiled SPIR-V from
// bunyip-shader, rebuilding its pipelines, so a game watching its shader
// files (asset.Watcher) can swap them while it runs. Images and uniforms
// are kept. The old pipelines are freed once the frame that may still be
// drawing with them has finished.
func (s *Shader) Reload(spirv []byte) error {
	fresh, err := s.g.newShader(spirv, s.mesh)
	if err != nil {
		return err
	}
	s.retirePipelines()
	s.frag, s.stages, s.pipes = fresh.frag, fresh.stages, fresh.pipes
	return nil
}

// Destroy frees the shader's pipelines. Called inside a frame it costs
// no wait: they go on the frame slot's retire list and are freed once
// that frame has finished.
func (s *Shader) Destroy() {
	if s == nil || s.pipes == nil {
		return
	}
	s.retirePipelines()
	s.pipes = nil
}

// retirePipelines hands the shader's pipelines to the frame slot's
// retire list, so draws already recorded keep their pipeline.
func (s *Shader) retirePipelines() {
	pipes := slices.Collect(maps.Values(s.pipes))
	s.g.deferDestroy(func() {
		for _, p := range pipes {
			p.Destroy()
		}
	})
}

// SetShader makes later 2D drawing in the current queue use a sprite
// shader; nil restores the default. It is reset at the start of each frame.
func (g *Graphics) SetShader(s *Shader) {
	if s != nil && s.mesh {
		panic("gfx: SetShader wants a sprite shader; use Material.Shader for meshes")
	}
	g.cur.shader = s
}

// Shaded runs draw with the shader set, then restores the previous one.
func (g *Graphics) Shaded(s *Shader, draw func()) {
	prev := g.cur.shader
	g.SetShader(s)
	draw()
	g.cur.shader = prev
}

// setTime tells the graphics context the game clock, which shaders read
// as time(). The engine calls it every frame.
func (g *Graphics) setTime(seconds float64) { g.time = float32(seconds) }

const maxUniformBlock = 1024
