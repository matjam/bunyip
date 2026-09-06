package gfx

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

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
func (g *Graphics) SetBlend(b Blend) {
	g.cur.blend, g.cur.customBlend = b, customBlend{}
}

// Blend returns the current built-in 2D blend mode. CustomBlended temporarily
// overrides its equations without changing this value.
func (g *Graphics) Blend() Blend { return g.cur.blend }

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

// Transform returns the current composed 2D transform.
func (g *Graphics) Transform() lin.Affine { return g.cur.xform }

// Shader is a fragment program the game wrote, compiled to SPIR-V with
// bunyip-shader or Compiler. A sprite shader colours 2D drawing; a mesh shader
// adjusts a surface before the engine lights it. Uniforms and up to four
// extra images ride along with every draw made while it is set.
type Shader struct {
	// VertexBounds is how far a mesh shader's vertex program moves a
	// vertex, as a multiple of the mesh's bounding radius: 0.25 for a flag
	// that ripples a quarter of its own size. Culling grows a draw's
	// radius by 1 + VertexBounds. Zero means the program may put a vertex
	// anywhere, so draws made with the shader are never culled; set it as
	// soon as the displacement has a limit. This applies only to shaders
	// with a vertex hook. It is read when the frame prepares its queued
	// draws, so the final value applies to every draw using this shader.
	VertexBounds float32

	g    *Graphics
	frag []byte
	// oitFrag is a mesh shader's fragment program for the
	// order-independent transparency pass, which writes a second
	// attachment. A bundle compiled before that pass existed has none,
	// and draws made with the shader keep the sorted path.
	oitFrag []byte
	stages  map[shaders.Stage][]byte // a mesh shader's vertex programs, when it hooks vertices
	mesh    bool
	images  [4]*Texture
	block   []byte // the latest uniform values
	dirty   bool   // block changed since it was last placed in the arena
	offset  uint32 // arena offset of the block for this frame
	frame   uint64 // frame the offset belongs to
	pipes   map[pipeKey]*render.Pipeline
}

// pipeKey selects a pipeline variant of a shader.
type pipeKey struct {
	customBlend  customBlend
	stencil2D    stencil2D
	blend        Blend // 2D: the blend mode; mesh: BlendAlpha for translucent materials, BlendReplace for opaque
	skinned      bool
	doubleSided  bool
	noDepthTest  bool
	noDepthWrite bool
	shadow       bool // the depth-only shadow pass
	stencil      bool // mark the stencil buffer, for outlines
	oit          bool // the order-independent transparency pass
	// The material's own stencil state, when it has one and no outline.
	stencilTest StencilTest
	stencilOp   StencilOp
	stencilRef  uint8
	// out is the pass's attachment set. A mesh shader uses its sample
	// count, since the scene may be multisampled; a sprite shader uses
	// all of it, since a render texture chooses its own colour format,
	// depth and samples. The shadow pass and the order-independent pass
	// are always the zero value: both are single-sample.
	out outKey
}

// meshKey is the pipeline variant a material needs in a pass. It takes a
// pointer because Material is large and this sits in the draw loop.
func meshKey(mat *Material, skinned, shell bool, out outKey) pipeKey {
	key := pipeKey{blend: BlendReplace, skinned: skinned, doubleSided: mat.DoubleSided, noDepthTest: mat.NoDepthTest, noDepthWrite: mat.NoDepthWrite, stencil: mat.Outline > 0, out: out}
	if !key.stencil {
		key.stencilTest, key.stencilOp, key.stencilRef = mat.Stencil, mat.StencilWrite, mat.StencilRef
	}
	// A fur shell is blended and leaves the depth buffer alone whatever
	// the material says, so the shells under it still draw.
	if mat.blended() || shell {
		key.blend = BlendAlpha
	}
	return key
}

// NewShader creates a sprite (2D) shader from SPIR-V produced by
// bunyip-shader from a source that defines fn fragment(uv: vec2f, color: vec4f) -> vec4f.
func (g *Graphics) NewShader(spirv []byte) (*Shader, error) {
	return g.newShader(spirv, false)
}

// NewMeshShader creates a mesh (surface) shader from SPIR-V produced by
// bunyip-shader -kind mesh from a source that defines fn surface(s: Surface) -> Surface.
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
		s.oitFrag = stages[shaders.StageOITFrag]
		delete(stages, shaders.StageFrag)
		delete(stages, shaders.StageOITFrag)
		if len(stages) > 0 {
			s.stages = stages
		}
	} else {
		s.frag = data
	}
	programs := append([][]byte{s.frag}, slices.Collect(maps.Values(s.stages))...)
	if s.oitFrag != nil {
		programs = append(programs, s.oitFrag)
	}
	for _, spv := range programs {
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
			s.Destroy()
			return nil, err
		}
	}
	g.owned.add(s)
	return s, nil
}

