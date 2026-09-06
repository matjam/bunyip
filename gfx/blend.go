package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// BlendFactor scales a source or destination component before blending.
type BlendFactor uint8

const (
	FactorZero             BlendFactor = iota // discard the input
	FactorOne                                 // use the input unchanged
	FactorSrcColor                            // multiply by the source colour
	FactorOneMinusSrcColor                    // multiply by one minus source colour
	FactorDstColor                            // multiply by the destination colour
	FactorOneMinusDstColor                    // multiply by one minus destination colour
	FactorSrcAlpha                            // multiply by source alpha
	FactorOneMinusSrcAlpha                    // multiply by one minus source alpha
	FactorDstAlpha                            // multiply by destination alpha
	FactorOneMinusDstAlpha                    // multiply by one minus destination alpha
	FactorSrcAlphaSaturate                    // min(source alpha, 1-destination alpha); one for alpha
	blendFactorCount
)

// BlendEquation combines the scaled source and destination components.
// Min and Max ignore their factors, as required by the graphics API.
type BlendEquation uint8

const (
	EquationAdd             BlendEquation = iota // source plus destination
	EquationSubtract                             // source minus destination
	EquationReverseSubtract                      // destination minus source
	EquationMin                                  // smaller source/destination component
	EquationMax                                  // larger source/destination component
	blendEquationCount
)

// BlendOptions controls colour and alpha independently. Values are literal:
// zero factors discard both inputs. Start with BlendAlpha.Options() for
// source-over defaults, then change the fields your effect needs. Colours
// supplied to the blend stage are premultiplied.
type BlendOptions struct {
	SrcColor, DstColor BlendFactor
	ColorOp            BlendEquation
	SrcAlpha, DstAlpha BlendFactor
	AlphaOp            BlendEquation
}

func (o BlendOptions) validate() {
	if o.SrcColor >= blendFactorCount || o.DstColor >= blendFactorCount || o.SrcAlpha >= blendFactorCount || o.DstAlpha >= blendFactorCount || o.ColorOp >= blendEquationCount || o.AlphaOp >= blendEquationCount {
		panic("gfx: invalid blend options")
	}
}

func (f BlendFactor) vkFactor() vk.VkBlendFactor {
	return [...]vk.VkBlendFactor{
		vk.VK_BLEND_FACTOR_ZERO, vk.VK_BLEND_FACTOR_ONE,
		vk.VK_BLEND_FACTOR_SRC_COLOR, vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_COLOR,
		vk.VK_BLEND_FACTOR_DST_COLOR, vk.VK_BLEND_FACTOR_ONE_MINUS_DST_COLOR,
		vk.VK_BLEND_FACTOR_SRC_ALPHA, vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA,
		vk.VK_BLEND_FACTOR_DST_ALPHA, vk.VK_BLEND_FACTOR_ONE_MINUS_DST_ALPHA,
		vk.VK_BLEND_FACTOR_SRC_ALPHA_SATURATE,
	}[f]
}

func (o BlendOptions) factors() *render.BlendFactors {
	return &render.BlendFactors{SrcColor: o.SrcColor.vkFactor(), DstColor: o.DstColor.vkFactor(), ColorOp: vk.VkBlendOp(o.ColorOp), SrcAlpha: o.SrcAlpha.vkFactor(), DstAlpha: o.DstAlpha.vkFactor(), AlphaOp: vk.VkBlendOp(o.AlphaOp)}
}

// Options returns the equations of a built-in blend mode. The result is
// independent and can be edited before passing it to CustomBlended.
func (b Blend) Options() BlendOptions {
	f := b.factors()
	return BlendOptions{SrcColor: BlendFactor(f.SrcColor), DstColor: BlendFactor(f.DstColor), ColorOp: BlendEquation(f.ColorOp), SrcAlpha: BlendFactor(f.SrcAlpha), DstAlpha: BlendFactor(f.DstAlpha), AlphaOp: BlendEquation(f.AlphaOp)}
}

type customBlend struct {
	options BlendOptions
	set     bool
}

func (c customBlend) factors(b Blend) *render.BlendFactors {
	if c.set {
		return c.options.factors()
	}
	return b.factors()
}

// CustomBlended runs 2D drawing with explicit blend equations, restoring
// the original queue's blend state even on panic. It includes geometry and
// flat particles. Invalid factors or equations panic before drawing.
func (g *Graphics) CustomBlended(options BlendOptions, draw func()) {
	options.validate()
	q := g.cur
	blend, custom := q.blend, q.customBlend
	defer func() { q.blend, q.customBlend = blend, custom }()
	q.customBlend = customBlend{options: options, set: true}
	draw()
}