// orderIndependent reports whether the shader has the fragment program
// the order-independent transparency pass needs. A mesh shader compiled
// before that pass existed has not, so its draws stay sorted.
func (s *Shader) orderIndependent() bool { return len(s.oitFrag) > 0 }

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
		if key.oit {
			desc.Frag = s.oitFrag
		}
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
		} else if key.stencilTest != StencilAlways || key.stencilOp != StencilKeep {
			desc.Stencil = &render.StencilState{
				Compare: key.stencilTest.compareOp(), Ref: uint32(key.stencilRef),
				Write: key.stencilOp != StencilKeep, Pass: key.stencilOp.vkOp(),
			}
		}
		desc.Samples = key.out.samples
		if key.oit {
			// The order-independent pass writes two attachments: the
			// weighted colour adds into the accumulation image, and the
			// alpha multiplies the revealage image down towards nothing.
			desc.Factors = &render.BlendFactors{
				SrcColor: vk.VK_BLEND_FACTOR_ONE, DstColor: vk.VK_BLEND_FACTOR_ONE, ColorOp: vk.VK_BLEND_OP_ADD,
				SrcAlpha: vk.VK_BLEND_FACTOR_ONE, DstAlpha: vk.VK_BLEND_FACTOR_ONE, AlphaOp: vk.VK_BLEND_OP_ADD,
			}
			desc.ExtraColor = []render.ColorAttachment{{Format: revealFormat, Blend: true, Factors: &render.BlendFactors{
				SrcColor: vk.VK_BLEND_FACTOR_ZERO, DstColor: vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_COLOR, ColorOp: vk.VK_BLEND_OP_ADD,
				SrcAlpha: vk.VK_BLEND_FACTOR_ZERO, DstAlpha: vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA, AlphaOp: vk.VK_BLEND_OP_ADD,
			}}}
		}
	} else {
		bindings, attrs := vertex2DLayout()
		desc = key.out.apply(render.PipelineDesc{
			Vert: g.spriteVert(), Frag: s.frag,
			ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
			Bindings: bindings, Attributes: attrs,
			Blend: true, Factors: key.customBlend.factors(key.blend),
			Stencil: key.stencil2D.renderState(), NoColorWrite: key.stencil2D.options.NoColor,
			PushConstantSize: push2DSize,
			SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout, g.uniforms.Layout},
		})
	}
	p, err := g.r.Device.NewPipeline(desc)
	if err != nil {
		return nil, fmt.Errorf("gfx: shader pipeline: %w", err)
	}
	s.pipes[key] = p
	return p, nil
}

// SetUniforms packs a struct or non-nil pointer to one into a std140 block
// for subsequent draws. Fields follow declaration order and must be exported.
// Supported values are float32, int32, uint32, bool (including named scalar
// types), lin.Vec2/Vec3/Vec4, Color, lin.Mat3/Mat4, fixed arrays and nested
// structs. Matrices are column-major. A plain [N]float32 is a scalar array,
// with 16-byte strides; use lin vector/matrix types for WGSL vectors/matrices.
// Booleans map to WGSL u32 fields (0 or 1). WGSL scalar arrays need padded
// element wrappers to match the 16-byte std140 stride. Padding is automatic and zeroed. No Go memory is retained. Unsupported
// fields or blocks exceeding 1024 packed bytes return errors and preserve
// the previous block. The caller must match the shader's declarations;
// this does not inspect SPIR-V or perform numeric precision conversions.
func (s *Shader) SetUniforms(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("gfx: SetUniforms wants a struct or non-nil pointer to one")
	}
	plan, err := cachedUniformPlan(rv.Type())
	if err != nil {
		return fmt.Errorf("gfx: shader uniforms: %w", err)
	}
	size := uniformAlign(plan.size, 16)
	if cap(s.block) < size {
		s.block = make([]byte, size)
	} else {
		s.block = s.block[:size]
		clear(s.block)
	}
	plan.pack(s.block, rv)
	s.dirty = true
	return nil
}

// SetImage binds a texture as image0..image3 for draws from now on; nil
// unbinds it (the shader then samples white).
// A texture from another Graphics panics without changing the binding.
func (s *Shader) SetImage(slot int, t *Texture) {
	if slot < 0 || slot >= len(s.images) {
		panic(fmt.Sprintf("gfx: image slot %d; want 0..3", slot))
	}
	s.g.requireTextureOwner(t)
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
			g.recordDrawError(fmt.Errorf("gfx: shader uniforms: %w", err))
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
	s.frag, s.oitFrag, s.stages, s.pipes = fresh.frag, fresh.oitFrag, fresh.stages, fresh.pipes
	s.g.owned.remove(fresh)
	s.g.owned.add(s)
	return nil
}

// Destroy frees the shader's pipelines. Called inside a frame it costs
// no wait: they go on the frame slot's retire list and are freed once
// that frame has finished.
func (s *Shader) Destroy() {
	if s == nil || s.pipes == nil {
		return
	}
	s.g.owned.remove(s)
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
// A shader from another Graphics panics without changing the current shader.
func (g *Graphics) SetShader(s *Shader) {
	g.requireShaderOwner(s)
	if s != nil && s.mesh {
		panic("gfx: SetShader wants a sprite shader; use Material.Shader for meshes")
	}
	g.cur.shader = s
}

// setTime tells the graphics context the game clock, which shaders read
// as time(). The engine calls it every frame.
func (g *Graphics) setTime(seconds float64) { g.time = float32(seconds) }

const maxUniformBlock = 1024
